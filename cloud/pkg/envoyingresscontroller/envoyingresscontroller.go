// Copyright Project Contour Authors
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
//@CHANGELOG The HarmonyCloud Authors:
// Thanks to the contour project authors. We have used their envoy related functions to write this controller.

package envoyingresscontroller

import (
	"encoding/base64"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	keinformers "github.com/kubeedge/kubeedge/cloud/pkg/common/informers"
	envoy_cache "github.com/kubeedge/kubeedge/cloud/pkg/envoyingresscontroller/cache"
	"github.com/kubeedge/kubeedge/cloud/pkg/envoyingresscontroller/constants"

	"github.com/golang/protobuf/proto"

	envoy_cluster_v3 "github.com/envoyproxy/go-control-plane/envoy/config/cluster/v3"
	envoy_endpoint_v3 "github.com/envoyproxy/go-control-plane/envoy/config/endpoint/v3"
	envoy_listener_v3 "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	envoy_route_v3 "github.com/envoyproxy/go-control-plane/envoy/config/route/v3"
	http "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/http_connection_manager/v3"
	matcher "github.com/envoyproxy/go-control-plane/envoy/type/matcher/v3"
	"github.com/envoyproxy/go-control-plane/pkg/wellknown"
	"github.com/golang/protobuf/ptypes/any"
	beehiveContext "github.com/kubeedge/beehive/pkg/core/context"
	"github.com/kubeedge/kubeedge/cloud/pkg/envoyingresscontroller/config"
	"github.com/kubeedge/kubeedge/cloud/pkg/envoyingresscontroller/messagelayer"
	"github.com/kubeedge/kubeedge/pkg/apis/componentconfig/cloudcore/v1alpha1"
	"google.golang.org/protobuf/types/known/wrapperspb"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/wait"

	envoy_v3_tls "github.com/envoyproxy/go-control-plane/envoy/extensions/transport_sockets/tls/v3"
	"github.com/kubeedge/beehive/pkg/core"
	"github.com/kubeedge/beehive/pkg/core/model"
	keclient "github.com/kubeedge/kubeedge/cloud/pkg/common/client"
	"github.com/kubeedge/kubeedge/cloud/pkg/common/modules"
	v1 "k8s.io/api/core/v1"
	ingressv1 "k8s.io/api/networking/v1"
	v1beta1Ingressv1 "k8s.io/api/networking/v1beta1"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	coreinformers "k8s.io/client-go/informers/core/v1"
	networkingInformers "k8s.io/client-go/informers/networking/v1"
	v1beta1networkInformers "k8s.io/client-go/informers/networking/v1beta1"
	clientset "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	v1core "k8s.io/client-go/kubernetes/typed/core/v1"
	corelisters "k8s.io/client-go/listers/core/v1"
	networkingListers "k8s.io/client-go/listers/networking/v1"
	v1beta1NetworkingListers "k8s.io/client-go/listers/networking/v1beta1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/component-base/metrics/prometheus/ratelimiter"
	"k8s.io/klog/v2"
	"k8s.io/kubernetes/pkg/controller"
)

const (
	ENVOY_HTTP_LISTENER  = "ingress_http"
	ENVOY_HTTPS_LISTENER = "ingress_https"

	DEFAULT_HTTP_LISTENER_ADDRESS = "0.0.0.0"
	DEFAULT_HTTP_LISTENER_PORT    = 8080

	ENVOYINGRESSCONTROLLERNAME = "envoyingress"

	// envoy ingress should have this annotation which indicates the node group to send to
	ENVOYINGRESSNODEGROUPANNOTATION = "v1alpha1.kubeedge.io/nodegroup"
	// service should have this annotation if the service connects to upstream uses httpprotocol
	// supports "tls", "h2", "h2c"
	SERVICEHTTPPROTOCOLANNOTATION = "v1alpha1.kubeedge.io/httpprotocol"
	// defines which path to do health check
	SERVICEHEALTHCHECKPATHANNOTATION = "v1alpha1.kubeedge.io/healthcheck"
	INGRESSCLASSANNOTATION           = "kubernetes.io/ingress.class"

	NODEGROUPLABEL = "nodegroup"
	GROUPRESOURCE  = "envoy"

	//ENVOYMANAGEMENTSERVER is the name of the edge side envoy control plane
	ENVOYMANAGEMENTSERVER = "envoymanagementserver"

	// LoadBalancerPolicyWeightedLeastRequest specifies the backend with least
	// active requests will be chosen by the load balancer.
	LoadBalancerPolicyWeightedLeastRequest = "WeightedLeastRequest"

	// LoadBalancerPolicyRandom denotes the load balancer will choose a random
	// backend when routing a request.
	LoadBalancerPolicyRandom = "Random"

	// LoadBalancerPolicyRoundRobin denotes the load balancer will route
	// requests in a round-robin fashion among backend instances.
	LoadBalancerPolicyRoundRobin = "RoundRobin"

	// LoadBalancerPolicyCookie denotes load balancing will be performed via a
	// Contour specified cookie.
	LoadBalancerPolicyCookie = "Cookie"

	// LoadBalancerPolicyRequestHash denotes request attribute hashing is used
	// to make load balancing decisions.
	LoadBalancerPolicyRequestHash = "RequestHash"

	CACertificateKey = "ca.crt"

	HTTPFilterRouter = "type.googleapis.com/envoy.extensions.filters.http.router.v3.Router"

	HTTPVersionAuto HTTPVersionType = http.HttpConnectionManager_AUTO
	HTTPVersion1    HTTPVersionType = http.HttpConnectionManager_HTTP1
	HTTPVersion2    HTTPVersionType = http.HttpConnectionManager_HTTP2
	HTTPVersion3    HTTPVersionType = http.HttpConnectionManager_HTTP3

	TCP_KEEPIDLE  = 0x4 // Linux syscall.TCP_KEEPIDLE
	TCP_KEEPINTVL = 0x5 // Linux syscall.TCP_KEEPINTVL
	TCP_KEEPCNT   = 0x6 // Linux syscall.TCP_KEEPCNT

	// The following are Linux syscall constants for all
	// architectures except MIPS.
	SOL_SOCKET   = 0x1
	SO_KEEPALIVE = 0x9

	// IPPROTO_TCP has the same value across Go platforms, but
	// is defined here for consistency.
	IPPROTO_TCP = syscall.IPPROTO_TCP
)

// TODO: need to add a method for sending all resources to a given node
// TODO: use RWMutex's rlock() runlock() in read situations

type HTTPVersionType = http.HttpConnectionManager_CodecType

//KubeedgeClient is used for sending message to and from cloudhub.
//It's communication is based upon beehive.
type KubeedgeClient struct {
	Source      string
	Destination string
}

//EICConfiguration's field affects how controller works
type EICConfiguration struct {
	syncInterval             time.Duration
	envoyServiceSyncInterval time.Duration
	//envoy related fields
	ingressSyncWorkerNumber      int
	envoyServiceSyncWorkerNumber int
}

// EnvoyIngressController is responsible for converting envoy ingress to envoy configuration
// and synchronizing it to cloudhub which will dispatch it's received objects to edgehub.
type EnvoyIngressController struct {
	enable         bool
	kubeClient     clientset.Interface
	kubeedgeClient KubeedgeClient

	envoyIngressControllerConfiguration EICConfiguration

	eventRecorder record.EventRecorder

	// To allow injection for testing.
	syncHandler func(key string) error

	lc *envoy_cache.LocationCache

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
	// v1beta1IngressLister can list/get v1beta1 ingresses from the shared informer's store
	v1beta1IngressLister v1beta1NetworkingListers.IngressLister
	// v1beta1IngressStoreSynced returns true if the v1beta1 ingress store has been synced at least once.
	// Added as a member to the struct to allow injection for testing.
	v1beta1IngressStoreSynced cache.InformerSynced
	// endpointLister can list/get endpoints from the shared informer's store
	endpointLister corelisters.EndpointsLister
	// endpointStoreSynced returns true if the endpoint store has been synced at least once.
	// Added as a member to the struct to allow injection for testing.
	endpointStoreSynced cache.InformerSynced
	// secretLister can list/get secrets from the shared informer's store
	secretLister corelisters.SecretLister
	// secretStoreSynced returns true if the secret store has been synced at least once.
	// Added as a member to the struct to allow injection for testing.
	secretStoreSynced cache.InformerSynced
	// secretStore saves all the converted envoy secrets in it
	secretStore     map[string]*EnvoySecret
	secretStoreLock sync.RWMutex
	// endpointStore saves all the converted envoy endpoints in it
	endpointStore     map[string]*EnvoyEndpoint
	endpointStoreLock sync.RWMutex
	// clusterStore saves all the converted envoy clusters in it
	clusterStore     map[string]*EnvoyCluster
	clusterStoreLock sync.RWMutex
	// routeStore saves all the converted envoy route object in it
	routeStore     map[string]*EnvoyRoute
	routeStoreLock sync.RWMutex
	// listener saves all the converted envoy listener object in it
	listenerStore     map[string]*EnvoyListener
	listenerStoreLock sync.RWMutex
	// resourceNeedToBeSentToEdgeStore saves all the converted envoy objects
	// which needed to be sent to edge
	resourceNeedToBeSentToEdgeStore     []EnvoyResource
	resourceNeedToBeSentToEdgeStoreLock sync.RWMutex
	// ingressToResourceNameStore save the relationship of ingress to envoy objects
	ingressToResourceNameStore     map[string][]EnvoyResource
	ingressToResourceNameStoreLock sync.RWMutex
	// ingressNodeGroupStore saves the nodegroups which the ingress belongs to
	ingressNodeGroupStore     map[string][]envoy_cache.NodeGroup
	ingressNodeGroupStoreLock sync.RWMutex
	// messageLayer is responsible for sending messages to cloudhub
	messageLayer messagelayer.MessageLayer

	// Ingress keys that need to be synced.
	queue workqueue.RateLimitingInterface
}

func initializeFields(eic *EnvoyIngressController) {
	eic.lc = &envoy_cache.LocationCache{}
	eic.lc.Node2group = make(map[string][]envoy_cache.NodeGroup)
	eic.lc.Group2node = make(map[envoy_cache.NodeGroup][]string)
	eic.secretStore = make(map[string]*EnvoySecret)
	eic.endpointStore = make(map[string]*EnvoyEndpoint)
	eic.clusterStore = make(map[string]*EnvoyCluster)
	eic.routeStore = make(map[string]*EnvoyRoute)
	eic.listenerStore = make(map[string]*EnvoyListener)
	eic.ingressToResourceNameStore = make(map[string][]EnvoyResource)
	eic.ingressNodeGroupStore = make(map[string][]envoy_cache.NodeGroup)
}

