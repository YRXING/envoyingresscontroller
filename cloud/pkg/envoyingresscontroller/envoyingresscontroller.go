package envoyingresscontroller

import (
	"fmt"
	envoy_api_v2_route "github.com/envoyproxy/go-control-plane/envoy/api/v2/route"
	route "github.com/envoyproxy/go-control-plane/envoy/config/route/v3"
	"github.com/golang/protobuf/ptypes/duration"
	"github.com/golang/protobuf/ptypes/wrappers"
	"google.golang.org/protobuf/types/known/wrapperspb"
	apiequality "k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/wait"
	podutil "k8s.io/kubernetes/pkg/api/v1/pod"
	"strings"
	"time"
	"reflect"
	"sync"

	beehiveContext "github.com/kubeedge/beehive/pkg/core/context"
	"github.com/kubeedge/kubeedge/cloud/pkg/common/modules"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/informers"
	coreinformers "k8s.io/client-go/informers/core/v1"
	clientset "k8s.io/client-go/kubernetes"
	v1core "k8s.io/client-go/kubernetes/typed/core/v1"
	corelisters "k8s.io/client-go/listers/core/v1"
	networkingListers "k8s.io/client-go/listers/networking/v1"
	networkingInformers "k8s.io/client-go/informers/networking/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/component-base/metrics/prometheus/ratelimiter"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/kubernetes/pkg/controller"
	"github.com/kubeedge/beehive/pkg/core/model"
	ingressv1 "k8s.io/api/networking/v1"
	v1 "k8s.io/api/core/v1"
	envoyv2 "github.com/envoyproxy/go-control-plane/envoy/api/v2"
	endpoint "github.com/envoyproxy/go-control-plane/envoy/api/v2/endpoint"
	listener "github.com/envoyproxy/go-control-plane/envoy/api/v2/listener"
	http_router "github.com/envoyproxy/go-control-plane/envoy/config/filter/http/router/v2"
	http_conn_manager "github.com/envoyproxy/go-control-plane/envoy/config/filter/network/http_connection_manager/v2"
	tlsv3 "github.com/envoyproxy/go-control-plane/envoy/extensions/transport_sockets/tls/v3"
	matcher "github.com/envoyproxy/go-control-plane/envoy/type/matcher"
	"github.com/envoyproxy/go-control-plane/pkg/conversion"
	"github.com/envoyproxy/go-control-plane/envoy/api/v2/core"
	"github.com/kubeedge/kubeedge/cloud/pkg/envoyingresscontroller/config/v1alpha1"
	"github.com/kubeedge/beehive/pkg/core"
	keclient "github.com/kubeedge/kubeedge/cloud/pkg/common/client"
	"k8s.io/klog/v2"
)

// TODO: Need consider situations where ingress contains configmap object

const(
	ENVOYINGRESSCONTROLLERNAME = "envoyingress"
	// envoy ingress should have this annotation which indicates the node group to send to
	ENVOYINGRESSNODEGROUPANNOTATION = "v1alpha1.kubeedge.io/nodegroup"
	NODEGROUPLABEL = "nodegroup"
)

//KubeedgeClient is used for sending message to and from cloudhub.
//It's communication is based upon beehive.
type KubeedgeClient struct{
	Source     string
	Destination  string
}

//EnvoyIngressControllerConfiguration's field affects how controller works
type EnvoyIngressControllerConfiguration struct{
	syncInterval time.Duration
	//envoy related fields
	ingressSyncWorkerNumber int
	envoyServiceSyncWorkerNumber int
}

// EnvoyIngressController is responsible for converting envoy ingress to envoy configuration
// and synchronizing it to cloudhub which will dispatch it's received objects to edgehub.
type EnvoyIngressController struct{
	enable bool
	kubeClient clientset.Interface
	kubeedgeClient KubeedgeClient

	envoyIngressControllerConfiguration EnvoyIngressControllerConfiguration

	eventRecorder record.EventRecorder

	// To allow injection for testing.
	syncHandler func(key string) error

	// node2group saves the 1 to n relationship of a node's groups
	// So a node can join not only one node group
	// Because the label in k8s is map[string]string, the nodegroup label can only contain one string.
	// In case a node belongs to more than one group, the groups should be seperated by ; signal.
	// For example, node A belongs to nodegroup x and y, and its nodegroup label can be in the format: nodegroup: a;b
	node2group map[string][]string
	//group2node save2 the 1 to n relationship of a group's node members
	group2node map[string][]string
	// The lock is used for protecting write operations to node2group and group2node
	lock sync.RWMutex

	// podLister get list/get pods from the shared informers's store
	podLister corelisters.PodLister
	// podStoreSynced returns true if the pod store has been synced at least once.
	// Added as a member to the struct to allow injection for testing.
	podStoreSynced cache.InformerSynced
	// nodeLister can list/get nodes from the shared informer's store
	nodeLister corelisters.NodeLister
	// nodeStoreSynced returns true if the node store has been synced at least once.
	// Added as a member to the struct to allow injection for testing.
	nodeStoreSynced cache.InformerSynced
	// serviceLister can list/get services from the shared informer's store
	serviceLister corelisters.ServiceLister
	// serviceStoreSynced returns true if the service store has been synced at least once.
	// Added as a member to the struct to allow injection for testing.
	serviceStoreSynced cache.InformerSynced
	// ingressLister can list/get ingresses from the shared informer's store
	ingressLister networkingListers.IngressLister
	// ingressStoreSynced returns true if the ingress store has been synced at least once.
	// Added as a member to the struct to allow injection for testing.
	ingressStoreSynced cache.InformerSynced

	// Ingress keys that need to be synced.
	queue workqueue.RateLimitingInterface
}

