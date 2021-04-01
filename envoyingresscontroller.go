package envoyingresscontroller

import (
	beehiveContext "github.com/kubeedge/beehive/pkg/core/context"
	clientset "k8s.io/client-go/kubernetes"
	corelisters "k8s.io/client-go/listers/core/v1"
	networkingListers "k8s.io/client-go/listers/networking/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/workqueue"
	"time"
	"github.com/kubeedge/beehive/pkg/core/model"
)

const(

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
}

// EnvoyIngressController is responsible for converting envoy ingress to envoy configuration
// and synchronizing it to cloudhub which will dispatch it's received objects to edgehub.
type EnvoyIngressController struct{
	enable bool
	kubeClient clientset.Interface
	kubeedgeClient KubeedgeClient

	envoyIngressControllerConfiguration EnvoyIngressControllerConfiguration

	eventRecorder record.EventRecorder

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
func NewEnvoyIngressController()(){}

// Register registers envoy ingress controller to beehive core.
func Register(){}


func (eic *EnvoyIngressController) Name() string {}

func (eic *EnvoyIngressController) Group() string {}

func (eic *EnvoyIngressController) Enable() bool {}

func (eic *EnvoyIngressController) Start() {}

func (eic *EnvoyIngressController) addPod(obj interface{}){}

func (eic *EnvoyIngressController) updatePod(obj interface){}

func (eic *EnvoyIngressController) deletePod(obj interface){}

func (eic *EnvoyIngressController) addNode(obj interface{}){}

func (eic *EnvoyIngressController) updateNode(obj interface){}

func (eic *EnvoyIngressController) deleteNode(obj interface){}

func (eic *EnvoyIngressController) addService(obj interface{}){}

func (eic *EnvoyIngressController) updateService(obj interface){}

func (eic *EnvoyIngressController) deleteService(obj interface){}

func (eic *EnvoyIngressController) addIngress(obj interface{}){}

func (eic *EnvoyIngressController) updateIngress(obj interface){}

func (eic *EnvoyIngressController) deleteIngress(obj interface){}

func (eic *EnvoyIngressController) Run(workers int, stopCh <-chan struct{}) {}

func (eic *EnvoyIngressController) processNextWorkItem() bool {}

func (eic *EnvoyIngressController) enqueue() {}

func (eic *EnvoyIngressController) storeIngressStatus(){}

func (eic *EnvoyIngressController) updateIngressStatus(){}