// NewEnvoyIngressController creates a new EnvoyIngressController
func NewEnvoyIngressController(
	secretInformer coreinformers.SecretInformer,
	endpointInformer coreinformers.EndpointsInformer,
	ingressInformer networkingInformers.IngressInformer,
	v1beta1IngressInformer v1beta1networkInformers.IngressInformer,
	nodeInformer coreinformers.NodeInformer,
	serviceInformer coreinformers.ServiceInformer,
	envoyIngressControllerConfiguration EICConfiguration,
	kubeCLient clientset.Interface,
	enable bool,
) (*EnvoyIngressController, error) {
	eventBroadcaster := record.NewBroadcaster()
	eventBroadcaster.StartStructuredLogging(0)
	eventBroadcaster.StartRecordingToSink(&v1core.EventSinkImpl{Interface: kubeCLient.CoreV1().Events("")})

	if kubeCLient != nil && kubeCLient.CoreV1().RESTClient().GetRateLimiter() != nil {
		if err := ratelimiter.RegisterMetricAndTrackRateLimiterUsage("envoyingress_controller", kubeCLient.CoreV1().RESTClient().GetRateLimiter()); err != nil {
			return nil, err
		}
	}
	eic := &EnvoyIngressController{
		kubeClient:                          kubeCLient,
		eventRecorder:                       eventBroadcaster.NewRecorder(scheme.Scheme, v1.EventSource{Component: "envoyingress-controller"}),
		envoyIngressControllerConfiguration: envoyIngressControllerConfiguration,
		queue:                               workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "envoyingress"),
	}

	ingressInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    eic.addIngress,
		UpdateFunc: eic.updateIngress,
		DeleteFunc: eic.deleteIngress,
	})
	eic.ingressLister = ingressInformer.Lister()
	eic.ingressStoreSynced = ingressInformer.Informer().HasSynced

	v1beta1IngressInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    eic.addV1beta1Ingress,
		UpdateFunc: eic.updateV1beta1Ingress,
		DeleteFunc: eic.deleteV1beta1Ingress,
	})
	eic.v1beta1IngressLister = v1beta1IngressInformer.Lister()
	eic.ingressStoreSynced = v1beta1IngressInformer.Informer().HasSynced

	nodeInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    eic.addNode,
		UpdateFunc: eic.updateNode,
		DeleteFunc: eic.deleteNode,
	})
	eic.nodeLister = nodeInformer.Lister()
	eic.nodeStoreSynced = nodeInformer.Informer().HasSynced

	serviceInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    eic.addService,
		UpdateFunc: eic.updateService,
		DeleteFunc: eic.deleteService,
	})
	eic.serviceLister = serviceInformer.Lister()
	eic.serviceStoreSynced = serviceInformer.Informer().HasSynced

	endpointInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    eic.addEndpoint,
		UpdateFunc: eic.updateEndpoint,
		DeleteFunc: eic.deleteEndpoint,
	})
	eic.endpointLister = endpointInformer.Lister()
	eic.endpointStoreSynced = endpointInformer.Informer().HasSynced

	secretInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    eic.addSecret,
		UpdateFunc: eic.updateSecret,
		DeleteFunc: eic.deleteSecret,
	})
	eic.secretLister = secretInformer.Lister()
	eic.secretStoreSynced = secretInformer.Informer().HasSynced

	eic.enable = enable
	eic.syncHandler = eic.syncEnvoyIngress

	eic.messageLayer = messagelayer.NewContextMessageLayer()

	initializeFields(eic)

	return eic, nil
}