// Send sends message to destination module which was defined in KubeedgeClient's destination field
func (ke *KubeedgeClient)Send(message string, operation string) error {}

// Receive receives message send to this module
func (ke *KubeedgeClient)Receive(message string) error {}

// NewEnvoyIngressController creates a new EnvoyIngressController
func NewEnvoyIngressController(
	ingressInformer networkingInformers.IngressInformer,
	podInformer coreinformers.PodInformer,
	nodeInformer coreinformers.NodeInformer,
	serviceInformer coreinformers.ServiceInformer,
	envoyIngressControllerConfiguration EnvoyIngressControllerConfiguration,
	kubeCLient clientset.Interface,
	enable bool,
	)(*EnvoyIngressController, error){
	eventBroadcaster := record.NewBroadcaster()
	eventBroadcaster.StartStructuredLogging(0)
	eventBroadcaster.StartRecordingToSink(&v1core.EventSinkImpl{Interface: kubeCLient.CoreV1().Events("")})

	if kubeCLient != nil && kubeCLient.CoreV1().RESTClient().GetRateLimiter() != nil{
		if err := ratelimiter.RegisterMetricAndTrackRateLimiterUsage("envoyingress_controller", kubeCLient.CoreV1().RESTClient().GetRateLimiter()); err!=nil{
			return nil, err
		}
	}
	eic := &EnvoyIngressController{
		kubeClient: kubeCLient,
		eventRecorder: eventBroadcaster.NewRecorder(scheme.Scheme, v1.EventSource{Component: "envoyingress-controller"}),
		envoyIngressControllerConfiguration: envoyIngressControllerConfiguration,
		queue: workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "envoyingress"),
	}
	ingressInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: eic.addIngress,
		UpdateFunc: eic.updateIngress,
		DeleteFunc: eic.deleteIngress,
	})
	eic.ingressLister = ingressInformer.Lister()
	eic.ingressStoreSynced = ingressInformer.Informer().HasSynced

	podInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: eic.addPod,
		UpdateFunc: eic.updatePod,
		DeleteFunc: eic.deletePod,
	})
	eic.podLister = podInformer.Lister()
	eic.podStoreSynced = podInformer.Informer().HasSynced

	nodeInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: eic.addNode,
		UpdateFunc: eic.updateNode,
		DeleteFunc: eic.deleteNode,
	})
	eic.nodeLister = nodeInformer.Lister()
	eic.nodeStoreSynced = nodeInformer.Informer().HasSynced

	serviceInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: eic.addService,
		UpdateFunc: eic.updateService,
		DeleteFunc: eic.deleteService,
	})
	eic.serviceLister = serviceInformer.Lister()
	eic.serviceStoreSynced = serviceInformer.Informer().HasSynced

	eic.enable = enable
	eic.syncHandler = eic.syncEnvoyIngress

	return eic, nil
}

// Register registers envoy ingress controller to beehive core.
func Register(eic *v1alpha1.EnvoyIngressController){
	// Get clientSet from keclient package
	kubeClient := keclient.GetKubeClient()
	sharedInformers := informers.NewSharedInformerFactory(kubeClient, time.Minute)
	ingressInformer := sharedInformers.Networking().V1().Ingresses()
	podInformer := sharedInformers.Core().V1().Pods()
	nodeInformer := sharedInformers.Core().V1().Nodes()
	serviceInformer := sharedInformers.Core().V1().Services()
	envoyIngressControllerConfiguration := EnvoyIngressControllerConfiguration{
		syncInterval: eic.SyncInterval,
		ingressSyncWorkerNumber: eic.IngressSyncWorkerNumber,
		envoyServiceSyncWorkerNumber: eic.EnvoyServiceSyncWorkerNumber,
	}
	// TODO: deal with error
	envoyIngresscontroller, _ := NewEnvoyIngressController(ingressInformer,podInformer,nodeInformer,serviceInformer,envoyIngressControllerConfiguration,kubeClient,eic.Enable)
	core.Register(envoyIngresscontroller)
}

// Name of controller
func (eic *EnvoyIngressController) Name() string {
	return modules.EnvoyIngressControllerModuleName
}

