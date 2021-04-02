package envoyingresscontroller

import (
	"fmt"
	beehiveContext "github.com/kubeedge/beehive/pkg/core/context"
	"github.com/kubeedge/kubeedge/cloud/pkg/common/modules"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/informers"
	coreinformers "k8s.io/client-go/informers/core/v1"
	clientset "k8s.io/client-go/kubernetes"
	v1core "k8s.io/client-go/kubernetes/typed/core/v1"
	appslisters "k8s.io/client-go/listers/apps/v1"
	corelisters "k8s.io/client-go/listers/core/v1"
	networkingListers "k8s.io/client-go/listers/networking/v1"
	networkingInformers "k8s.io/client-go/informers/networking/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/component-base/metrics/prometheus/ratelimiter"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/kubernetes/pkg/controller"
	"time"
	"github.com/kubeedge/beehive/pkg/core/model"
	ingressv1 "k8s.io/api/networking/v1"
	v1 "k8s.io/api/core/v1"
	envoyv2 "github.com/envoyproxy/go-control-plane/envoy/api/v2"
	"github.com/kubeedge/kubeedge/cloud/pkg/envoyingresscontroller/config/v1alpha1"
	"github.com/kubeedge/beehive/pkg/core"
	keclient "github.com/kubeedge/kubeedge/cloud/pkg/common/client"
	"k8s.io/klog/v2"
)

// TODO: Need consider situations where ingress contains configmap object

const(
	ENVOYINGRESSCONTROLLERNAME = "envoyingress"
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
	clusterSyncWorkerNumber int
	listenerSyncWorkerNumber int
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

	// cluster keys that need to be synced.
	clusterQueue workqueue.RateLimitingInterface

	// listener keys that need to be synced.
	listenerQueue workqueue.RateLimitingInterface
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
		clusterQueue: workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "envoyingress-cluster"),
		listenerQueue: workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "envoyingress-listener"),
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
		clusterSyncWorkerNumber: eic.ClusterSyncWorkerNumber,
		listenerSyncWorkerNumber: eic.ListenerSyncWorkerNumber,
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
func (eic *EnvoyIngressController) updateIngress(cur, old interface){
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
func (eic *EnvoyIngressController) deleteIngress(obj interface){
	ingress, ok := obj.(*ingressv1.Ingress)
	if !ok {
		tombstone, ok := obj.(cache.DeletedFinalStateUnknown)
		if !ok {
			utilruntime.HandleError(fmt.Errorf("couldn't get object from tombstone %#v", obj))
			return
		}
		ingress, ok := tombstone.Obj.(*ingressv1.Ingress)
		if !ok {
			utilruntime.HandleError(fmt.Errorf("tombstone contained object that is not an ingress %#v", obj))
			return
		}
	}
	klog.V(4).Infof("Deleting ingress %s", ingress.Name)

	key, err := controller.KeyFunc(Ingress)
	if err != nil {
		utilruntime.HandleError(fmt.Errorf("couldn't get key for object %#v: %v", ds, err))
		return
	}

	eic.queue.Add(ingress)
}

func (eic *EnvoyIngressController) addPod(obj interface{}){}

func (eic *EnvoyIngressController) updatePod(cur, old interface){}

func (eic *EnvoyIngressController) deletePod(obj interface){}

func (eic *EnvoyIngressController) addNode(obj interface{}){}

func (eic *EnvoyIngressController) updateNode(cur, old interface){}

func (eic *EnvoyIngressController) deleteNode(obj interface){}

func (eic *EnvoyIngressController) addService(obj interface{}){}

func (eic *EnvoyIngressController) updateService(cur, old interface){}

func (eic *EnvoyIngressController) deleteService(obj interface){}

// Run begins watching and syncing ingresses.
func (eic *EnvoyIngressController) Run(workers int, stopCh <-chan struct{}) {}

func (eic *EnvoyIngressController) runIngressWorkers(){}

func (eic *EnvoyIngressController) runClusterWorkers(){}

func (eic *EnvoyIngressController) runListenerWorkers(){}

// processNextIngressWorkItem deals with one key off the queue.  It returns false when it's time to quit.
func (eic *EnvoyIngressController) processNextIngressWorkItem() bool {}

// processNextClusterWorkItem deals with one key off the queue.  It returns false when it's time to quit.
func (eic *EnvoyIngressController) processNextClusterWorkItem() bool {}

// processNextListenerWorkItem deals with one key off the queue.  It returns false when it's time to quit.
func (eic *EnvoyIngressController) processNextListenerWorkItem() bool {}

func (eic *EnvoyIngressController) enqueue(ingress *ingressv1.Ingress) {}

func (eic *EnvoyIngressController) enqueueEnvoyIngressAfter(obj interface{}, after time.Duration) {}

func (eic *EnvoyIngressController) clusterEnqueue() {}

func (eic *EnvoyIngressController) enqueueClusterAfter(obj interface{}, after time.Duration) {}

func (eic *EnvoyIngressController) listenerEnqueue() {}

func (eic *EnvoyIngressController) enqueueListenerAfter(obj interface{}, after time.Duration) {}

func (eic *EnvoyIngressController) storeIngressStatus(){}

func (eic *EnvoyIngressController) updateIngressStatus(){}

// getServicesForIngress returns a list of services that potentially match the ingress.
func (eic *EnvoyIngressController) getServicesForIngress(ingress ingressv1.Ingress) ([]*v1.Service, error) {}

// getPodsForService returns a list for pods that potentially match the service.
func (eic *EnvoyIngressController) getPodsForService(service v1.Service) ([]*v1.Pod, error) {}

func (eic *EnvoyIngressController) getIngress(key string) error {}

func (eic *EnvoyIngressController) getClustersFromIngress(ingress ingressv1.Ingress) ([]*envoyv2.Cluster, error) {}

func (eic *EnvoyIngressController) getListenerFromIngress(ingress ingressv1.Ingress) ([]*envoyv2.Listener, error) {}

func (eic*EnvoyIngressController) syncEnvoyIngress(key string) error {}