// TODO: need reconstructing
// Register registers envoy ingress controller to beehive core.
func Register(eic *v1alpha1.EnvoyIngressController) {
	config.InitConfigure(eic)
	// Get clientSet from keclient package
	kubeClient := keclient.GetKubeClient()
	sharedInformers := keinformers.GetInformersManager().GetK8sInformerFactory()
	endpointInformer := sharedInformers.Core().V1().Endpoints()
	secretInformer := sharedInformers.Core().V1().Secrets()
	ingressInformer := sharedInformers.Networking().V1().Ingresses()
	v1beta1IngressInformer := sharedInformers.Networking().V1beta1().Ingresses()
	nodeInformer := sharedInformers.Core().V1().Nodes()
	serviceInformer := sharedInformers.Core().V1().Services()
	envoyIngressControllerConfiguration := EICConfiguration{
		syncInterval:                 eic.SyncInterval,
		envoyServiceSyncInterval:     eic.EnvoyServiceSyncInterval,
		ingressSyncWorkerNumber:      eic.IngressSyncWorkerNumber,
		envoyServiceSyncWorkerNumber: eic.EnvoyServiceSyncWorkerNumber,
	}
	// TODO: deal with error
	envoyIngresscontroller, err := NewEnvoyIngressController(secretInformer, endpointInformer, ingressInformer, v1beta1IngressInformer, nodeInformer, serviceInformer, envoyIngressControllerConfiguration, kubeClient, eic.Enable)
	if err != nil {
		klog.Errorf("failed to create envoy ingress controller, err: %v", err)
	}
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
func (eic *EnvoyIngressController) Start() {
	eic.Run(5, beehiveContext.Done())
}

// addIngress adds the given ingress to the queue
func (eic *EnvoyIngressController) addIngress(obj interface{}) {
	ingress := obj.(*ingressv1.Ingress)
	if ingress.Annotations[INGRESSCLASSANNOTATION] != ENVOYINGRESSCONTROLLERNAME {
		klog.V(4).Infof("Ignore ingress %s, which is not an envoy ingress object", ingress.Name)
		return
	}
	key, err := controller.KeyFunc(ingress)
	if err != nil {
		utilruntime.HandleError(fmt.Errorf("couldn't get key for object %+v: %v", ingress, err))
		return
	}
	nodegroup := strings.Split(ingress.Annotations[ENVOYINGRESSNODEGROUPANNOTATION], ";")
	eic.ingressNodeGroupStoreLock.Lock()
	defer eic.ingressNodeGroupStoreLock.Unlock()
	for _, v := range nodegroup {
		if len(v) != 0 {
			eic.ingressNodeGroupStore[key] = append(eic.ingressNodeGroupStore[key], envoy_cache.NodeGroup(v))
		}
	}
	klog.V(4).Infof("Adding envoy ingress %s", ingress.Name)
	eic.enqueue(ingress)
}

// updateIngress compares the uid of given ingresses and if they differences
// delete the old ingress and enqueue the new one
func (eic *EnvoyIngressController) updateIngress(old, cur interface{}) {
	oldIngress := old.(*ingressv1.Ingress)
	curIngress := cur.(*ingressv1.Ingress)

	if curIngress.UID != oldIngress.UID && oldIngress.Annotations[INGRESSCLASSANNOTATION] == ENVOYINGRESSCONTROLLERNAME {
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
	if curIngress.Annotations[INGRESSCLASSANNOTATION] == ENVOYINGRESSCONTROLLERNAME {
		klog.V(4).Infof("Updating envoy ingress %s", oldIngress.Name)
		eic.enqueue(curIngress)
	} else {
		klog.V(4).Infof("Updating envoy ingress controller class has changed, old envoy ingress %s ", oldIngress.Name)
	}
}

// deleteIngress deletes the given ingress from queue.
func (eic *EnvoyIngressController) deleteIngress(obj interface{}) {
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

func (eic *EnvoyIngressController) addV1beta1Ingress(obj interface{}) {
	v1beta1Ingress := obj.(*v1beta1Ingressv1.Ingress)
	ingress := toV1Ingress(v1beta1Ingress)
	eic.addIngress(ingress)
}

func (eic *EnvoyIngressController) updateV1beta1Ingress(old, cur interface{}) {
	curV1beta1Ingress := cur.(*v1beta1Ingressv1.Ingress)
	oldV1beta1Ingress := old.(*v1beta1Ingressv1.Ingress)
	curIngress := toV1Ingress(curV1beta1Ingress)
	oldIngress := toV1Ingress(oldV1beta1Ingress)
	eic.updateIngress(curIngress, oldIngress)
}

func (eic *EnvoyIngressController) deleteV1beta1Ingress(obj interface{}) {
	v1beta1Ingress := obj.(*v1beta1Ingressv1.Ingress)
	ingress := toV1Ingress(v1beta1Ingress)
	eic.deleteIngress(ingress)
}

func toV1Ingress(obj *v1beta1Ingressv1.Ingress) *ingressv1.Ingress {
	if obj == nil {
		return nil
	}

	var convertedTLS []ingressv1.IngressTLS
	var convertedIngressRules []ingressv1.IngressRule
	var convertedDefaultBackend *ingressv1.IngressBackend

	for _, tls := range obj.Spec.TLS {
		convertedTLS = append(convertedTLS, ingressv1.IngressTLS{
			Hosts:      tls.Hosts,
			SecretName: tls.SecretName,
		})
	}

	for _, r := range obj.Spec.Rules {
		rule := ingressv1.IngressRule{}

		if r.Host != "" {
			rule.Host = r.Host
		}

		if r.HTTP != nil {
			var paths []ingressv1.HTTPIngressPath

			for _, p := range r.HTTP.Paths {
				// Default to implementation specific path type if not set.
				// In practice this is mostly to ensure tests do not panic as a
				// a real resource cannot be created without a path type set.
				pathType := ingressv1.PathTypeImplementationSpecific
				if p.PathType != nil {
					switch *p.PathType {
					case v1beta1Ingressv1.PathTypePrefix:
						pathType = ingressv1.PathTypePrefix
					case v1beta1Ingressv1.PathTypeExact:
						pathType = ingressv1.PathTypeExact
					case v1beta1Ingressv1.PathTypeImplementationSpecific:
						pathType = ingressv1.PathTypeImplementationSpecific
					}
				}

				paths = append(paths, ingressv1.HTTPIngressPath{
					Path:     p.Path,
					PathType: &pathType,
					Backend: ingressv1.IngressBackend{
						Service: &ingressv1.IngressServiceBackend{
							Name: p.Backend.ServiceName,
							Port: serviceBackendPort(p.Backend.ServicePort),
						},
					},
				})
			}

			rule.IngressRuleValue = ingressv1.IngressRuleValue{
				HTTP: &ingressv1.HTTPIngressRuleValue{
					Paths: paths,
				},
			}
		}

		convertedIngressRules = append(convertedIngressRules, rule)
	}

	if obj.Spec.Backend != nil {
		convertedDefaultBackend = &ingressv1.IngressBackend{
			Service: &ingressv1.IngressServiceBackend{
				Name: obj.Spec.Backend.ServiceName,
				Port: serviceBackendPort(obj.Spec.Backend.ServicePort),
			},
		}
	}

	return &ingressv1.Ingress{
		ObjectMeta: obj.ObjectMeta,
		Spec: ingressv1.IngressSpec{
			IngressClassName: obj.Spec.IngressClassName,
			DefaultBackend:   convertedDefaultBackend,
			TLS:              convertedTLS,
			Rules:            convertedIngressRules,
		},
	}
}

func serviceBackendPort(port intstr.IntOrString) ingressv1.ServiceBackendPort {
	if port.Type == intstr.String {
		return ingressv1.ServiceBackendPort{
			Name: port.StrVal,
		}
	}
	return ingressv1.ServiceBackendPort{
		Number: port.IntVal,
	}
}

// addNode updates the node2group and group2node map.
func (eic *EnvoyIngressController) addNode(obj interface{}) {
	node := obj.(*v1.Node)
	eic.lc.UpdateNodeGroup(node)

	var nstatus string
	for _, nsc := range node.Status.Conditions {
		if nsc.Type != v1.NodeReady {
			continue
		}
		nstatus = string(nsc.Status)
		status, _ := eic.lc.GetNodeStatus(node.ObjectMeta.Name)
		eic.lc.UpdateEdgeNode(node)
		if nsc.Status != v1.ConditionTrue || status == nstatus {
			continue
		}
		eic.syncAllResourcesToEdgeNodes(node)
	}

}

// updateNode updates the node2group and group2node map.
func (eic *EnvoyIngressController) updateNode(old, cur interface{}) {
	oldNode := old.(*v1.Node)
	curNode := cur.(*v1.Node)

	if curNode.Labels[NODEGROUPLABEL] != oldNode.Labels[NODEGROUPLABEL] {
		eic.lc.DeleteNodeGroup(oldNode)
		eic.lc.UpdateNodeGroup(curNode)
	}

	var nstatus string

	for _, nsc := range curNode.Status.Conditions {
		if nsc.Type != v1.NodeReady {
			continue
		}
		nstatus = string(nsc.Status)
		status, _ := eic.lc.GetNodeStatus(curNode.ObjectMeta.Name)
		eic.lc.UpdateEdgeNode(curNode)
		if nsc.Status != v1.ConditionTrue || status == nstatus {
			continue
		}
		eic.syncAllResourcesToEdgeNodes(curNode)
	}
}

// deleteNode updates the node2group and group2node map.
func (eic *EnvoyIngressController) deleteNode(obj interface{}) {
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
	eic.lc.DeleteNodeGroup(node)
	eic.lc.DeleteEdgeNode(node)
}

// When a service is added, figure out what ingresses potentially match it.
func (eic *EnvoyIngressController) addService(obj interface{}) {
	service := obj.(*v1.Service)

	ingresses := eic.getIngressesForService(service)
	if len(ingresses) == 0 {
		return
	}
	for _, ingress := range ingresses {
		eic.enqueue(ingress)
	}
}

// When a service is updated, figure out what ingresses potentially match it.
func (eic *EnvoyIngressController) updateService(old, cur interface{}) {
	oldService := old.(*v1.Service)
	curService := cur.(*v1.Service)

	selectorChanged := !reflect.DeepEqual(curService.Spec.Selector, oldService.Spec.Selector)
	if selectorChanged {
		klog.V(4).Infof("service %v's selector has changed", oldService.Name)
		eic.deleteService(oldService)
		eic.addService(curService)
	}
}

// When a service is deleted, figure out what ingresses potentially match it.
func (eic *EnvoyIngressController) deleteService(obj interface{}) {
	service, ok := obj.(*v1.Service)
	if !ok {
		tombstone, ok := obj.(cache.DeletedFinalStateUnknown)
		if !ok {
			utilruntime.HandleError(fmt.Errorf("couldn't get object from tombstone %#v", obj))
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
	if len(ingresses) == 0 {
		return
	}
	for _, ingress := range ingresses {
		eic.enqueue(ingress)
	}
}

func (eic *EnvoyIngressController) addEndpoint(obj interface{}) {
	endpoint := obj.(*v1.Endpoints)
	serviceName := endpoint.Name
	serviceNamespace := endpoint.Namespace

	service, err := eic.serviceLister.Services(serviceNamespace).Get(serviceName)
	if err != nil {
		utilruntime.HandleError(fmt.Errorf("couldn't get service for endpoint %s in namespace %s", endpoint.Name, endpoint.Namespace))
		return
	}

	eic.addService(service)
}

func (eic *EnvoyIngressController) updateEndpoint(old, cur interface{}) {
	curEndpoint := cur.(*v1.Endpoints)
	oldEndpoint := old.(*v1.Endpoints)

	subsetsChanged := !reflect.DeepEqual(curEndpoint.Subsets, oldEndpoint.Subsets)
	if subsetsChanged {
		service, err := eic.serviceLister.Services(oldEndpoint.Namespace).Get(oldEndpoint.Name)
		if err != nil {
			utilruntime.HandleError(fmt.Errorf("couldn't get service for endpoint %s in namespace %s", oldEndpoint.Name, oldEndpoint.Namespace))
			return
		}
		eic.deleteService(service)
		eic.addService(service)
	}
}

func (eic *EnvoyIngressController) deleteEndpoint(obj interface{}) {
	endpoint, ok := obj.(*v1.Endpoints)
	if !ok {
		tombstone, ok := obj.(cache.DeletedFinalStateUnknown)
		if !ok {
			utilruntime.HandleError(fmt.Errorf("couldn't get object from tombstone %#v", obj))
			return
		}
		endpoint, ok = tombstone.Obj.(*v1.Endpoints)
		if !ok {
			utilruntime.HandleError(fmt.Errorf("tombstone contained object that is not a endpoint %#v", obj))
			return
		}
	}

	service, err := eic.serviceLister.Services(endpoint.Namespace).Get(endpoint.Name)
	if err != nil {
		utilruntime.HandleError(fmt.Errorf("couldn't get service for endpoint %s in namespace %s", endpoint.Name, endpoint.Namespace))
		return
	}

	eic.deleteService(service)
}

func (eic *EnvoyIngressController) addSecret(obj interface{}) {
	secret := obj.(*v1.Secret)

	ingresses := eic.getIngressesForSecret(secret)
	if len(ingresses) == 0 {
		return
	}
	for _, ingress := range ingresses {
		eic.enqueue(ingress)
	}
}

func (eic *EnvoyIngressController) updateSecret(old, cur interface{}) {
	oldSecret := old.(*v1.Secret)
	curSecret := cur.(*v1.Secret)

	secretChanged := !reflect.DeepEqual(oldSecret.Type, curSecret.Type) ||
		!reflect.DeepEqual(oldSecret.Data, curSecret.Data) ||
		!reflect.DeepEqual(oldSecret.StringData, curSecret.StringData)
	if secretChanged {
		eic.deleteSecret(oldSecret)
		eic.addSecret(curSecret)
	}
}

func (eic *EnvoyIngressController) deleteSecret(obj interface{}) {
	secret, ok := obj.(*v1.Secret)
	if !ok {
		tombstone, ok := obj.(cache.DeletedFinalStateUnknown)
		if !ok {
			utilruntime.HandleError(fmt.Errorf("couldn't get object from tombstone %#v", obj))
			return
		}
		secret, ok = tombstone.Obj.(*v1.Secret)
		if !ok {
			utilruntime.HandleError(fmt.Errorf("tombstone contained object that is not a secret %#v", obj))
			return
		}
	}

	klog.V(4).Infof("Secret %s deleted.", secret.Name)
	ingresses := eic.getIngressesForSecret(secret)
	if len(ingresses) == 0 {
		return
	}
	for _, ingress := range ingresses {
		eic.enqueue(ingress)
	}
}

// Run begins watching and syncing ingresses.
func (eic *EnvoyIngressController) Run(workers int, stopCh <-chan struct{}) {
	defer utilruntime.HandleCrash()
	defer eic.queue.ShutDown()

	klog.Infof("Starting envoy ingress controller")
	defer klog.Infof("Shutting down envoy ingress controller")

	// TODO:when starting controller, first sync nodegroup relationship
	// then generate envoy resources for all present ingresses

	err := eic.initCache()
	if err != nil {
		utilruntime.HandleError(fmt.Errorf("fail to initiate local cache"))
		return
	}

	klog.Infof("succeeded in initiate local cache")

	err = eic.initiateEnvoyResources()
	if err != nil {
		utilruntime.HandleError(fmt.Errorf("fail to initiate envoy resources"))
		return
	}

	klog.Infof("succeeded in initiate envoy resources")

	klog.Infof("start to sync envoy resources")

	for i := 0; i < eic.envoyIngressControllerConfiguration.ingressSyncWorkerNumber; i++ {
		go wait.Until(eic.runIngressWorkers, eic.envoyIngressControllerConfiguration.syncInterval, stopCh)
	}

	klog.Infof("start to dispatch messages")

	for i := 0; i < eic.envoyIngressControllerConfiguration.envoyServiceSyncWorkerNumber; i++ {
		go wait.Until(eic.consumer, eic.envoyIngressControllerConfiguration.envoyServiceSyncInterval, stopCh)
	}

	<-stopCh
}

func (eic *EnvoyIngressController) runIngressWorkers() {
	for eic.processNextIngressWorkItem() {
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
	if ingress.Annotations[INGRESSCLASSANNOTATION] != ENVOYINGRESSCONTROLLERNAME {
		return
	}
	key, err := controller.KeyFunc(ingress)
	if err != nil {
		utilruntime.HandleError(fmt.Errorf("couldn't get key for object %#v: %v", ingress, err))
		return
	}

	eic.queue.Add(key)
}

func (eic *EnvoyIngressController) enqueueEnvoyIngressAfter(obj interface{}, after time.Duration) {
	ingress, ok := obj.(*ingressv1.Ingress)
	if !ok {
		utilruntime.HandleError(fmt.Errorf("cloudn't convert obj into ingress, obj:%#v", obj))
	}
	if ingress.Annotations[INGRESSCLASSANNOTATION] != ENVOYINGRESSCONTROLLERNAME {
		return
	}

	key, err := controller.KeyFunc(obj)
	if err != nil {
		utilruntime.HandleError(fmt.Errorf("couldn't get key for object %+v: %v", obj, err))
		return
	}

	eic.queue.AddAfter(key, after)
}

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
	for _, service := range services {
		tmpIngresses = eic.getIngressesForService(service)
		if len(tmpIngresses) == 0 {
			continue
		}
		ingresses = append(ingresses, tmpIngresses...)
	}
	if len(ingresses) == 0 {
		return nil
	}
	return ingresses
}

// getServicesForPod returns a list of services that potentially match the pod
func (eic *EnvoyIngressController) getServicesForPod(pod *v1.Pod) []*v1.Service {
	var selector labels.Selector

	if pod == nil {
		return nil
	}
	if len(pod.Labels) == 0 {
		// If the pod has no label, it can't be bound to a service
		return nil
	}

	list, err := eic.serviceLister.Services(pod.Namespace).List(labels.Everything())
	if err != nil {
		utilruntime.HandleError(fmt.Errorf("failed to list all the services in cluster for pod: %v", pod.Name))
		return nil
	}

	var services []*v1.Service
	for _, service := range list {
		var labelSelector = &metav1.LabelSelector{}
		if service.Namespace != pod.Namespace {
			continue
		}
		err = metav1.Convert_Map_string_To_string_To_v1_LabelSelector(&service.Spec.Selector, labelSelector, nil)
		if err != nil {
			utilruntime.HandleError(fmt.Errorf("failed to convert service %v's selector into label selector", service.Name))
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
	if service == nil {
		return nil
	}
	if len(service.Spec.Selector) == 0 {
		return nil
	}

	list, err := eic.ingressLister.Ingresses(service.Namespace).List(labels.Everything())
	if err != nil {
		utilruntime.HandleError(fmt.Errorf("cloudn't get ingresses for service %#v, err: %v", service.Name, err))
		return nil
	}
	// Merge v1beta1 ingresses into v1 ingresses' list
	tmpList, err := eic.v1beta1IngressLister.Ingresses(service.Namespace).List(labels.Everything())
	if err != nil {
		utilruntime.HandleError(fmt.Errorf("cloudn't get v1beta1 ingresses for service %#v, err: %v", service.Name, err))
		return nil
	}
	for _, v1beta1Ingress := range tmpList {
		ingress := toV1Ingress(v1beta1Ingress)
		list = append(list, ingress)
	}

	var ingresses []*ingressv1.Ingress
	for _, ingress := range list {
		if ingress.Annotations[INGRESSCLASSANNOTATION] != ENVOYINGRESSCONTROLLERNAME {
			continue
		}
		isIngressMatchService := false
		if ingress.Namespace != service.Namespace {
			continue
		}
		if ingress.Spec.DefaultBackend != nil && ingress.Spec.DefaultBackend.Service != nil {
			if ingress.Spec.DefaultBackend.Service.Name == service.Name {
				isIngressMatchService = true
			}
		}
		if len(ingress.Spec.Rules) != 0 && !isIngressMatchService {
		RuleLoop:
			for _, rule := range ingress.Spec.Rules {
				if len(rule.IngressRuleValue.HTTP.Paths) != 0 {
					for _, path := range rule.IngressRuleValue.HTTP.Paths {
						if path.Backend.Service.Name == service.Name {
							isIngressMatchService = true
							break RuleLoop
						}
					}
				} else if len(rule.HTTP.Paths) != 0 {
					for _, path := range rule.HTTP.Paths {
						if path.Backend.Service.Name == service.Name {
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
func (eic *EnvoyIngressController) getServicesForIngress(ingress *ingressv1.Ingress) ([]*v1.Service, error) {
	var services []*v1.Service
	var isServiceMatchIngress bool
	list, err := eic.serviceLister.Services(ingress.Namespace).List(labels.Everything())
	if err != nil {
		utilruntime.HandleError(fmt.Errorf("cloudn't get services fro ingress:%#v", ingress))
		return nil, err
	}
	for _, service := range list {
		isServiceMatchIngress = false
		if service.Namespace != ingress.Namespace {
			continue
		}
		if ingress.Spec.DefaultBackend != nil && ingress.Spec.DefaultBackend.Service != nil {
			if service.Name == ingress.Spec.DefaultBackend.Service.Name {
				isServiceMatchIngress = true
			}
		}
		if !isServiceMatchIngress && len(ingress.Spec.Rules) != 0 {
		RuleLoop:
			for _, rule := range ingress.Spec.Rules {
				if len(rule.IngressRuleValue.HTTP.Paths) != 0 {
					for _, path := range rule.IngressRuleValue.HTTP.Paths {
						if path.Backend.Service.Name == service.Name {
							isServiceMatchIngress = true
							break RuleLoop
						}
					}
				} else if len(rule.HTTP.Paths) != 0 {
					for _, path := range rule.HTTP.Paths {
						if path.Backend.Service.Name == service.Name {
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

func (eic *EnvoyIngressController) getIngressesForSecret(secret *v1.Secret) []*ingressv1.Ingress {
	if secret == nil {
		return nil
	}

	list, err := eic.ingressLister.Ingresses(secret.Namespace).List(labels.Everything())
	if err != nil {
		utilruntime.HandleError(fmt.Errorf("cloudn't get ingresses for secret %#v, err: %v", secret.Name, err))
		return nil
	}

	// Merge v1beta1 ingresses into v1 ingresses' list
	tmpList, err := eic.v1beta1IngressLister.Ingresses(secret.Namespace).List(labels.Everything())
	if err != nil {
		utilruntime.HandleError(fmt.Errorf("cloudn't get v1beta1 ingresses for service %#v, err: %v", secret.Name, err))
		return nil
	}
	for _, v1beta1Ingress := range tmpList {
		ingress := toV1Ingress(v1beta1Ingress)
		list = append(list, ingress)
	}

	var ingresses []*ingressv1.Ingress
	for _, ingress := range list {
		if ingress.Annotations[INGRESSCLASSANNOTATION] != ENVOYINGRESSCONTROLLERNAME {
			continue
		}
		for _, tls := range ingress.Spec.TLS {
			if tls.SecretName == secret.Name {
				ingresses = append(ingresses, ingress)
				break
			}
		}
	}

	if len(ingresses) == 0 {
		return nil
	}

	return ingresses
}

func getNodeGroupForIngress(ingress *ingressv1.Ingress) ([]envoy_cache.NodeGroup, error) {
	var nodegroup []envoy_cache.NodeGroup
	nodeGroupStrings := strings.Split(ingress.Annotations[ENVOYINGRESSNODEGROUPANNOTATION], ";")
	for _, nodeGroupString := range nodeGroupStrings {
		if len(nodeGroupString) != 0 {
			nodegroup = append(nodegroup, envoy_cache.NodeGroup(nodeGroupString))
		}
	}
	if len(nodegroup) == 0 {
		return nil, fmt.Errorf("ingress %s in namespace %s doesn't have nodegroup annotation", ingress.Name, ingress.Namespace)
	}

	return nodegroup, nil
}

func (eic *EnvoyIngressController) GetSecretsForIngress(ingress *ingressv1.Ingress) ([]*EnvoySecret, error) {
	return eic.getSecretsForIngress(ingress)
}

// TODO: sending envoy objects to edge will make it different to manage the objects. Need considering construct a object which is k8s style
func (eic *EnvoyIngressController) getSecretsForIngress(ingress *ingressv1.Ingress) ([]*EnvoySecret, error) {
	nodegroup, err := getNodeGroupForIngress(ingress)
	if err != nil {
		return nil, err
	}

	var envoySecret []*EnvoySecret
	for _, ingressTLS := range ingress.Spec.TLS {
		// The secret and ingress have to be in the same namespace
		secret, err := eic.secretLister.Secrets(ingress.Namespace).Get(ingressTLS.SecretName)
		if err != nil {
			continue
		}
		tmp := Secret(secret)
		envoySecret = append(envoySecret, &EnvoySecret{
			Name:            tmp.Name,
			Namespace:       ingress.Namespace,
			ResourceVersion: secret.ResourceVersion,
			NodeGroup:       nodegroup,
			Secret:          *tmp,
		})
	}

	return envoySecret, nil
}

func (eic *EnvoyIngressController) GetEndpointsForIngress(ingress *ingressv1.Ingress) ([]*EnvoyEndpoint, error) {
	return eic.getEndpointsForIngress(ingress)
}

func (eic *EnvoyIngressController) getEndpointsForIngress(ingress *ingressv1.Ingress) ([]*EnvoyEndpoint, error) {
	nodegroup, err := getNodeGroupForIngress(ingress)
	if err != nil {
		return nil, err
	}

	var clusterLoadAssignments []*EnvoyEndpoint
	var clusterLoadAssignment *EnvoyEndpoint
	services, err := eic.getServicesForIngress(ingress)
	if err != nil {
		return nil, err
	}

	for _, service := range services {
		for _, servicePort := range service.Spec.Ports {
			var lbs []*envoy_endpoint_v3.LbEndpoint
			endpoints, err := eic.endpointLister.Endpoints(service.Namespace).Get(service.Name)
			if err != nil {
				continue
			}
			for _, s := range endpoints.Subsets {
				// Skip subsets without ready addresses
				if len(s.Ports) < 1 {
					continue
				}

				for _, p := range s.Ports {
					if servicePort.Protocol != p.Protocol && p.Protocol != v1.ProtocolTCP {
						// NOTE: we only support "TCP", which is the default.
						continue
					}

					// If the port isn't named, it must be the
					// only Service port, so it's a match by
					// definition. Otherwise, only take endpoint
					// ports that match the service port name.
					if servicePort.Name != "" && servicePort.Name != p.Name {
						continue
					}

					// If we matched this port, collect Envoy endpoints for all the ready addresses.
					addresses := append([]v1.EndpointAddress{}, s.Addresses...) // Shallow copy.

					sort.Slice(addresses, func(i, j int) bool { return addresses[i].IP < addresses[j].IP })
					for _, a := range addresses {
						addr := SocketAddress(a.IP, int(p.Port))
						lbs = append(lbs, LBEndpoint(addr))
					}
				}
			}
			clusterLoadAssignment = &EnvoyEndpoint{
				Name:            Hashname(60, service.Name, service.Namespace),
				Namespace:       service.Namespace,
				ResourceVersion: service.ResourceVersion,
				NodeGroup:       nodegroup,
				ClusterLoadAssignment: envoy_endpoint_v3.ClusterLoadAssignment{
					ClusterName: service.Name,
					Endpoints: []*envoy_endpoint_v3.LocalityLbEndpoints{
						{
							LbEndpoints: lbs,
							LoadBalancingWeight: &wrapperspb.UInt32Value{
								Value: 1,
							},
						},
					},
					Policy: nil,
				},
			}
		}
		// TODO: need to consider when clusterLoadAssignment is nil
		if clusterLoadAssignment != nil {
			clusterLoadAssignments = append(clusterLoadAssignments, clusterLoadAssignment)
		}
	}

	if len(clusterLoadAssignments) == 0 {
		return nil, fmt.Errorf("cloudn't get clusterLoadAssignment for ingress %v in namespace %v", ingress.Name, ingress.Namespace)
	}

	return clusterLoadAssignments, nil
}

func (eic *EnvoyIngressController) GetClusterForIngress(ingress *ingressv1.Ingress) ([]*EnvoyCluster, error) {
	return eic.getClustersForIngress(ingress)
}

func (eic *EnvoyIngressController) getClustersForIngress(ingress *ingressv1.Ingress) ([]*EnvoyCluster, error) {
	var (
		clusters              []*EnvoyCluster
		envoyCluster          *EnvoyCluster
		cluster               *envoy_cluster_v3.Cluster
		host2ServiceName      map[string]map[string]bool
		UpstreamValidation    *PeerValidationContext
		httpHealthCheckPolicy *HTTPHealthCheckPolicy
	)
	nodegroup, err := getNodeGroupForIngress(ingress)
	if err != nil {
		return nil, err
	}

	services, err := eic.getServicesForIngress(ingress)
	if err != nil {
		return nil, err
	}

	host2ServiceName = make(map[string]map[string]bool)

	for _, rule := range ingress.Spec.Rules {
		if len(rule.Host) == 0 {
			continue
		}
		for _, path := range rule.HTTP.Paths {
			if path.Backend.Service != nil {
				if len(path.Backend.Service.Name) != 0 {
					if _, ok := host2ServiceName[rule.Host]; !ok {
						host2ServiceName[rule.Host] = make(map[string]bool)
					}
					host2ServiceName[rule.Host][path.Backend.Service.Name] = true
				}
			}
		}
	}

	for _, service := range services {
		var (
			protocol = service.Annotations[SERVICEHTTPPROTOCOLANNOTATION]
			path     = service.Annotations[SERVICEHEALTHCHECKPATHANNOTATION]
		)
		if len(path) != 0 {
			httpHealthCheckPolicy = &HTTPHealthCheckPolicy{
				Path:               path,
				Interval:           5 * time.Second,
				Timeout:            30 * time.Second,
				UnhealthyThreshold: 3,
				HealthyThreshold:   1,
			}
		} else {
			httpHealthCheckPolicy = nil
		}
		for _, servicePort := range service.Spec.Ports {
			var sni string
			var secretName string
			var sec *v1.Secret
			for _, rule := range ingress.Spec.Rules {
				for _, path := range rule.HTTP.Paths {
					serviceName := path.Backend.Service.Name
					if serviceName == service.Name {
						sni = rule.Host
						break
					}
				}
			}
			if len(sni) > 0 {
				for _, tls := range ingress.Spec.TLS {
					for _, host := range tls.Hosts {
						if host == sni {
							secretName = tls.SecretName
							break
						}
					}
				}
			}
			if len(secretName) > 0 {
				sec, err = eic.secretLister.Secrets(ingress.Namespace).Get(secretName)
				if err != nil {
					klog.V(4).Infof("Fail to get secret %s in namespace %s for ingress %s with host %s", secretName, ingress.Namespace, ingress.Name, sni)
					continue
				}
			}
			cluster = clusterDefaults()
			// TODO: 5.9 bug here
			switch protocol {
			case "tls":
				fallthrough
			case "h2":
				UpstreamValidation = &PeerValidationContext{
					CACertificate: sec,
					SubjectName:   sni,
				}
			default:
				UpstreamValidation = nil
			}
			cluster.Name = Clustername(service, &servicePort, "", httpHealthCheckPolicy, UpstreamValidation)
			cluster.AltStatName = AltStatName(service, &servicePort)
			cluster.DnsLookupFamily = envoy_cluster_v3.Cluster_AUTO
			switch len(service.Spec.ExternalName) {
			case 0:
				// external name not set, cluster will be discovered via EDS
				cluster.ClusterDiscoveryType = ClusterDiscoveryType(envoy_cluster_v3.Cluster_EDS)
				cluster.EdsClusterConfig = edsconfig("envoyingresscontroller", service, &servicePort)
			default:
				// external name set, use hard coded DNS name
				cluster.ClusterDiscoveryType = ClusterDiscoveryType(envoy_cluster_v3.Cluster_STRICT_DNS)
				cluster.LoadAssignment = StaticClusterLoadAssignment(service, &servicePort)
			}
			switch protocol {
			case "tls":
				cluster.TransportSocket = UpstreamTLSTransportSocket(
					UpstreamTLSContext(
						UpstreamValidation,
						sni,
						sec,
					),
				)
			case "h2":
				cluster.TypedExtensionProtocolOptions = http2ProtocolOptions()
				cluster.TransportSocket = UpstreamTLSTransportSocket(
					UpstreamTLSContext(
						UpstreamValidation,
						sni,
						sec,
						"h2",
					),
				)
			case "h2c":
				cluster.TypedExtensionProtocolOptions = http2ProtocolOptions()
			}
			envoyCluster = &EnvoyCluster{
				Name:            cluster.Name,
				Namespace:       ingress.Namespace,
				ResourceVersion: service.ResourceVersion,
				NodeGroup:       nodegroup,
				Cluster:         *cluster,
			}
			clusters = append(clusters, envoyCluster)
		}
	}

	if len(clusters) == 0 {
		return nil, fmt.Errorf("cloudn't get clusters for ingress %v in namespace %v", ingress.Name, ingress.Namespace)
	}

	return clusters, nil
}

func (eic *EnvoyIngressController) GetRouteForIngress(ingress *ingressv1.Ingress) (*EnvoyRoute, error) {
	return eic.getRouteForIngress(ingress)
}

func (eic *EnvoyIngressController) getRouteForIngress(ingress *ingressv1.Ingress) (*EnvoyRoute, error) {
	var (
		envoyRoute            *EnvoyRoute
		routeConfiguration    *envoy_route_v3.RouteConfiguration
		virtualHosts          []*envoy_route_v3.VirtualHost
		virtualHost           *envoy_route_v3.VirtualHost
		host2Secret           map[string]string
		httpHealthCheckPolicy *HTTPHealthCheckPolicy
		UpstreamValidation    *PeerValidationContext
	)
	nodegroup, err := getNodeGroupForIngress(ingress)
	if err != nil {
		return nil, err
	}

	if ingress.Spec.DefaultBackend != nil && ingress.Spec.DefaultBackend.Service != nil && len(ingress.Spec.DefaultBackend.Service.Name) > 0 {
		// TODO: deal with this case
		// In this case, the default backend has been set. Thus need to deal with it.
	}

	host2Secret = make(map[string]string)

	for _, tls := range ingress.Spec.TLS {
		for _, host := range tls.Hosts {
			if len(host) != 0 {
				host2Secret[host] = tls.SecretName
			}
		}
	}

	for _, rule := range ingress.Spec.Rules {
		var routes []*envoy_route_v3.Route
		var route *envoy_route_v3.Route
		for _, path := range rule.HTTP.Paths {
			route = &envoy_route_v3.Route{}
			// TODO: need to initialize this two variables
			if secretName, ok := host2Secret[rule.Host]; ok {
				secret, err := eic.secretLister.Secrets(ingress.Namespace).Get(secretName)
				if err != nil {
					UpstreamValidation = nil
				}
				UpstreamValidation = &PeerValidationContext{
					CACertificate: secret,
					SubjectName:   rule.Host,
				}
			} else {
				UpstreamValidation = nil
			}
			service, err := eic.serviceLister.Services(ingress.Namespace).Get(path.Backend.Service.Name)
			if err != nil {
				klog.V(4).Infof("Fail to get service %s in namespace %s", path.Backend.Service.Name, ingress.Namespace)
				continue
			}
			var (
				healthCheckPath = service.Annotations[SERVICEHEALTHCHECKPATHANNOTATION]
			)
			if len(healthCheckPath) != 0 {
				httpHealthCheckPolicy = &HTTPHealthCheckPolicy{
					Path:               healthCheckPath,
					Interval:           5 * time.Second,
					Timeout:            30 * time.Second,
					UnhealthyThreshold: 3,
					HealthyThreshold:   1,
				}
			} else {
				httpHealthCheckPolicy = nil
			}
			for _, servicePort := range service.Spec.Ports {
				// TODO: the clustername has to be inconsistent with getClustersForIngress
				clusterName := Clustername(service, &servicePort, "", httpHealthCheckPolicy, UpstreamValidation)
				switch *path.PathType {
				case ingressv1.PathTypeExact:
					route.Match = &envoy_route_v3.RouteMatch{
						PathSpecifier: &envoy_route_v3.RouteMatch_Path{
							Path: path.Path,
						},
					}
				case ingressv1.PathTypeImplementationSpecific:
					route.Match = &envoy_route_v3.RouteMatch{
						PathSpecifier: &envoy_route_v3.RouteMatch_SafeRegex{
							SafeRegex: &matcher.RegexMatcher{
								EngineType: &matcher.RegexMatcher_GoogleRe2{
									GoogleRe2: &matcher.RegexMatcher_GoogleRE2{},
								},
								Regex: clusterName,
							},
						},
					}
				default:
					route.Match = &envoy_route_v3.RouteMatch{
						PathSpecifier: &envoy_route_v3.RouteMatch_Prefix{
							Prefix: path.Path,
						},
					}
				}
				route.Action = &envoy_route_v3.Route_Route{
					Route: &envoy_route_v3.RouteAction{
						ClusterSpecifier: &envoy_route_v3.RouteAction_Cluster{
							Cluster: clusterName,
						},
					},
				}
				routes = append(routes, route)
			}
		}
		virtualHost = &envoy_route_v3.VirtualHost{
			Name:    Hashname(60, rule.Host),
			Domains: []string{rule.Host},
			Routes:  routes,
		}
		virtualHosts = append(virtualHosts, virtualHost)
	}
	routeConfiguration = RouteConfiguration(ENVOY_HTTP_LISTENER, virtualHosts...)

	envoyRoute = &EnvoyRoute{
		Name:               Hashname(60, ingress.Name, ingress.Namespace, "envoyroute"),
		Namespace:          ingress.Namespace,
		ResourceVersion:    ingress.ResourceVersion,
		NodeGroup:          nodegroup,
		RouteConfiguration: *routeConfiguration,
	}

	return envoyRoute, nil
}

func (eic *EnvoyIngressController) GetListenersForIngress(ingress *ingressv1.Ingress) ([]*EnvoyListener, error) {
	return eic.getListenersForIngress(ingress)
}

func (eic *EnvoyIngressController) getListenersForIngress(ingress *ingressv1.Ingress) ([]*EnvoyListener, error) {
	nodegroup, err := getNodeGroupForIngress(ingress)
	if err != nil {
		return nil, err
	}

	var envoyListeners []*EnvoyListener
	var envoyListener *EnvoyListener
	var httpListener *envoy_listener_v3.Listener
	var httpsListener *envoy_listener_v3.Listener

	for _, rule := range ingress.Spec.Rules {
		var needTLS = false
		var secretName string
	Loop:
		for _, tls := range ingress.Spec.TLS {
			for _, host := range tls.Hosts {
				if host == rule.Host {
					needTLS = true
					secretName = tls.SecretName
					break Loop
				}
			}
		}
		var alpnProtos []string
		var filters []*envoy_listener_v3.Filter
		var filterChain *envoy_listener_v3.FilterChain
		if needTLS {
			httpCm := &http.HttpConnectionManager{
				CodecType: http.HttpConnectionManager_AUTO,
				RouteSpecifier: &http.HttpConnectionManager_Rds{
					Rds: &http.Rds{
						RouteConfigName: ENVOY_HTTP_LISTENER,
						ConfigSource:    ConfigSource("envoyingresscontroller"),
					},
				},
				HttpFilters: []*http.HttpFilter{
					{
						Name: "router",
						ConfigType: &http.HttpFilter_TypedConfig{
							TypedConfig: &any.Any{
								TypeUrl: HTTPFilterRouter,
							},
						},
					},
				},
			}

			cm := &envoy_listener_v3.Filter{
				Name: wellknown.HTTPConnectionManager,
				ConfigType: &envoy_listener_v3.Filter_TypedConfig{
					TypedConfig: MustMarshalAny(httpCm),
				},
			}

			filters = Filters(cm)

			// TODO: add options for http version
			alpnProtos = ProtoNamesForVersions(http.HttpConnectionManager_AUTO)
			secret, err := eic.secretLister.Secrets(ingress.Namespace).Get(secretName)
			if err != nil {
				klog.Infof("Fail to get secret %s in namespace %s", secretName, ingress.Namespace)
				continue
			}
			downstreamTLS := DownstreamTLSContext(secret, envoy_v3_tls.TlsParameters_TLSv1_0, &PeerValidationContext{}, alpnProtos...)
			filterChain = FilterChainTLS(rule.Host, downstreamTLS, filters)
			for _, path := range rule.HTTP.Paths {
				// TODO: Make listener address configurable
				listenerName := Hashname(60, ENVOY_HTTPS_LISTENER, DEFAULT_HTTP_LISTENER_ADDRESS, string(path.Backend.Service.Port.Number))
				httpsListener = Listener(ENVOY_HTTPS_LISTENER, DEFAULT_HTTP_LISTENER_ADDRESS, int(path.Backend.Service.Port.Number), nil)
				httpsListener.FilterChains = append(httpsListener.FilterChains, filterChain)
				envoyListener = &EnvoyListener{
					Name:            listenerName,
					Namespace:       ingress.Namespace,
					ResourceVersion: ingress.ResourceVersion,
					NodeGroup:       nodegroup,
					Listener:        *httpsListener,
				}
				envoyListeners = append(envoyListeners, envoyListener)
			}
		}
		httpCm := &http.HttpConnectionManager{
			CodecType: http.HttpConnectionManager_AUTO,
			RouteSpecifier: &http.HttpConnectionManager_Rds{
				Rds: &http.Rds{
					RouteConfigName: ENVOY_HTTP_LISTENER,
					ConfigSource:    ConfigSource("envoyingresscontroller"),
				},
			},
			HttpFilters: []*http.HttpFilter{
				{
					Name: "router",
					ConfigType: &http.HttpFilter_TypedConfig{
						TypedConfig: &any.Any{
							TypeUrl: HTTPFilterRouter,
						},
					},
				},
			},
		}

		cm := &envoy_listener_v3.Filter{
			Name: wellknown.HTTPConnectionManager,
			ConfigType: &envoy_listener_v3.Filter_TypedConfig{
				TypedConfig: MustMarshalAny(httpCm),
			},
		}
		for _, path := range rule.HTTP.Paths {
			// TODO: Make listener address configurable
			listenerName := Hashname(60, ENVOY_HTTP_LISTENER, DEFAULT_HTTP_LISTENER_ADDRESS, string(path.Backend.Service.Port.Number))
			httpListener = Listener(listenerName, DEFAULT_HTTP_LISTENER_ADDRESS, int(path.Backend.Service.Port.Number), nil, cm)
			envoyListener = &EnvoyListener{
				Name:            listenerName,
				Namespace:       ingress.Namespace,
				ResourceVersion: ingress.ResourceVersion,
				NodeGroup:       nodegroup,
				Listener:        *httpListener,
			}
			envoyListeners = append(envoyListeners, envoyListener)
		}
	}

	if len(envoyListeners) == 0 {
		return nil, fmt.Errorf("fail to create listeners for ingress %s in namespace %s", ingress.Name, ingress.Namespace)
	}

	return envoyListeners, nil
}

func (eic *EnvoyIngressController) consumer() {
	if len(eic.resourceNeedToBeSentToEdgeStore) == 0 {
		return
	}
	eic.resourceNeedToBeSentToEdgeStoreLock.Lock()
	envoyResource := eic.resourceNeedToBeSentToEdgeStore[0]
	eic.resourceNeedToBeSentToEdgeStore = eic.resourceNeedToBeSentToEdgeStore[1:]
	eic.resourceNeedToBeSentToEdgeStoreLock.Unlock()
	var nodegroup []envoy_cache.NodeGroup
	switch envoyResource.Kind {
	case SECRET:
		eic.secretStoreLock.RLock()
		secret, ok := eic.secretStore[envoyResource.Name]
		if !ok {
			klog.Warningf("Fail to get secret %s from secret store", envoyResource.Name)
			return
		}
		eic.secretStoreLock.RUnlock()
		nodegroup = secret.NodeGroup
	case ENDPOINT:
		eic.endpointStoreLock.RLock()
		endpoint, ok := eic.endpointStore[envoyResource.Name]
		if !ok {
			klog.Warningf("Fail to get endpoint %s from endpoint store", envoyResource.Name)
			return
		}
		eic.endpointStoreLock.RUnlock()
		nodegroup = endpoint.NodeGroup
	case CLUSTER:
		eic.clusterStoreLock.RLock()
		cluster, ok := eic.clusterStore[envoyResource.Name]
		if !ok {
			klog.Warningf("Fail to get cluster %s from cluster store", envoyResource.Name)
			return
		}
		eic.clusterStoreLock.RUnlock()
		nodegroup = cluster.NodeGroup
	case ROUTE:
		eic.routeStoreLock.RLock()
		route, ok := eic.routeStore[envoyResource.Name]
		if !ok {
			klog.Warningf("Fail to get route %s from route store", envoyResource.Name)
			return
		}
		eic.routeStoreLock.RUnlock()
		nodegroup = route.NodeGroup
	case LISTENER:
		eic.listenerStoreLock.RLock()
		listener, ok := eic.listenerStore[envoyResource.Name]
		if !ok {
			klog.Warningf("Fail to get listener %s from listener store", envoyResource.Name)
			return
		}
		eic.listenerStoreLock.RUnlock()
		nodegroup = listener.NodeGroup
	}
	nodesToSend := make(map[string]bool)
	for _, v := range nodegroup {
		for _, node := range eic.lc.Group2node[v] {
			nodesToSend[node] = true
		}
	}
	for node := range nodesToSend {
		klog.Infof("dispatch to node, resource: %v, node: %s", envoyResource, node)
		err := eic.dispatchResource(&envoyResource, model.InsertOperation, node)
		if err != nil {
			klog.Warning(err)
		}
	}
}

func (eic *EnvoyIngressController) syncEnvoyIngress(key string) error {
	startTime := time.Now()
	defer func() {
		klog.V(4).Infof("Finished syncing envoy ingress  %q (%v)", key, time.Since(startTime))
	}()

	namespace, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		return err
	}
	ingress, err := eic.ingressLister.Ingresses(namespace).Get(name)
	if errors.IsNotFound(err) {
		v1beta1Ingress, err := eic.v1beta1IngressLister.Ingresses(namespace).Get(name)
		if errors.IsNotFound(err) {
			klog.V(4).Infof("ingress has been deleted %v", key)
			eic.ingressNodeGroupStoreLock.Lock()
			nodegroup := eic.ingressNodeGroupStore[key]
			eic.ingressNodeGroupStoreLock.Unlock()
			var nodesToSend map[string]bool
			for _, v := range nodegroup {
				for _, node := range eic.lc.Group2node[v] {
					nodesToSend[node] = true
				}
			}
			// TODO: need to maintain stores and ingress2store relationship
			eic.ingressToResourceNameStoreLock.Lock()
			envoyResources := eic.ingressToResourceNameStore[key]
			eic.ingressToResourceNameStoreLock.Unlock()
			for _, envoyResource := range envoyResources {
				for node := range nodesToSend {
					_ = eic.dispatchResource(&envoyResource, model.DeleteOperation, node)
					switch envoyResource.Kind {
					case SECRET:
						eic.secretStoreLock.Lock()
						delete(eic.secretStore, envoyResource.Name)
						eic.secretStoreLock.Unlock()
					case ENDPOINT:
						eic.endpointStoreLock.Lock()
						delete(eic.endpointStore, envoyResource.Name)
						eic.endpointStoreLock.Unlock()
					case CLUSTER:
						eic.clusterStoreLock.Lock()
						delete(eic.clusterStore, envoyResource.Name)
						eic.clusterStoreLock.Unlock()
					case ROUTE:
						eic.routeStoreLock.Lock()
						delete(eic.routeStore, envoyResource.Name)
						eic.routeStoreLock.Unlock()
					case LISTENER:
						eic.listenerStoreLock.Lock()
						delete(eic.listenerStore, envoyResource.Name)
						eic.listenerStoreLock.Unlock()
					}
				}
			}
			eic.ingressToResourceNameStoreLock.Lock()
			delete(eic.ingressToResourceNameStore, key)
			eic.ingressToResourceNameStoreLock.Unlock()
			return nil
		}
		if err != nil {
			return fmt.Errorf("unable to retrieve ingress %v from store: %v", key, err)
		}
		ingress = toV1Ingress(v1beta1Ingress)
	}
	//if err != nil {
	//	return fmt.Errorf("unable to retrieve ingress %v from store: %v", key, err)
	//}

	nodegroup := strings.Split(ingress.Annotations[ENVOYINGRESSNODEGROUPANNOTATION], ";")
	for _, v := range nodegroup {
		if len(v) != 0 {
			eic.ingressNodeGroupStore[key] = append(eic.ingressNodeGroupStore[key], envoy_cache.NodeGroup(v))
		}
	}
	// TODO: the following code need to consider whether the resource needs to be sent
	secrets, _ := eic.getSecretsForIngress(ingress)
	for _, secret := range secrets {
		eic.secretStoreLock.Lock()
		if sec, ok := eic.secretStore[secret.Name]; !ok || sec.Secret.Name != secret.Secret.Name || !reflect.DeepEqual(sec.Secret.Type, secret.Secret.Type) {
			eic.secretStore[secret.Name] = secret
			eic.ingressToResourceNameStoreLock.Lock()
			eic.ingressToResourceNameStore[key] = append(eic.ingressToResourceNameStore[key], EnvoyResource{Name: secret.Name, Kind: SECRET})
			eic.ingressToResourceNameStoreLock.Unlock()
			eic.resourceNeedToBeSentToEdgeStoreLock.Lock()
			eic.resourceNeedToBeSentToEdgeStore = append(eic.resourceNeedToBeSentToEdgeStore, EnvoyResource{Name: secret.Name, Kind: SECRET})
			eic.resourceNeedToBeSentToEdgeStoreLock.Unlock()
		}
		eic.secretStoreLock.Unlock()
	}
	endpoints, err := eic.getEndpointsForIngress(ingress)
	if err != nil {
		klog.Warning(err)
	} else {
		for _, endpoint := range endpoints {
			eic.endpointStoreLock.Lock()
			if edp, ok := eic.endpointStore[endpoint.Name]; !ok || edp.ClusterLoadAssignment.ClusterName != endpoint.ClusterLoadAssignment.ClusterName ||
				!reflect.DeepEqual(edp.ClusterLoadAssignment.Endpoints, endpoint.ClusterLoadAssignment.Endpoints) ||
				!reflect.DeepEqual(edp.ClusterLoadAssignment.NamedEndpoints, endpoint.ClusterLoadAssignment.NamedEndpoints) ||
				!reflect.DeepEqual(edp.ClusterLoadAssignment.Policy, endpoint.ClusterLoadAssignment.Policy) {
				eic.endpointStore[endpoint.Name] = endpoint
				eic.ingressToResourceNameStoreLock.Lock()
				eic.ingressToResourceNameStore[key] = append(eic.ingressToResourceNameStore[key], EnvoyResource{Name: endpoint.Name, Kind: ENDPOINT})
				eic.ingressToResourceNameStoreLock.Unlock()
				eic.resourceNeedToBeSentToEdgeStoreLock.Lock()
				eic.resourceNeedToBeSentToEdgeStore = append(eic.resourceNeedToBeSentToEdgeStore, EnvoyResource{Name: endpoint.Name, Kind: ENDPOINT})
				eic.resourceNeedToBeSentToEdgeStoreLock.Unlock()
			}
			eic.endpointStoreLock.Unlock()
		}
	}
	clusters, err := eic.getClustersForIngress(ingress)
	if err != nil {
		klog.Warning(err)
	} else {
		for _, cluster := range clusters {
			eic.clusterStoreLock.Lock()
			// TODO: do not copy lock
			if cls, ok := eic.clusterStore[cluster.Name]; !ok || !reflect.DeepEqual(cls.Cluster, cluster.Cluster) {
				eic.clusterStore[cluster.Name] = cluster
				eic.ingressToResourceNameStoreLock.Lock()
				eic.ingressToResourceNameStore[key] = append(eic.ingressToResourceNameStore[key], EnvoyResource{Name: cluster.Name, Kind: CLUSTER})
				eic.ingressToResourceNameStoreLock.Unlock()
				eic.resourceNeedToBeSentToEdgeStoreLock.Lock()
				eic.resourceNeedToBeSentToEdgeStore = append(eic.resourceNeedToBeSentToEdgeStore, EnvoyResource{Name: cluster.Name, Kind: CLUSTER})
				eic.resourceNeedToBeSentToEdgeStoreLock.Unlock()
			}
			eic.clusterStoreLock.Unlock()
		}
	}
	route, err := eic.getRouteForIngress(ingress)
	if err != nil {
		klog.Warning(err)
	} else {
		eic.routeStoreLock.Lock()
		// TODO: do not copy lock
		if rte, ok := eic.routeStore[route.Name]; !ok || !reflect.DeepEqual(rte.RouteConfiguration, route.RouteConfiguration) {
			eic.routeStore[route.Name] = route
			eic.ingressToResourceNameStoreLock.Lock()
			eic.ingressToResourceNameStore[key] = append(eic.ingressToResourceNameStore[key], EnvoyResource{Name: route.Name, Kind: ROUTE})
			eic.ingressToResourceNameStoreLock.Unlock()
			eic.resourceNeedToBeSentToEdgeStoreLock.Lock()
			eic.resourceNeedToBeSentToEdgeStore = append(eic.resourceNeedToBeSentToEdgeStore, EnvoyResource{Name: route.Name, Kind: ROUTE})
			eic.resourceNeedToBeSentToEdgeStoreLock.Unlock()
		}
		eic.routeStoreLock.Unlock()
	}
	listeners, err := eic.getListenersForIngress(ingress)
	if err != nil {
		klog.Warning(err)
	}
	for _, listener := range listeners {
		eic.listenerStoreLock.Lock()
		// TODO: do not copy lock
		if lis, ok := eic.listenerStore[listener.Name]; !ok || !reflect.DeepEqual(lis.Listener, listener.Listener) {
			eic.listenerStore[listener.Name] = listener
			eic.ingressToResourceNameStoreLock.Lock()
			eic.ingressToResourceNameStore[key] = append(eic.ingressToResourceNameStore[key], EnvoyResource{Name: listener.Name, Kind: LISTENER})
			eic.ingressToResourceNameStoreLock.Unlock()
			eic.resourceNeedToBeSentToEdgeStoreLock.Lock()
			eic.resourceNeedToBeSentToEdgeStore = append(eic.resourceNeedToBeSentToEdgeStore, EnvoyResource{Name: listener.Name, Kind: LISTENER})
			eic.resourceNeedToBeSentToEdgeStoreLock.Unlock()
		}
		eic.listenerStoreLock.Unlock()
	}
	return nil
}

//TODO:restructrue send message model
func (eic *EnvoyIngressController) dispatchResource(envoyResource *EnvoyResource, opr string, node string) error {
	switch envoyResource.Kind {
	case SECRET:
		secret, ok := eic.secretStore[envoyResource.Name]
		if !ok {
			err := fmt.Errorf("couldn't get secret %s from secret store", envoyResource.Name)
			utilruntime.HandleError(err)
			return err
		}
		resource, err := messagelayer.BuildResource(node, secret.Namespace, string(SECRET), envoyResource.Name)
		if err != nil {
			klog.Warningf("built message resource failed with error: %s", err)
			return err
		}
		secretpb, err := proto.Marshal(&secret.Secret)
		if err != nil {
			klog.Warningf("failed to marshal secret into protobuf bytes, err: %s", err)
			return err
		}
		content := base64.StdEncoding.EncodeToString(secretpb)
		msg := model.NewMessage("").SetResourceVersion(secret.ResourceVersion).
			BuildRouter(ENVOYINGRESSCONTROLLERNAME, GROUPRESOURCE, resource, opr).
			FillBody(content)
		err = eic.messageLayer.Send(*msg)
		if err != nil {
			klog.Warningf("send message failed with error: %s, operation: %s, resource: %s", err, msg.GetOperation(), msg.GetResource())
			return err
		}
		klog.V(4).Infof("send message successfully, operation: %s, resource: %s", msg.GetOperation(), msg.GetResource())
	case ENDPOINT:
		endpoint, ok := eic.endpointStore[envoyResource.Name]
		if !ok {
			err := fmt.Errorf("couldn't get endpoint %s from endpoint store", envoyResource.Name)
			utilruntime.HandleError(err)
			return err
		}
		resource, err := messagelayer.BuildResource(node, endpoint.Namespace, string(ENDPOINT), envoyResource.Name)
		if err != nil {
			klog.Warningf("built message resource failed with error: %s", err)
			return err
		}
		endpointpb, err := proto.Marshal(&endpoint.ClusterLoadAssignment)
		if err != nil {
			klog.Warningf("failed to marshal endpoint into protobuf bytes, err: %s", err)
			return err
		}
		content := base64.StdEncoding.EncodeToString(endpointpb)
		msg := model.NewMessage("").SetResourceVersion(endpoint.ResourceVersion).
			BuildRouter(ENVOYINGRESSCONTROLLERNAME, GROUPRESOURCE, resource, opr).
			FillBody(content)
		err = eic.messageLayer.Send(*msg)
		if err != nil {
			klog.Warningf("send message failed with error: %s, operation: %s, resource: %s", err, msg.GetOperation(), msg.GetResource())
			return err
		}
		klog.V(4).Infof("send message successfully, operation: %s, resource: %s", msg.GetOperation(), msg.GetResource())
	case CLUSTER:
		cluster, ok := eic.clusterStore[envoyResource.Name]
		if !ok {
			err := fmt.Errorf("couldn't get cluster %s from cluster store", envoyResource.Name)
			utilruntime.HandleError(err)
			return err
		}
		resource, err := messagelayer.BuildResource(node, cluster.Namespace, string(CLUSTER), envoyResource.Name)
		if err != nil {
			klog.Warningf("built message resource failed with error: %s", err)
			return err
		}
		clusterpb, err := proto.Marshal(&cluster.Cluster)
		if err != nil {
			klog.Warningf("failed to marshal cluster into protobuf bytes, err: %s", err)
			return err
		}
		content := base64.StdEncoding.EncodeToString(clusterpb)
		msg := model.NewMessage("").SetResourceVersion(cluster.ResourceVersion).
			BuildRouter(ENVOYINGRESSCONTROLLERNAME, GROUPRESOURCE, resource, opr).
			FillBody(content)
		err = eic.messageLayer.Send(*msg)
		if err != nil {
			klog.Warningf("send message failed with error: %s, operation: %s, resource: %s", err, msg.GetOperation(), msg.GetResource())
			return err
		}
		klog.V(4).Infof("send message successfully, operation: %s, resource: %s", msg.GetOperation(), msg.GetResource())
	case ROUTE:
		route, ok := eic.routeStore[envoyResource.Name]
		if !ok {
			err := fmt.Errorf("couldn't get route %s from route store", envoyResource.Name)
			utilruntime.HandleError(err)
			return err
		}
		resource, err := messagelayer.BuildResource(node, route.Namespace, string(ROUTE), envoyResource.Name)
		if err != nil {
			klog.Warningf("built message resource failed with error: %s", err)
			return err
		}
		routepb, err := proto.Marshal(&route.RouteConfiguration)
		if err != nil {
			klog.Warningf("failed to marshal route into protobuf bytes, err: %v", err)
			return err
		}
		content := base64.StdEncoding.EncodeToString(routepb)
		msg := model.NewMessage("").SetResourceVersion(route.ResourceVersion).
			BuildRouter(ENVOYINGRESSCONTROLLERNAME, GROUPRESOURCE, resource, opr).
			FillBody(content)
		err = eic.messageLayer.Send(*msg)
		if err != nil {
			klog.Warningf("send message failed with error: %s, operation: %s, resource: %s", err, msg.GetOperation(), msg.GetResource())
			return err
		}
		klog.V(4).Infof("send message successfully, operation: %s, resource: %s", msg.GetOperation(), msg.GetResource())
	case LISTENER:
		listener, ok := eic.listenerStore[envoyResource.Name]
		if !ok {
			err := fmt.Errorf("couldn't get listener %s from listener store", envoyResource.Name)
			utilruntime.HandleError(err)
			return err
		}
		resource, err := messagelayer.BuildResource(node, listener.Namespace, string(LISTENER), envoyResource.Name)
		if err != nil {
			klog.Warningf("built message resource failed with error: %s", err)
			return err
		}
		listenerpb, err := proto.Marshal(&listener.Listener)
		if err != nil {
			klog.Warningf("failed to marshal listener into protobuf bytes, err: %v", err)
			return err
		}
		content := base64.StdEncoding.EncodeToString(listenerpb)
		msg := model.NewMessage("").SetResourceVersion(listener.ResourceVersion).
			BuildRouter(ENVOYINGRESSCONTROLLERNAME, GROUPRESOURCE, resource, opr).
			FillBody(content)
		err = eic.messageLayer.Send(*msg)
		if err != nil {
			klog.Warningf("send message failed with error: %s, operation: %s, resource: %s", err, msg.GetOperation(), msg.GetResource())
			return err
		}
		klog.V(4).Infof("send message successfully, operation: %s, resource: %s", msg.GetOperation(), msg.GetResource())
	}
	return nil
}

//when node comes to running,send all the envoy resources to edge.
func (eic *EnvoyIngressController) syncAllResourcesToEdgeNodes(obj interface{}) {
	node, ok := obj.(*v1.Node)
	if !ok {
		klog.Warningf("Object type %T unsupported", obj)
		return
	}

	//send all secrets to edge
	for name, _ := range eic.secretStore {
		envoyResource := EnvoyResource{
			Name: name,
			Kind: SECRET,
		}
		eic.dispatchResource(&envoyResource, model.InsertOperation, node.Name)
	}

	//send all endpoints to the edge
	for name, _ := range eic.endpointStore {
		envoyResource := EnvoyResource{
			Name: name,
			Kind: ENDPOINT,
		}
		eic.dispatchResource(&envoyResource, model.InsertOperation, node.Name)
	}

	//send all clusters to the edge
	for name, _ := range eic.clusterStore {
		envoyResource := EnvoyResource{
			Name: name,
			Kind: CLUSTER,
		}
		eic.dispatchResource(&envoyResource, model.InsertOperation, node.Name)
	}

	//send all routes to the edge
	for name, _ := range eic.routeStore {
		envoyResource := EnvoyResource{
			Name: name,
			Kind: ROUTE,
		}
		eic.dispatchResource(&envoyResource, model.InsertOperation, node.Name)
	}

	//send all listeners to the edge
	for name, _ := range eic.listenerStore {
		envoyResource := EnvoyResource{
			Name: name,
			Kind: LISTENER,
		}
		eic.dispatchResource(&envoyResource, model.InsertOperation, node.Name)
	}
}

// initiateEnvoyResources generates all the corresponding envoy resources for envoy ingress
// It should be called first when envoy ingress controller starts to run.
func (eic *EnvoyIngressController) initiateEnvoyResources() error {
	ingressList, err := eic.ingressLister.List(labels.Everything())
	if err != nil {
		return err
	}
	v1beta1IngressList, err := eic.v1beta1IngressLister.List(labels.Everything())
	if err != nil {
		return err
	}
	// merge v1beta1 ingress and v1 ingress into a list
	for _, v1beta1Ingress := range v1beta1IngressList {
		ingressList = append(ingressList, toV1Ingress(v1beta1Ingress))
	}

	for _, ingress := range ingressList {
		// ignore ingresses which is not maintained by envoy ingress controller
		if ingress.Annotations[INGRESSCLASSANNOTATION] != ENVOYINGRESSCONTROLLERNAME {
			continue
		}
		key, err := controller.KeyFunc(ingress)
		if err != nil {
			utilruntime.HandleError(fmt.Errorf("couldn't get key for object %+v: %v", ingress, err))
			continue
		}
		nodegroup := strings.Split(ingress.Annotations[ENVOYINGRESSNODEGROUPANNOTATION], ";")
		for _, v := range nodegroup {
			if len(v) != 0 {
				eic.ingressNodeGroupStore[key] = append(eic.ingressNodeGroupStore[key], envoy_cache.NodeGroup(v))
			}
		}
		secrets, _ := eic.getSecretsForIngress(ingress)
		for _, secret := range secrets {
			if sec, ok := eic.secretStore[secret.Name]; !ok || !reflect.DeepEqual(sec.Secret, secret.Secret) {
				eic.secretStore[secret.Name] = secret
				eic.ingressToResourceNameStore[key] = append(eic.ingressToResourceNameStore[key], EnvoyResource{Name: secret.Name, Kind: SECRET})
				eic.resourceNeedToBeSentToEdgeStore = append(eic.resourceNeedToBeSentToEdgeStore, EnvoyResource{Name: secret.Name, Kind: SECRET})
			}
		}
		endpoints, err := eic.getEndpointsForIngress(ingress)
		if err != nil {
			continue
		}
		for _, endpoint := range endpoints {
			if edp, ok := eic.endpointStore[endpoint.Name]; !ok || !reflect.DeepEqual(endpoint.ClusterLoadAssignment, edp.ClusterLoadAssignment) {
				eic.endpointStore[endpoint.Name] = endpoint
				eic.ingressToResourceNameStore[key] = append(eic.ingressToResourceNameStore[key], EnvoyResource{Name: endpoint.Name, Kind: ENDPOINT})
				eic.resourceNeedToBeSentToEdgeStore = append(eic.resourceNeedToBeSentToEdgeStore, EnvoyResource{Name: endpoint.Name, Kind: ENDPOINT})
			}
		}
		clusters, err := eic.getClustersForIngress(ingress)
		if err != nil {
			continue
		}
		for _, cluster := range clusters {
			if cls, ok := eic.clusterStore[cluster.Name]; !ok || !reflect.DeepEqual(cls.Cluster, cluster.Cluster) {
				eic.clusterStore[cluster.Name] = cluster
				eic.ingressToResourceNameStore[key] = append(eic.ingressToResourceNameStore[key], EnvoyResource{Name: cluster.Name, Kind: CLUSTER})
				eic.resourceNeedToBeSentToEdgeStore = append(eic.resourceNeedToBeSentToEdgeStore, EnvoyResource{Name: cluster.Name, Kind: CLUSTER})
			}
		}
		route, err := eic.getRouteForIngress(ingress)
		if err != nil {
			continue
		}
		if rte, ok := eic.routeStore[route.Name]; !ok || !reflect.DeepEqual(rte.RouteConfiguration, route.RouteConfiguration) {
			eic.routeStore[route.Name] = route
			eic.ingressToResourceNameStore[key] = append(eic.ingressToResourceNameStore[key], EnvoyResource{Name: route.Name, Kind: ROUTE})
			eic.resourceNeedToBeSentToEdgeStore = append(eic.resourceNeedToBeSentToEdgeStore, EnvoyResource{Name: route.Name, Kind: ROUTE})
		}
		listeners, err := eic.getListenersForIngress(ingress)
		if err != nil {
			continue
		}
		for _, listener := range listeners {
			if lis, ok := eic.listenerStore[listener.Name]; !ok || !reflect.DeepEqual(lis.Listener, listener.Listener) {
				eic.listenerStore[listener.Name] = listener
				eic.ingressToResourceNameStore[key] = append(eic.ingressToResourceNameStore[key], EnvoyResource{Name: listener.Name, Kind: LISTENER})
				eic.resourceNeedToBeSentToEdgeStore = append(eic.resourceNeedToBeSentToEdgeStore, EnvoyResource{Name: listener.Name, Kind: LISTENER})
			}
		}
	}

	return nil
}

func (eic *EnvoyIngressController) initCache() error {
	nodeList, err := eic.nodeLister.List(labels.Everything())

	if err != nil {
		utilruntime.HandleError(fmt.Errorf("cloudn't get clusters's node list"))
		return err
	}

	for _, node := range nodeList {
		if _, ok := node.Labels[constants.NODEGROUPLABEL]; !ok {
			continue
		}
		// initiateNodeGroupsWithNodes should be called first when the controller begins to run.
		// It list all nodes in the cluster and read the labels of nodes to build relationship of node and there group.
		eic.lc.UpdateNodeGroup(node)
		eic.lc.UpdateEdgeNode(node)
	}

	return nil
}