// Group of controller
func (eic *EnvoyIngressController) Group() string {
	return modules.EnvoyIngressControllerModuleGroup
}

// Enable indicates whether enable this module
func (eic *EnvoyIngressController) Enable() bool {
	return eic.enable
}

// Start starts controller
func (eic *EnvoyIngressController) Start() {}

// addIngress adds the given ingress to the queue
func (eic *EnvoyIngressController) addIngress(obj interface{}){
	ingress := obj.(*ingressv1.Ingress)
	if *(ingress.Spec.IngressClassName) != ENVOYINGRESSCONTROLLERNAME {
		klog.V(4).Infof("Ignore ingress %s, which is not a envoy ingress object", ingress.Name)
		return
	}
	klog.V(4).Infof("Adding envoy ingress %s", ingress.Name)
	eic.enqueue(ingress)
}

// updateIngress compares the uid of given ingresses and if they differences
// delete the old ingress and enqueue the new one
func (eic *EnvoyIngressController) updateIngress(cur, old interface{}){
	oldIngress := old.(*ingressv1.Ingress)
	curIngress := cur.(*ingressv1.Ingress)

	if curIngress.UID != oldIngress.UID {
		key, err := controller.KeyFunc(oldIngress)
		if err != nil {
			utilruntime.HandleError(fmt.Errorf("couldn't get key for object %#v: %v", oldIngress, err))
			return
		}
		eic.deleteIngress(cache.DeletedFinalStateUnknown{
			Key: key,
			Obj: oldIngress,
		})
	}

	// check whether the ingress object is an envoy ingress object
	if *curIngress.Spec.IngressClassName == ENVOYINGRESSCONTROLLERNAME{
		klog.V(4).Infof("Updating envoy ingress %s", oldIngress.Name)
		eic.enqueue(curIngress)
	}else{
		klog.V(4).Infof("Updating envoy ingress controller class has changed, old envoy ingress %s ", oldIngress.Name)
	}
}

// deleteIngress deletes the given ingress from queue.
func (eic *EnvoyIngressController) deleteIngress(obj interface{}){
	ingress, ok := obj.(*ingressv1.Ingress)
	if !ok {
		tombstone, ok := obj.(cache.DeletedFinalStateUnknown)
		if !ok {
			utilruntime.HandleError(fmt.Errorf("couldn't get object from tombstone %#v", obj))
			return
		}
		ingress, ok = tombstone.Obj.(*ingressv1.Ingress)
		if !ok {
			utilruntime.HandleError(fmt.Errorf("tombstone contained object that is not an ingress %#v", obj))
			return
		}
	}
	klog.V(4).Infof("Deleting ingress %s", ingress.Name)

	key, err := controller.KeyFunc(ingress)
	if err != nil {
		utilruntime.HandleError(fmt.Errorf("couldn't get key for object %#v: %v", ingress, err))
		return
	}

	eic.queue.Add(key)
}

// addPod first checks whether the pod is being deleted or has been deleted.
// If it is being deleted or has been deleted, call deletePod and return.
// Check the pod label and find if any ingress want theses pod.
func (eic *EnvoyIngressController) addPod(obj interface{}){
	pod := obj.(*v1.Pod)

	if pod.DeletionTimestamp != nil {
		eic.deletePod(pod)
		return
	}

	ingresses := eic.getIngressesForPod(pod)
	if len(ingresses) == 0{
		return
	}
	for _, ingress := range ingresses{
		eic.enqueue(ingress)
	}
}

// When a pod is updated, figure out what ingresses potentially match it.
func (eic *EnvoyIngressController) updatePod(cur, old interface{}){
	curPod := cur.(*v1.Pod)
	oldPod := old.(*v1.Pod)
	if curPod.ResourceVersion == oldPod.ResourceVersion{
		return
	}

	if curPod.DeletionTimestamp != nil {
		eic.deletePod(curPod)
		return
	}

	labelChanged := !reflect.DeepEqual(curPod.Labels, oldPod.Labels)
	if labelChanged {
		ingresses := eic.getIngressesForPod(oldPod)
		if len(ingresses) != 0{
			for _, ingress := range ingresses{
				eic.enqueue(ingress)
			}
		}

		ingresses = eic.getIngressesForPod(curPod)
		if len(ingresses) == 0{
			return
		}
		for _, ingress := range ingresses{
			eic.enqueue(ingress)
		}
	}
}

// When a pod is deleted, figure out what ingresses potentially match it.
func (eic *EnvoyIngressController) deletePod(obj interface{}){
	pod, ok := obj.(*v1.Pod)
	if !ok {
		tombstone, ok := obj.(cache.DeletedFinalStateUnknown)
		if !ok {
			utilruntime.HandleError(fmt.Errorf("couldn't get object from tombstone %#v", obj))
			return
		}
		pod, ok = tombstone.Obj.(*v1.Pod)
		if !ok {
			utilruntime.HandleError(fmt.Errorf("tombstone contained object that is not a pod %#v", obj))
			return
		}
	}
	klog.V(4).Infof("Pod %s deleted.", pod.Name)
	ingresses := eic.getIngressesForPod(pod)
	if len(ingresses) == 0{
		return
	}
	for _, ingress := range ingresses{
		eic.enqueue(ingress)
	}
}

// addNode updates the node2group and group2node map.
func (eic *EnvoyIngressController) addNode(obj interface{}){
	node := obj.(*v1.Node)
	if node.Labels != nil {
		if len(node.Labels[NODEGROUPLABEL]) != 0{
			eic.lock.Lock()
			defer eic.lock.Unlock()
			nodegroup := strings.Split(node.Labels[NODEGROUPLABEL], ";")
			if eic.node2group[node.Name] == nil{
				eic.node2group[node.Name] = make([]string, 0, 10)
			}
			for _, v := range nodegroup{
				if len(v) != 0{
					eic.node2group[node.Name]=append(eic.node2group[node.Name], v)
					if eic.group2node[v] == nil{
						eic.group2node[v] = make([]string, 0, 10)
					}
					eic.group2node[v]=append(eic.group2node[v], node.Name)
				}
			}
		}
	}
}

// Copied from daemonset controller
// nodeInSameCondition returns true if all effective types ("Status" is true) equals;
// otherwise, returns false.
func nodeInSameCondition(old []v1.NodeCondition, cur []v1.NodeCondition) bool {
	if len(old) == 0 && len(cur) == 0 {
		return true
	}

	c1map := map[v1.NodeConditionType]v1.ConditionStatus{}
	for _, c := range old {
		if c.Status == v1.ConditionTrue {
			c1map[c.Type] = c.Status
		}
	}

	for _, c := range cur {
		if c.Status != v1.ConditionTrue {
			continue
		}

		if _, found := c1map[c.Type]; !found {
			return false
		}

		delete(c1map, c.Type)
	}

	return len(c1map) == 0
}

// Copied from daemonset controller
func shouldIgnoreNodeUpdate(oldNode, curNode v1.Node) bool {
	if !nodeInSameCondition(oldNode.Status.Conditions, curNode.Status.Conditions) {
		return false
	}
	oldNode.ResourceVersion = curNode.ResourceVersion
	oldNode.Status.Conditions = curNode.Status.Conditions
	return apiequality.Semantic.DeepEqual(oldNode, curNode)
}

// updateNode updates the node2group and group2node map.
func (eic *EnvoyIngressController) updateNode(cur, old interface{}){
	oldNode := old.(*v1.Node)
	curNode := cur.(*v1.Node)
	if shouldIgnoreNodeUpdate(*oldNode, *curNode){
		return
	}

	if curNode.Labels[NODEGROUPLABEL] != oldNode.Labels[NODEGROUPLABEL]{
		eic.deleteNode(oldNode)
		eic.addNode(curNode)
	}
}

// deleteNode updates the node2group and group2node map.
func (eic *EnvoyIngressController) deleteNode(obj interface{}){
	node, ok := obj.(*v1.Node)

	if !ok {
		tombstone, ok := obj.(cache.DeletedFinalStateUnknown)
		if !ok {
			utilruntime.HandleError(fmt.Errorf("couldn't get object from tombstone %#v", obj))
			return
		}
		node, ok = tombstone.Obj.(*v1.Node)
		if !ok {
			utilruntime.HandleError(fmt.Errorf("tombstone contained object that is not a node %#v", obj))
			return
		}
	}

	if len(node.Labels[NODEGROUPLABEL]) != 0{
		eic.lock.Lock()
		defer eic.lock.Unlock()
		nodegroup := strings.Split(node.Labels[NODEGROUPLABEL], ";")
		delete(eic.node2group, node.Name)
		for _, v := range nodegroup{
			//delete the old relationship between this node and group
			if len(v) != 0 {
				nodeList := []string{}
				for _, nodeName := range eic.group2node[v]{
					if nodeName == node.Name{
						continue
					}
					nodeList = append(nodeList, nodeName)
				}
				eic.group2node[v] = nodeList
			}
		}
	}
}

// When a service is added, figure out what ingresses potentially match it.
func (eic *EnvoyIngressController) addService(obj interface{}){
	service := obj.(*v1.Service)

	ingresses := eic.getIngressesForService(service)
	if len(ingresses) == 0{
		return
	}
	for _, ingress := range ingresses{
		eic.enqueue(ingress)
	}
}

// When a service is updated, figure out what ingresses potentially match it.
func (eic *EnvoyIngressController) updateService(cur, old interface{}){
	oldService := old.(*v1.Service)
	curService := cur.(*v1.Service)

	if curService.UID != oldService.UID {
		selectorChanged := !reflect.DeepEqual(curService.Spec.Selector, oldService.Spec.Selector)
		if selectorChanged {
			klog.V(4).Infof("service %v's selector has changed", oldService.Name)
			eic.deleteService(oldService)
			eic.addService(curService)
		}
	}
}

// When a service is deleted, figure out what ingresses potentially match it.
func (eic *EnvoyIngressController) deleteService(obj interface{}){
	service, ok := obj.(*v1.Service)
	if !ok {
		tombstone, ok := obj.(cache.DeletedFinalStateUnknown)
		if !ok {
			utilruntime.HandleError(fmt.Errorf("couldn;t get object from tombstone %#v", obj))
			return
		}
		service, ok = tombstone.Obj.(*v1.Service)
		if !ok {
			utilruntime.HandleError(fmt.Errorf("tombstone contained object that is not a service %#v", obj))
			return
		}
	}

	klog.V(4).Infof("Service %s deleted.", service.Name)
	ingresses := eic.getIngressesForService(service)
	if len(ingresses) == 0{
		return
	}
	for _, ingress := range ingresses{
		eic.enqueue(ingress)
	}
}

// Run begins watching and syncing ingresses.
func (eic *EnvoyIngressController) Run(workers int, stopCh <-chan struct{}) {
	defer utilruntime.HandleCrash()
	defer eic.queue.ShutDown()

	klog.Infof("Starting envoy ingress controller")
	defer klog.Infof("Shutting down envoy ingress controller")

	if !cache.WaitForNamedCacheSync("envoy ingress", stopCh, eic.podStoreSynced, eic.nodeStoreSynced, eic.serviceStoreSynced, eic.ingressStoreSynced){
		return
	}

	for i := 0; i < eic.envoyIngressControllerConfiguration.ingressSyncWorkerNumber; i++ {
		go wait.Until(eic.runIngressWorkers, eic.envoyIngressControllerConfiguration.syncInterval, stopCh)
	}

	<-stopCh
}

func (eic *EnvoyIngressController) runIngressWorkers(){
	for eic.processNextIngressWorkItem(){
	}
}



// processNextIngressWorkItem deals with one key off the queue.  It returns false when it's time to quit.
func (eic *EnvoyIngressController) processNextIngressWorkItem() bool {
	ingressKey, quit := eic.queue.Get()
	if quit {
		return false
	}
	defer eic.queue.Done(ingressKey)

	err := eic.syncHandler(ingressKey.(string))
	if err == nil {
		eic.queue.Forget(ingressKey)
		return true
	}

	utilruntime.HandleError(fmt.Errorf("%v failed with: %v", ingressKey, err))
	eic.queue.AddRateLimited(ingressKey)

	return true
}



func (eic *EnvoyIngressController) enqueue(ingress *ingressv1.Ingress) {
	// ingore ingresses which mismatch the controller type
	if *ingress.Spec.IngressClassName != ENVOYINGRESSCONTROLLERNAME {
		return
	}
	key, err := controller.KeyFunc(ingress)
	if err != nil {
		utilruntime.HandleError(fmt.Errorf("Couldn't get key for object %#v: %v", ingress, err))
		return
	}

	eic.queue.Add(key)
}

func (eic *EnvoyIngressController) enqueueEnvoyIngressAfter(obj interface{}, after time.Duration) {
	ingress, ok := obj.(*ingressv1.Ingress)
	if !ok {
		utilruntime.HandleError(fmt.Errorf("Cloudn't convert obj into ingress, obj:%#v", obj))
	}
	if *ingress.Spec.IngressClassName != ENVOYINGRESSCONTROLLERNAME {
		return
	}

	key, err := controller.KeyFunc(obj)
	if err != nil {
		utilruntime.HandleError(fmt.Errorf("Couldn't get key for object %+v: %v", obj, err))
		return
	}

	eic.queue.AddAfter(key, after)
}




func (eic *EnvoyIngressController) storeIngressStatus(){}

func (eic *EnvoyIngressController) updateIngressStatus(){}

// getIngressesForPod returns a list of ingresses that potentially match the pod
func (eic *EnvoyIngressController) getIngressesForPod(pod *v1.Pod) []*ingressv1.Ingress {
	if pod == nil {
		return nil
	}
	services := eic.getServicesForPod(pod)
	if len(services) == 0 {
		return nil
	}
	var ingresses []*ingressv1.Ingress
	var tmpIngresses []*ingressv1.Ingress
	for _, service := range services{
		tmpIngresses = eic.getIngressesForService(service)
		if len(tmpIngresses) == 0 {
			continue
		}
		ingresses = append(ingresses, tmpIngresses...)
	}
	if len(ingresses) == 0{
		return nil
	}
	return ingresses
}

// getServicesForPod returns a list of services that potentially match the pod
func (eic *EnvoyIngressController) getServicesForPod(pod *v1.Pod) []*v1.Service {
	var selector labels.Selector

	if pod == nil{
		return nil
	}
	if len(pod.Labels) == 0{
		// If the pod has no label, it can't be bound to a service
		return nil
	}

	list, err := eic.serviceLister.Services(pod.Namespace).List(labels.Everything())
	if err != nil {
		utilruntime.HandleError(fmt.Errorf("Failed to list all the services in cluster for pod: %v", pod.Name))
		return nil
	}

	var services []*v1.Service
	for _, service := range list{
		var labelSelector *metav1.LabelSelector
		if service.Namespace != pod.Namespace{
			continue
		}
		err = metav1.Convert_Map_string_To_string_To_v1_LabelSelector(&service.Spec.Selector, labelSelector, nil)
		if err != nil {
			utilruntime.HandleError(fmt.Errorf("Failed to convert service %v's selector into label selector", service.Name))
			return nil
		}
		selector, err = metav1.LabelSelectorAsSelector(labelSelector)
		if err != nil {
			// this should not happen if the DaemonSet passed validation
			return nil
		}

		//If a service with a nil or empty selector creeps in, it should match nothing, not everything
		if selector.Empty() || !selector.Matches(labels.Set(pod.Labels)) {
			continue
		}
		services = append(services, service)
	}

	if len(services) == 0 {
		return nil
	}

	return services
}

// getIngressesForService returns a list of ingresses that potentially match the service
func (eic *EnvoyIngressController) getIngressesForService(service *v1.Service) []*ingressv1.Ingress {
	if service == nil{
		return nil
	}
	if len(service.Spec.Selector) == 0 {
		return nil
	}
	// TODO: check ingress

	list, err := eic.ingressLister.Ingresses(service.Namespace).List(labels.Everything())
	if err != nil {
		utilruntime.HandleError(fmt.Errorf("Cloudn't get ingresses for service %#v, err: %v", service.Name, err))
		return nil
	}

	var ingresses []*ingressv1.Ingress
	for _, ingress := range list{
		isIngressMatchService := false
		if ingress.Namespace != service.Namespace {
			continue
		}
		if ingress.Spec.DefaultBackend.Service.Name == service.Name{
			isIngressMatchService = true
		}
		if len(ingress.Spec.Rules) != 0 && !isIngressMatchService {
			RuleLoop:
			for _, rule := range ingress.Spec.Rules {
				if len(rule.IngressRuleValue.HTTP.Paths) != 0 {
					for _, path := range rule.IngressRuleValue.HTTP.Paths {
						if path.Backend.Service.Name == service.Name{
							isIngressMatchService = true
							break RuleLoop
						}
					}
				}else if len(rule.HTTP.Paths) != 0 {
					for _, path := range rule.HTTP.Paths {
						if path.Backend.Service.Name == service.Name{
							isIngressMatchService = true
							break RuleLoop
						}
					}
				}
			}
		}
		ingresses = append(ingresses, ingress)
	}

	if len(ingresses) == 0 {
		return nil
	}

	return ingresses
}

// getServicesForIngress returns a list of services that potentially match the ingress.
func (eic *EnvoyIngressController) getServicesForIngress(ingress ingressv1.Ingress) ([]*v1.Service, error) {
	var services []*v1.Service
	var isServiceMatchIngress bool
	list, err := eic.serviceLister.Services(ingress.Namespace).List(labels.Everything())
	if err != nil {
		utilruntime.HandleError(fmt.Errorf("Cloudn't get services fro ingress:%#v", ingress))
		return nil, err
	}
	for _, service := range list {
		isServiceMatchIngress = false
		if service.Namespace != ingress.Namespace {
			continue
		}
		if service.Name == ingress.Spec.DefaultBackend.Service.Name {
			isServiceMatchIngress = true
		}
		if !isServiceMatchIngress && len(ingress.Spec.Rules) != 0{
			RuleLoop:
			for _, rule := range ingress.Spec.Rules {
				if len(rule.IngressRuleValue.HTTP.Paths) != 0 {
					for _, path := range rule.IngressRuleValue.HTTP.Paths {
						if path.Backend.Service.Name == service.Name{
							isServiceMatchIngress = true
							break RuleLoop
						}
					}
				}else if len(rule.HTTP.Paths) != 0 {
					for _, path := range rule.HTTP.Paths {
						if path.Backend.Service.Name == service.Name{
							isServiceMatchIngress = true
							break RuleLoop
						}
					}
				}
			}
		}
		services = append(services, service)
	}

	if len(services) == 0 {
		return nil, fmt.Errorf("could not find services for ingress %s in namespace %s", ingress.Name, ingress.Namespace)
	}

	return services, nil
}

// getPodsForService returns a list for pods that potentially match the service.
func (eic *EnvoyIngressController) getPodsForService(service *v1.Service) ([]*v1.Pod, error) {
	var selector labels.Selector
	var pod *v1.Pod

	if len(service.Spec.Selector) == 0 {
		return nil, fmt.Errorf("Service %s has no selector", service.Name)
	}

	list, err := eic.podLister.Pods(service.Namespace).List(labels.Everything())
	if err != nil {
		return nil, fmt.Errorf("Couldn't get pod for service %s in namespace %s", service.Name, service.Namespace)
	}

	var pods []*v1.Pod
	for _, pod = range list{
		var labelSelector *metav1.LabelSelector
		if service.Namespace != pod.Namespace{
			continue
		}
		err = metav1.Convert_Map_string_To_string_To_v1_LabelSelector(&service.Spec.Selector, labelSelector, nil)
		if err != nil {
			utilruntime.HandleError(fmt.Errorf("Failed to convert service %v's selector into label selector", service.Name))
			return nil, err
		}
		selector, err = metav1.LabelSelectorAsSelector(labelSelector)
		if err != nil {
			// this should not happen if the DaemonSet passed validation
			return nil, err
		}
		if selector.Empty() || !selector.Matches(labels.Set(pod.Labels)) {
			continue
		}
		pods = append(pods, pod)
	}

	if len(pods) == 0 {
		return nil, fmt.Errorf("Couldn't find pod for service %s in namespace %s", service.Name, service.Namespace)
	}

	return pods, nil
}

// TODO: sending envoy objects to edge will make it different to manage the objects. Need considering construct a object which is k8s style
func (eic *EnvoyIngressController) getClustersFromIngress(ingress ingressv1.Ingress) ([]*envoyv2.Cluster, error) {
	services, err := eic.getServicesForIngress(ingress)
	if err != nil {
		return nil, err
	}

	type Endpoint struct{
		Address string
		Port int
	}
	var clusters []*envoyv2.Cluster
	var cluster *envoyv2.Cluster
	for _, service := range services {
		pods, err := eic.getPodsForService(service)
		if err != nil {
			return nil, err
		}
		var lbEndpoints []*endpoint.LbEndpoint
		var lbEndpoint *endpoint.LbEndpoint
		for _, pod := range pods {
			var endpoints []Endpoint
			if len(pod.Status.PodIPs) == 0 {
				continue
			}
			for _, port := range service.Spec.Ports{
				portNum, err := podutil.FindPort(pod, &port)
				if err != nil {
					klog.V(4).Infof("Failed to find port for service %s/%s: %v", service.Namespace, service.Name, err)
					return nil, err
				}
				endpoint := Endpoint{
					Address: pod.Status.PodIP,
					Port: portNum,
				}
				endpoints = append(endpoints, endpoint)
			}
			for _, v := range endpoints {
				lbEndpoint = &endpoint.LbEndpoint{
					HostIdentifier: &endpoint.LbEndpoint_Endpoint{
						Endpoint: &endpoint.Endpoint{
							Address: &envoy_api_v2_core.Address{
								Address: &envoy_api_v2_core.Address_SocketAddress{
									SocketAddress: &envoy_api_v2_core.SocketAddress{
										Protocol: envoy_api_v2_core.SocketAddress_TCP,
										Address: v.Address,
										PortSpecifier: &envoy_api_v2_core.SocketAddress_PortValue{
											PortValue: uint32(v.Port),
										},
									},
								},
							},
						},
					},
				}
				lbEndpoints = append(lbEndpoints,lbEndpoint)
			}
		}
		cluster = &envoyv2.Cluster{
			Name: service.Namespace+"-"+service.Name,
			ConnectTimeout: &duration.Duration{Seconds: 1},
			ClusterDiscoveryType: &envoyv2.Cluster_Type{envoyv2.Cluster_STATIC},
			LbPolicy: envoyv2.Cluster_ROUND_ROBIN,
			LoadAssignment: &envoyv2.ClusterLoadAssignment{
				ClusterName: service.Namespace+"-"+service.Name+"lb",
				Endpoints: []*endpoint.LocalityLbEndpoints{
					&endpoint.LocalityLbEndpoints{
						LbEndpoints: lbEndpoints,
					},
				},
			},
		}
		clusters = append(clusters, cluster)
	}
	if len(clusters) == 0 {
		return nil, fmt.Errorf("Cloudn't get clusters for ingress %v in namespace %v", ingress.Name, ingress.Namespace)
	}

	return clusters, nil
}

func (eic *EnvoyIngressController) getListenerFromIngress(ingress ingressv1.Ingress, clusters []*envoyv2.Cluster) ([]*envoyv2.Listener, error) {
	// need considering using tls for connecting to listeners
	var tlsEnable = false
	if len(ingress.Spec.TLS) != 0 {
		tlsEnable = true
	}

	httpFilterRouter := &http_router.Router{
		DynamicStats: &wrapperspb.BoolValue{
			Value: true,
		},
	}
	httpFilterRouterStuct, err := conversion.MessageToStruct(httpFilterRouter)
	if err != nil {
		klog.Infof("Failed to convert httpFilterRouter message to struct")
		return nil, err
	}

	var listener  *envoyv2.Listener
	var listeners []*envoyv2.Listener
	var virtualHost *envoy_api_v2_route.VirtualHost
	var virtualHosts []*envoy_api_v2_route.VirtualHost
	var routes []*envoy_api_v2_route.Route
	//1.deal with ingress's path
	for _, rule := range ingress.Spec.Rules {
		for _, path := range rule.HTTP.Paths {
			service, err := eic.serviceLister.Services(ingress.Namespace).Get(path.Backend.Service.Name)
			if err != nil {
				klog.V(4).Infof("Failed to get service %s in namespace %s from service lister", service.Name, service.Namespace)
				continue
			}
			clusterName := service.Namespace+"-"+service.Name
			var route *envoy_api_v2_route.Route
			switch *path.PathType {
			case ingressv1.PathTypeExact:
				route = &envoy_api_v2_route.Route{
					Match: &envoy_api_v2_route.RouteMatch{
						PathSpecifier: &envoy_api_v2_route.RouteMatch_Path{
							Path: path.Path,
						},
						CaseSensitive: &wrapperspb.BoolValue{
							Value: false,
						},
					},
					Action: &envoy_api_v2_route.Route_Route{
						Route: &envoy_api_v2_route.RouteAction{
							ClusterSpecifier: &envoy_api_v2_route.RouteAction_Cluster{
								Cluster: clusterName,
							},
						},
					},
				}
			case ingressv1.PathTypePrefix:
				route = &envoy_api_v2_route.Route{
					Match: &envoy_api_v2_route.RouteMatch{
						PathSpecifier: &envoy_api_v2_route.RouteMatch_Prefix{
							Prefix: path.Path,
						},
						CaseSensitive: &wrapperspb.BoolValue{
							Value: false,
						},
					},
					Action: &envoy_api_v2_route.Route_Route{
						Route: &envoy_api_v2_route.RouteAction{
							ClusterSpecifier: &envoy_api_v2_route.RouteAction_Cluster{
								Cluster: clusterName,
							},
						},
					},
				}
			case ingressv1.PathTypeImplementationSpecific:
				//In our envoy ingress controller, this field refers to envoy listener's regex field
				route = &envoy_api_v2_route.Route{
					Match: &envoy_api_v2_route.RouteMatch{
						PathSpecifier: &envoy_api_v2_route.RouteMatch_SafeRegex{
							SafeRegex: &matcher.RegexMatcher{
								EngineType: &matcher.RegexMatcher_GoogleRe2{
									&matcher.RegexMatcher_GoogleRE2{},
								},
								Regex: clusterName,
							},
						},
						CaseSensitive: &wrapperspb.BoolValue{
							Value: false,
						},
					},
					Action: &envoy_api_v2_route.Route_Route{
						Route: &envoy_api_v2_route.RouteAction{
							ClusterSpecifier: &envoy_api_v2_route.RouteAction_Cluster{
								Cluster: clusterName,
							},
						},
					},
				}
			}
			routes = append(routes, route)
		}
		virtualHost = &envoy_api_v2_route.VirtualHost{
			Name: rule.Host+"vh",
			Domains: []string{
				rule.Host,
			},
			Routes: routes,
		}
		listenFilterHttpConn := &http_conn_manager.HttpConnectionManager{
			StatPrefix: "envoy_ingress_http",
			RouteSpecifier: &http_conn_manager.HttpConnectionManager_RouteConfig{
				RouteConfig: &envoyv2.RouteConfiguration{
					Name: ingress.Namespace+"-"+ingress.Name+"-RouteConfig",
					VirtualHosts: []*envoy_api_v2_route.VirtualHost{
						virtualHost,
					},
				},
			},
			HttpFilters: []*http_conn_manager.HttpFilter{
				&http_conn_manager.HttpFilter{
					Name: "envoy.router",
					ConfigType: &http_conn_manager.HttpFilter_Config{
						Config: httpFilterRouterStuct,
					},
				},
			},
		}
		listenFilterHttpConnStruct, err := conversion.MessageToStruct(listenFilterHttpConn)
		if err != nil {
			klog.Infof("Failed to convert listenFilterHttpConn message to struct")
			return nil, err
		}
		listener = &envoyv2.Listener{
			Name: "listener_with_static_route_port_"+rule.,
		}
	}
}

func (eic *EnvoyIngressController) getEnvoyServiceForIngress(ingress ingressv1.Ingress) ([]*v1alpha1.EnvoyService, error) {}

func (eic *EnvoyIngressController) syncEnvoyIngress(key string) error {}

func (eic *EnvoyIngressController) syncEnvoyService(key string) error {}

// syncNodeGroupsWithNodes should be called first when the controller begins to run.
// It list all nodes in the cluster and read the labels of nodes to build relationship of node and there group.
func (eic *EnvoyIngressController) syncNodeGroupsWithNodes() error {}