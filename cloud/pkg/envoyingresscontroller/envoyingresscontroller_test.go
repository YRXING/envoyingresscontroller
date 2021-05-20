package envoyingresscontroller

import (
	"fmt"
	"math/rand"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/kubeedge/kubeedge/tests/e2e/utils"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	"github.com/kubeedge/kubeedge/cloud/pkg/envoyingresscontroller/constants"

	envoy_cache "github.com/kubeedge/kubeedge/cloud/pkg/envoyingresscontroller/cache"

	"github.com/kubeedge/beehive/pkg/core"
	beehiveContext "github.com/kubeedge/beehive/pkg/core/context"
	"github.com/kubeedge/kubeedge/cloud/pkg/cloudhub/common/model"
	"github.com/kubeedge/kubeedge/cloud/pkg/common/modules"
	"github.com/kubeedge/kubeedge/cloud/pkg/envoyingresscontroller/config"
	"github.com/kubeedge/kubeedge/pkg/apis/componentconfig/cloudcore/v1alpha1"
	v1 "k8s.io/api/core/v1"
	ingressv1 "k8s.io/api/networking/v1"
	v1beta1Ingress "k8s.io/api/networking/v1beta1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apiserver/pkg/storage/names"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"
	"k8s.io/klog/v2"
	"k8s.io/kubernetes/pkg/controller"
	"k8s.io/kubernetes/pkg/securitycontext"
)

var (
	alwaysReady = func() bool { return true }
)

type envoyIngressController struct {
	*EnvoyIngressController
	ingressStore        cache.Store
	v1Beta1IngressStore cache.Store
	nodeStore           cache.Store
	serviceStore        cache.Store
	endpointStore       cache.Store
	secretStore         cache.Store
	podStore            cache.Store
	fakeRecorder        *record.FakeRecorder
}

func newNode(name string, label map[string]string) *v1.Node {
	return &v1.Node{
		TypeMeta: metav1.TypeMeta{APIVersion: "v1"},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Labels:    label,
			Namespace: metav1.NamespaceNone,
		},
		Status: v1.NodeStatus{
			Conditions: []v1.NodeCondition{
				{Type: v1.NodeReady, Status: v1.ConditionTrue},
			},
			Allocatable: v1.ResourceList{
				v1.ResourcePods: resource.MustParse("100"),
			},
		},
	}
}

func addNodes(nodeStore cache.Store, startIndex, numNodes int, label map[string]string) {
	for i := startIndex; i < startIndex+numNodes; i++ {
		err := nodeStore.Add(newNode(fmt.Sprintf("node-%d", i), label))
		if err != nil {
			klog.Error(err)
		}
	}
}

func newIngress(name string, annotation map[string]string, serviceName string, serviceBackendPort ingressv1.ServiceBackendPort) *ingressv1.Ingress {
	pathType := ingressv1.PathTypePrefix
	return &ingressv1.Ingress{
		TypeMeta: metav1.TypeMeta{APIVersion: "networking.k8s.io/v1"},
		ObjectMeta: metav1.ObjectMeta{
			Name:        name,
			Namespace:   metav1.NamespaceDefault,
			Annotations: annotation,
		},
		Spec: ingressv1.IngressSpec{
			Rules: []ingressv1.IngressRule{
				{
					Host: "bar.foo.com",
					IngressRuleValue: ingressv1.IngressRuleValue{
						HTTP: &ingressv1.HTTPIngressRuleValue{
							Paths: []ingressv1.HTTPIngressPath{
								{
									Path:     "/login",
									PathType: &pathType,
									Backend: ingressv1.IngressBackend{
										Service: &ingressv1.IngressServiceBackend{
											Name: serviceName,
											Port: serviceBackendPort,
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}
}

func addIngresses(ingressStore cache.Store, number int, annotation map[string]string, serviceName []string, serviceBackendPort []ingressv1.ServiceBackendPort) {
	for i := 0; i < number; i++ {
		err := ingressStore.Add(newIngress(fmt.Sprintf("ingress-%d", i), annotation, serviceName[i], serviceBackendPort[i]))
		if err != nil {
			klog.Error(err)
		}
	}
}

func newV1beta1Ingress(name string, annotation map[string]string, serviceName string, serviceBackendPort intstr.IntOrString) *v1beta1Ingress.Ingress {
	pathType := v1beta1Ingress.PathTypePrefix
	return &v1beta1Ingress.Ingress{
		TypeMeta: metav1.TypeMeta{APIVersion: "networking.k8s.io/v1beta1"},
		ObjectMeta: metav1.ObjectMeta{
			Name:        name,
			Namespace:   metav1.NamespaceDefault,
			Annotations: annotation,
		},
		Spec: v1beta1Ingress.IngressSpec{
			Rules: []v1beta1Ingress.IngressRule{
				{
					Host: "bar.foo.com",
					IngressRuleValue: v1beta1Ingress.IngressRuleValue{
						HTTP: &v1beta1Ingress.HTTPIngressRuleValue{
							Paths: []v1beta1Ingress.HTTPIngressPath{
								{
									Path:     "/login",
									PathType: &pathType,
									Backend: v1beta1Ingress.IngressBackend{
										ServiceName: serviceName,
										ServicePort: serviceBackendPort,
									},
								},
							},
						},
					},
				},
			},
		},
	}
}

func addV1beta1Ingresses(v1beta1IngressStore cache.Store, number int, annotation map[string]string, serviceName []string, serviceBackendPort []intstr.IntOrString) {
	for i := 0; i < number; i++ {
		err := v1beta1IngressStore.Add(newV1beta1Ingress(fmt.Sprintf("v1beta1Ingress-%d", i), annotation, serviceName[i], serviceBackendPort[i]))
		if err != nil {
			klog.Error(err)
		}
	}
}

func newService(name string, selector map[string]string, portName string, portNumber int32) *v1.Service {
	return &v1.Service{
		TypeMeta: metav1.TypeMeta{APIVersion: "v1"},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: metav1.NamespaceDefault,
		},
		Spec: v1.ServiceSpec{
			Selector: selector,
			Ports: []v1.ServicePort{
				{
					Name:     portName,
					Port:     portNumber,
					Protocol: "TCP",
				},
			},
		},
	}
}

func addServices(serviceStore cache.Store, number int, selector map[string]string) {
	for i := 0; i < number; i++ {
		err := serviceStore.Add(newService(fmt.Sprintf("service-%d", i), selector, "servicePort-%d", int32(rand.Intn(1000)+100)))
		if err != nil {
			klog.Error(err)
		}
	}
}

func newPod(podName string, nodeName string, label map[string]string) *v1.Pod {
	podSpec := v1.PodSpec{
		Containers: []v1.Container{
			{
				Image:                  "foo/bar",
				TerminationMessagePath: v1.TerminationMessagePathDefault,
				ImagePullPolicy:        v1.PullIfNotPresent,
				SecurityContext:        securitycontext.ValidSecurityContextWithContainerDefaults(),
			},
		},
	}
	// Add node name to the pod
	if len(nodeName) > 0 {
		podSpec.NodeName = nodeName
	}

	pod := &v1.Pod{
		TypeMeta: metav1.TypeMeta{APIVersion: "v1"},
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: podName,
			Labels:       label,
			Namespace:    metav1.NamespaceDefault,
		},
		Spec: podSpec,
		Status: v1.PodStatus{
			PodIP: "10.1.10.1",
		},
	}
	pod.Name = names.SimpleNameGenerator.GenerateName(podName)
	return pod
}

func addPods(podStore cache.Store, nodeName string, label map[string]string, number int) {
	for i := 0; i < number; i++ {
		err := podStore.Add(newPod(fmt.Sprintf("pod-%dd", i), nodeName, label))
		if err != nil {
			klog.Error(err)
		}
	}
}

func newEndpoint(pod *v1.Pod, service *v1.Service) *v1.Endpoints {
	return &v1.Endpoints{
		TypeMeta: metav1.TypeMeta{APIVersion: "v1"},
		ObjectMeta: metav1.ObjectMeta{
			Name:      service.ObjectMeta.Name,
			Namespace: metav1.NamespaceDefault,
		},
		Subsets: []v1.EndpointSubset{
			{
				Addresses: []v1.EndpointAddress{
					{
						IP:       pod.Status.PodIP,
						NodeName: &pod.Spec.NodeName,
						TargetRef: &v1.ObjectReference{
							Kind:            "Pod",
							Namespace:       pod.ObjectMeta.Namespace,
							Name:            pod.ObjectMeta.Name,
							UID:             pod.ObjectMeta.UID,
							ResourceVersion: pod.ObjectMeta.ResourceVersion,
						},
					},
				},
				Ports: []v1.EndpointPort{
					{
						Name:     service.Spec.Ports[0].Name,
						Protocol: service.Spec.Ports[0].Protocol,
						Port:     service.Spec.Ports[0].Port,
					},
				},
			},
		},
	}
}

func addEndpoints(endpointStore cache.Store, serviceStore cache.Store, podStore cache.Store) {
	serviceList := serviceStore.List()
	podList := podStore.List()
	for _, _service := range serviceList {
		for _, _pod := range podList {
			service := _service.(*v1.Service)
			pod := _pod.(*v1.Pod)
			var labelSelector = &metav1.LabelSelector{}
			err := metav1.Convert_Map_string_To_string_To_v1_LabelSelector(&service.Spec.Selector, labelSelector, nil)
			if err != nil {
				fmt.Print(err)
			}
			selector, _ := metav1.LabelSelectorAsSelector(labelSelector)
			//If a service with a nil or empty selector creeps in, it should match nothing, not everything
			if selector.Matches(labels.Set(pod.Labels)) {
				endpointStore.Add(newEndpoint(pod, service))
			}
		}
	}
}

func newSecret(name, cert, key string) *v1.Secret {
	return &v1.Secret{
		TypeMeta: metav1.TypeMeta{APIVersion: "v1"},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: metav1.NamespaceDefault,
		},
		StringData: map[string]string{
			v1.TLSCertKey:       cert,
			v1.TLSPrivateKeyKey: key,
		},
		Type: v1.SecretTypeTLS,
	}
}

func addSecrets(secretStore cache.Store, number int) {
	for i := 0; i < number; i++ {
		err := secretStore.Add(newSecret(fmt.Sprintf("secret-%d", i), "cert", "key"))
		if err != nil {
			klog.Error(err)
		}
	}
}

func newTestController(initialObjects ...runtime.Object) (*envoyIngressController, *fake.Clientset, error) {
	clientset := fake.NewSimpleClientset(initialObjects...)
	informerFactory := informers.NewSharedInformerFactory(clientset, controller.NoResyncPeriodFunc())

	eic, err := NewEnvoyIngressController(
		informerFactory.Core().V1().Secrets(),
		informerFactory.Core().V1().Endpoints(),
		informerFactory.Networking().V1().Ingresses(),
		informerFactory.Networking().V1beta1().Ingresses(),
		informerFactory.Core().V1().Nodes(),
		informerFactory.Core().V1().Services(),
		EICConfiguration{
			syncInterval:                 1,
			envoyServiceSyncInterval:     1,
			ingressSyncWorkerNumber:      5,
			envoyServiceSyncWorkerNumber: 5,
		},
		clientset,
		true,
	)
	if err != nil {
		return nil, nil, err
	}

	fakeRecorder := record.NewFakeRecorder(100)
	eic.eventRecorder = fakeRecorder

	eic.ingressStoreSynced = alwaysReady
	eic.v1beta1IngressStoreSynced = alwaysReady
	eic.endpointStoreSynced = alwaysReady
	eic.serviceStoreSynced = alwaysReady
	eic.nodeStoreSynced = alwaysReady
	eic.secretStoreSynced = alwaysReady

	newEic := &envoyIngressController{
		EnvoyIngressController: eic,
		ingressStore:           informerFactory.Networking().V1().Ingresses().Informer().GetStore(),
		v1Beta1IngressStore:    informerFactory.Networking().V1beta1().Ingresses().Informer().GetStore(),
		nodeStore:              informerFactory.Core().V1().Nodes().Informer().GetStore(),
		serviceStore:           informerFactory.Core().V1().Services().Informer().GetStore(),
		endpointStore:          informerFactory.Core().V1().Endpoints().Informer().GetStore(),
		secretStore:            informerFactory.Core().V1().Secrets().Informer().GetStore(),
		podStore:               informerFactory.Core().V1().Pods().Informer().GetStore(),
		fakeRecorder:           fakeRecorder,
	}

	return newEic, clientset, nil
}

func TestDeletedFinalStateUnknown(t *testing.T) {
	var (
		podLabel = map[string]string{
			"app": "envoyingresstest",
		}
		annotation = map[string]string{
			"nginx.ingress.kubernetes.io/rewrite-target": "/",
			"v1alpha1.kubeedge.io/nodegroup":             "guiyang",
			"kubernetes.io/ingress.class":                "envoyingress",
		}
		nodeLabel = map[string]string{
			"nodegroup": "guiyang",
		}
		ingress *ingressv1.Ingress
	)
	config.InitConfigure(&v1alpha1.EnvoyIngressController{
		Context: &v1alpha1.ControllerContext{
			SendModule:     "cloudhub",
			ReceiveModule:  "envoyingresscontroller",
			ResponseModule: "cloudhub",
		},
	})
	manager, _, err := newTestController()
	if err != nil {
		t.Fatalf("error creating EnvoyIngress controller: %v", err)
	}
	addNodes(manager.nodeStore, 0, 1, nodeLabel)
	nodeList := manager.nodeStore.List()
	for _, _node := range nodeList {
		node := _node.(*v1.Node)
		addPods(manager.podStore, node.ObjectMeta.Name, podLabel, 3)
	}
	addServices(manager.serviceStore, 1, podLabel)
	addEndpoints(manager.endpointStore, manager.serviceStore, manager.podStore)
	for _, _service := range manager.serviceStore.List() {
		service := _service.(*v1.Service)
		ingress = newIngress("ingressTest", annotation, service.Name,
			ingressv1.ServiceBackendPort{Name: service.Spec.Ports[0].Name, Number: service.Spec.Ports[0].Port})
	}
	manager.deleteIngress(cache.DeletedFinalStateUnknown{Key: "ingressTest", Obj: ingress})
	enqueuedKey, _ := manager.queue.Get()
	if enqueuedKey.(string) != "default/ingressTest" {
		t.Errorf("expected delete of DeletedFinalStateUnknown to enqueue the ingress but found: %#v", enqueuedKey)
	}
}

func validateSyncEnvoyIngress(manager *envoyIngressController) error {
	return nil
}

func syncAndValidateEnvoyIngress(manager *envoyIngressController, ingress *ingressv1.Ingress) error {
	key, err := controller.KeyFunc(ingress)
	if err != nil {
		return fmt.Errorf("could not get key for ingress")
	}

	err = manager.syncHandler(key)
	if err != nil {
		klog.Warning(err)
	}

	err = validateSyncEnvoyIngress(manager)
	if err != nil {
		return err
	}

	return nil
}

func TestInitNodeGroupsWithNodes(t *testing.T) {
	var (
		nodeLabel = map[string]string{
			"nodegroup":           "guiyang",
			constants.NodeRoleKey: constants.NodeRoleValue,
		}
	)
	config.InitConfigure(&v1alpha1.EnvoyIngressController{
		Context: &v1alpha1.ControllerContext{
			SendModule:     "cloudhub",
			ReceiveModule:  "envoyingresscontroller",
			ResponseModule: "cloudhub",
		},
	})
	manager, _, err := newTestController()
	if err != nil {
		t.Fatalf("error creating EnvoyIngress controller: %v", err)
	}
	addNodes(manager.nodeStore, 0, 1, nodeLabel)
	err = manager.EnvoyIngressController.initCache()
	if err != nil {
		t.Fatalf("Failed to initiate nodegroups with nodes, err: %v", err)
	}
	if string(manager.EnvoyIngressController.lc.Node2group["node-0"][0]) != "guiyang" || manager.EnvoyIngressController.lc.Group2node[envoy_cache.NodeGroup("guiyang")][0] != "node-0" {
		t.Errorf("Fail to initiate nodegroup relationship")
	}
}

func TestAddNode(t *testing.T) {
	var (
		nodeLabel = map[string]string{
			"nodegroup": "guiyang;shanxi",
		}
	)
	config.InitConfigure(&v1alpha1.EnvoyIngressController{
		Context: &v1alpha1.ControllerContext{
			SendModule:     "cloudhub",
			ReceiveModule:  "envoyingresscontroller",
			ResponseModule: "cloudhub",
		},
	})
	manager, _, err := newTestController()
	if err != nil {
		t.Fatalf("error creating EnvoyIngress controller: %v", err)
	}
	node1 := newNode("node-0", nodeLabel)
	node2 := newNode("node-1", nodeLabel)
	manager.addNode(node1)
	manager.addNode(node2)
	if !reflect.DeepEqual(manager.EnvoyIngressController.lc.Node2group["node-0"], []envoy_cache.NodeGroup{"guiyang", "shanxi"}) ||
		!reflect.DeepEqual(manager.EnvoyIngressController.lc.Group2node[envoy_cache.NodeGroup("guiyang")], []string{"node-0", "node-1"}) {
		t.Errorf("Fail to add nodes to nodegroup, %v, %v", manager.EnvoyIngressController.lc.Node2group["node-0"],
			manager.EnvoyIngressController.lc.Group2node[envoy_cache.NodeGroup("guiyang")])
	}
}

func TestUpdateNode(t *testing.T) {
	var (
		nodeLabel = map[string]string{
			"nodegroup": "guiyang;shanxi",
		}
	)
	config.InitConfigure(&v1alpha1.EnvoyIngressController{
		Context: &v1alpha1.ControllerContext{
			SendModule:     "cloudhub",
			ReceiveModule:  "envoyingresscontroller",
			ResponseModule: "cloudhub",
		},
	})
	manager, _, err := newTestController()
	if err != nil {
		t.Fatalf("error creating EnvoyIngress controller: %v", err)
	}
	oldNode := newNode("node-0", nodeLabel)
	curNode := oldNode
	curNode.Labels = map[string]string{"nodegroup": "zhejiang"}
	manager.addNode(oldNode)
	manager.updateNode(oldNode, curNode)
	if !reflect.DeepEqual(manager.EnvoyIngressController.lc.Node2group["node-0"], []envoy_cache.NodeGroup{"zhejiang"}) {
		t.Errorf("Fail to update nodes to nodegroup, %v", manager.EnvoyIngressController.lc.Group2node["zhejiang"])
	}
}

func TestDeleteNode(t *testing.T) {
	var (
		nodeLabel = map[string]string{
			"nodegroup": "guiyang;shanxi",
		}
	)
	config.InitConfigure(&v1alpha1.EnvoyIngressController{
		Context: &v1alpha1.ControllerContext{
			SendModule:     "cloudhub",
			ReceiveModule:  "envoyingresscontroller",
			ResponseModule: "cloudhub",
		},
	})
	manager, _, err := newTestController()
	if err != nil {
		t.Fatalf("error creating EnvoyIngress controller: %v", err)
	}
	node := newNode("node-0", nodeLabel)
	manager.addNode(node)
	manager.deleteNode(node)
	if len(manager.lc.Node2group["node-0"]) != 0 || len(manager.lc.Group2node["guiyang"]) != 0 ||
		len(manager.lc.Group2node["shanxi"]) != 0 {
		t.Errorf("Fail to delete nodes from nodegroup")
	}
}

func TestAddIngress(t *testing.T) {
	var (
		annotation = map[string]string{
			"nginx.ingress.kubernetes.io/rewrite-target": "/",
			"v1alpha1.kubeedge.io/nodegroup":             "guiyang",
			"kubernetes.io/ingress.class":                "envoyingress",
		}
	)
	config.InitConfigure(&v1alpha1.EnvoyIngressController{
		Context: &v1alpha1.ControllerContext{
			SendModule:     "cloudhub",
			ReceiveModule:  "envoyingresscontroller",
			ResponseModule: "cloudhub",
		},
	})
	manager, _, err := newTestController()
	if err != nil {
		t.Fatalf("error creating EnvoyIngress controller: %v", err)
	}
	ingress := newIngress("ingress-0", annotation, "envoyingresstest", ingressv1.ServiceBackendPort{Name: "http", Number: 9000})
	manager.addIngress(ingress)
	if manager.queue.Len() == 0 {
		t.Fatalf("failed to enqueue controller for ingress %v", ingress.Name)
	}
	key, done := manager.queue.Get()
	if key == nil || done {
		t.Fatalf("failed to enqueue controller for ingress %v", ingress.Name)
	}
	expectedKey, _ := controller.KeyFunc(ingress)
	if got, want := key.(string), expectedKey; got != want {
		t.Errorf("queue.Get() = %v, want %v", got, want)
	}
}

func TestUpdateIngress(t *testing.T) {
	var (
		annotation = map[string]string{
			"nginx.ingress.kubernetes.io/rewrite-target": "/",
			"v1alpha1.kubeedge.io/nodegroup":             "guiyang",
			"kubernetes.io/ingress.class":                "envoyingress",
		}
		annotation2 = map[string]string{
			"nginx.ingress.kubernetes.io/rewrite-target": "/",
			"v1alpha1.kubeedge.io/nodegroup":             "shanxi",
			"kubernetes.io/ingress.class":                "envoyingress",
		}
	)
	config.InitConfigure(&v1alpha1.EnvoyIngressController{
		Context: &v1alpha1.ControllerContext{
			SendModule:     "cloudhub",
			ReceiveModule:  "envoyingresscontroller",
			ResponseModule: "cloudhub",
		},
	})
	manager, _, err := newTestController()
	if err != nil {
		t.Fatalf("error creating EnvoyIngress controller: %v", err)
	}
	ingress := newIngress("ingress-0", annotation, "envoyingresstest", ingressv1.ServiceBackendPort{Name: "http", Number: 9000})
	curIngress := newIngress("ingress-0", annotation2, "envoyingresstest", ingressv1.ServiceBackendPort{Name: "http", Number: 9000})
	manager.updateIngress(ingress, curIngress)
	if manager.queue.Len() == 0 {
		t.Fatalf("failed to enqueue controller for ingress %v", ingress.Name)
	}
	key, done := manager.queue.Get()
	if key == nil || done {
		t.Fatalf("failed to enqueue controller for ingress %v", ingress.Name)
	}
	expectedKey, _ := controller.KeyFunc(ingress)
	if got, want := key.(string), expectedKey; got != want {
		t.Errorf("queue.Get() = %v, want %v", got, want)
	}
}

func TestDeleteIngress(t *testing.T) {
	var (
		annotation = map[string]string{
			"nginx.ingress.kubernetes.io/rewrite-target": "/",
			"v1alpha1.kubeedge.io/nodegroup":             "guiyang",
			"kubernetes.io/ingress.class":                "envoyingress",
		}
	)
	config.InitConfigure(&v1alpha1.EnvoyIngressController{
		Context: &v1alpha1.ControllerContext{
			SendModule:     "cloudhub",
			ReceiveModule:  "envoyingresscontroller",
			ResponseModule: "cloudhub",
		},
	})
	manager, _, err := newTestController()
	if err != nil {
		t.Fatalf("error creating EnvoyIngress controller: %v", err)
	}
	ingress := newIngress("ingress-0", annotation, "envoyingresstest", ingressv1.ServiceBackendPort{Name: "http", Number: 9000})
	manager.deleteIngress(ingress)
	if manager.queue.Len() == 0 {
		t.Fatalf("failed to enqueue controller for ingress %v", ingress.Name)
	}
	key, done := manager.queue.Get()
	if key == nil || done {
		t.Fatalf("failed to enqueue controller for ingress %v", ingress.Name)
	}
	expectedKey, _ := controller.KeyFunc(ingress)
	if got, want := key.(string), expectedKey; got != want {
		t.Errorf("queue.Get() = %v, want %v", got, want)
	}
}

func TestAddV1beta1Ingress(t *testing.T) {
	var (
		annotation = map[string]string{
			"nginx.ingress.kubernetes.io/rewrite-target": "/",
			"v1alpha1.kubeedge.io/nodegroup":             "guiyang",
			"kubernetes.io/ingress.class":                "envoyingress",
		}
	)
	config.InitConfigure(&v1alpha1.EnvoyIngressController{
		Context: &v1alpha1.ControllerContext{
			SendModule:     "cloudhub",
			ReceiveModule:  "envoyingresscontroller",
			ResponseModule: "cloudhub",
		},
	})
	manager, _, err := newTestController()
	if err != nil {
		t.Fatalf("error creating EnvoyIngress controller: %v", err)
	}
	ingress := newV1beta1Ingress("ingress-0", annotation, "envoyingresstest", intstr.IntOrString{Type: intstr.Int, IntVal: 9000})
	manager.addV1beta1Ingress(ingress)
	if manager.queue.Len() == 0 {
		t.Fatalf("failed to enqueue controller for ingress %v", ingress.Name)
	}
	key, done := manager.queue.Get()
	if key == nil || done {
		t.Fatalf("failed to enqueue controller for ingress %v", ingress.Name)
	}
	expectedKey, _ := controller.KeyFunc(ingress)
	if got, want := key.(string), expectedKey; got != want {
		t.Errorf("queue.Get() = %v, want %v", got, want)
	}
}

func TestUpdateV1beta1Ingress(t *testing.T) {
	var (
		annotation = map[string]string{
			"nginx.ingress.kubernetes.io/rewrite-target": "/",
			"v1alpha1.kubeedge.io/nodegroup":             "guiyang",
			"kubernetes.io/ingress.class":                "envoyingress",
		}
		annotation2 = map[string]string{
			"nginx.ingress.kubernetes.io/rewrite-target": "/",
			"v1alpha1.kubeedge.io/nodegroup":             "shanxi",
			"kubernetes.io/ingress.class":                "envoyingress",
		}
	)
	config.InitConfigure(&v1alpha1.EnvoyIngressController{
		Context: &v1alpha1.ControllerContext{
			SendModule:     "cloudhub",
			ReceiveModule:  "envoyingresscontroller",
			ResponseModule: "cloudhub",
		},
	})
	manager, _, err := newTestController()
	if err != nil {
		t.Fatalf("error creating EnvoyIngress controller: %v", err)
	}
	ingress := newV1beta1Ingress("ingress-0", annotation, "envoyingresstest", intstr.IntOrString{Type: intstr.Int, IntVal: 9000})
	curIngress := newV1beta1Ingress("ingress-0", annotation2, "envoyingresstest", intstr.IntOrString{Type: intstr.Int, IntVal: 9000})
	manager.updateV1beta1Ingress(ingress, curIngress)
	if manager.queue.Len() == 0 {
		t.Fatalf("failed to enqueue controller for ingress %v", ingress.Name)
	}
	key, done := manager.queue.Get()
	if key == nil || done {
		t.Fatalf("failed to enqueue controller for ingress %v", ingress.Name)
	}
	expectedKey, _ := controller.KeyFunc(ingress)
	if got, want := key.(string), expectedKey; got != want {
		t.Errorf("queue.Get() = %v, want %v", got, want)
	}
}

func TestDeleteV1beta1Ingress(t *testing.T) {
	var (
		annotation = map[string]string{
			"nginx.ingress.kubernetes.io/rewrite-target": "/",
			"v1alpha1.kubeedge.io/nodegroup":             "guiyang",
			"kubernetes.io/ingress.class":                "envoyingress",
		}
	)
	config.InitConfigure(&v1alpha1.EnvoyIngressController{
		Context: &v1alpha1.ControllerContext{
			SendModule:     "cloudhub",
			ReceiveModule:  "envoyingresscontroller",
			ResponseModule: "cloudhub",
		},
	})
	manager, _, err := newTestController()
	if err != nil {
		t.Fatalf("error creating EnvoyIngress controller: %v", err)
	}
	ingress := newV1beta1Ingress("ingress-0", annotation, "envoyingresstest", intstr.IntOrString{Type: intstr.Int, IntVal: 9000})
	manager.deleteV1beta1Ingress(ingress)
	if manager.queue.Len() == 0 {
		t.Fatalf("failed to enqueue controller for ingress %v", ingress.Name)
	}
	key, done := manager.queue.Get()
	if key == nil || done {
		t.Fatalf("failed to enqueue controller for ingress %v", ingress.Name)
	}
	expectedKey, _ := controller.KeyFunc(ingress)
	if got, want := key.(string), expectedKey; got != want {
		t.Errorf("queue.Get() = %v, want %v", got, want)
	}
}

func TestAddService(t *testing.T) {
	var (
		selector = map[string]string{
			"app": "envoyingress",
		}
		annotation = map[string]string{
			"nginx.ingress.kubernetes.io/rewrite-target": "/",
			"v1alpha1.kubeedge.io/nodegroup":             "guiyang",
			"kubernetes.io/ingress.class":                "envoyingress",
		}
	)
	config.InitConfigure(&v1alpha1.EnvoyIngressController{
		Context: &v1alpha1.ControllerContext{
			SendModule:     "cloudhub",
			ReceiveModule:  "envoyingresscontroller",
			ResponseModule: "cloudhub",
		},
	})
	manager, _, err := newTestController()
	if err != nil {
		t.Fatalf("error creating EnvoyIngress controller: %v", err)
	}
	ingress := newIngress("ingress-0", annotation, "service-0", ingressv1.ServiceBackendPort{Name: "http", Number: 9000})
	err = manager.ingressStore.Add(ingress)
	if err != nil {
		klog.Error(err)
	}
	service := newService("service-0", selector, "port-0", 9000)
	ingresses := manager.getIngressesForService(service)
	if len(ingresses) == 0 {
		t.Fatalf("failed to get ingresses for service %s in namespace %s", service.Name, service.Namespace)
	}
	manager.addService(service)
	if manager.queue.Len() == 0 {
		t.Fatalf("failed to enqueue controller for ingress %v", ingress.Name)
	}
	key, done := manager.queue.Get()
	if key == nil || done {
		t.Fatalf("failed to enqueue controller for ingress %v", ingress.Name)
	}
	expectedKey, _ := controller.KeyFunc(ingress)
	if got, want := key.(string), expectedKey; got != want {
		t.Errorf("queue.Get() = %v, want %v", got, want)
	}
}

func TestUpdateService(t *testing.T) {
	var (
		selector = map[string]string{
			"app": "envoyingress",
		}
		newSelector = map[string]string{
			"app": "nginxingress",
		}
		annotation = map[string]string{
			"nginx.ingress.kubernetes.io/rewrite-target": "/",
			"v1alpha1.kubeedge.io/nodegroup":             "guiyang",
			"kubernetes.io/ingress.class":                "envoyingress",
		}
	)
	config.InitConfigure(&v1alpha1.EnvoyIngressController{
		Context: &v1alpha1.ControllerContext{
			SendModule:     "cloudhub",
			ReceiveModule:  "envoyingresscontroller",
			ResponseModule: "cloudhub",
		},
	})
	manager, _, err := newTestController()
	if err != nil {
		t.Fatalf("error creating EnvoyIngress controller: %v", err)
	}
	ingress := newIngress("ingress-0", annotation, "service-0", ingressv1.ServiceBackendPort{Name: "http", Number: 9000})
	err = manager.ingressStore.Add(ingress)
	if err != nil {
		klog.Error(err)
	}
	service := newService("service-0", selector, "port-0", 9000)
	curService := newService("service-0", newSelector, "port-1", 9001)
	ingresses := manager.getIngressesForService(service)
	if len(ingresses) == 0 {
		t.Fatalf("failed to get ingresses for service %s in namespace %s", service.Name, service.Namespace)
	}
	manager.updateService(service, curService)
	if manager.queue.Len() == 0 {
		t.Fatalf("failed to enqueue controller for ingress %v", ingress.Name)
	}
	key, done := manager.queue.Get()
	if key == nil || done {
		t.Fatalf("failed to enqueue controller for ingress %v", ingress.Name)
	}
	expectedKey, _ := controller.KeyFunc(ingress)
	if got, want := key.(string), expectedKey; got != want {
		t.Errorf("queue.Get() = %v, want %v", got, want)
	}
}

func TestDeleteService(t *testing.T) {
	var (
		selector = map[string]string{
			"app": "envoyingress",
		}
		annotation = map[string]string{
			"nginx.ingress.kubernetes.io/rewrite-target": "/",
			"v1alpha1.kubeedge.io/nodegroup":             "guiyang",
			"kubernetes.io/ingress.class":                "envoyingress",
		}
	)
	config.InitConfigure(&v1alpha1.EnvoyIngressController{
		Context: &v1alpha1.ControllerContext{
			SendModule:     "cloudhub",
			ReceiveModule:  "envoyingresscontroller",
			ResponseModule: "cloudhub",
		},
	})
	manager, _, err := newTestController()
	if err != nil {
		t.Fatalf("error creating EnvoyIngress controller: %v", err)
	}
	ingress := newIngress("ingress-0", annotation, "service-0", ingressv1.ServiceBackendPort{Name: "http", Number: 9000})
	err = manager.ingressStore.Add(ingress)
	if err != nil {
		klog.Error(err)
	}
	service := newService("service-0", selector, "port-0", 9000)
	ingresses := manager.getIngressesForService(service)
	if len(ingresses) == 0 {
		t.Fatalf("failed to get ingresses for service %s in namespace %s", service.Name, service.Namespace)
	}
	manager.deleteService(service)
	if manager.queue.Len() == 0 {
		t.Fatalf("failed to enqueue controller for ingress %v", ingress.Name)
	}
	key, done := manager.queue.Get()
	if key == nil || done {
		t.Fatalf("failed to enqueue controller for ingress %v", ingress.Name)
	}
	expectedKey, _ := controller.KeyFunc(ingress)
	if got, want := key.(string), expectedKey; got != want {
		t.Errorf("queue.Get() = %v, want %v", got, want)
	}
}

func TestAddEndpoint(t *testing.T) {
	var (
		selector = map[string]string{
			"app": "envoyingress",
		}
		annotation = map[string]string{
			"nginx.ingress.kubernetes.io/rewrite-target": "/",
			"v1alpha1.kubeedge.io/nodegroup":             "guiyang",
			"kubernetes.io/ingress.class":                "envoyingress",
		}
	)
	config.InitConfigure(&v1alpha1.EnvoyIngressController{
		Context: &v1alpha1.ControllerContext{
			SendModule:     "cloudhub",
			ReceiveModule:  "envoyingresscontroller",
			ResponseModule: "cloudhub",
		},
	})
	manager, _, err := newTestController()
	if err != nil {
		t.Fatalf("error creating EnvoyIngress controller: %v", err)
	}
	ingress := newIngress("ingress-0", annotation, "service-0", ingressv1.ServiceBackendPort{Name: "http", Number: 9000})
	err = manager.ingressStore.Add(ingress)
	if err != nil {
		klog.Error(err)
	}
	service := newService("service-0", selector, "port-0", 9000)
	err = manager.serviceStore.Add(service)
	if err != nil {
		klog.Error(err)
	}
	pod := newPod("pod-0", "node-0", selector)
	err = manager.podStore.Add(pod)
	if err != nil {
		klog.Error(err)
	}
	endpoint := newEndpoint(pod, service)
	manager.addEndpoint(endpoint)
	if manager.queue.Len() == 0 {
		t.Fatalf("failed to enqueue controller for ingress %v", ingress.Name)
	}
	key, done := manager.queue.Get()
	if key == nil || done {
		t.Fatalf("failed to enqueue controller for ingress %v", ingress.Name)
	}
	expectedKey, _ := controller.KeyFunc(ingress)
	if got, want := key.(string), expectedKey; got != want {
		t.Errorf("queue.Get() = %v, want %v", got, want)
	}
}

func TestUpdateEndpoint(t *testing.T) {
	var (
		selector = map[string]string{
			"app": "envoyingress",
		}
		annotation = map[string]string{
			"nginx.ingress.kubernetes.io/rewrite-target": "/",
			"v1alpha1.kubeedge.io/nodegroup":             "guiyang",
			"kubernetes.io/ingress.class":                "envoyingress",
		}
	)
	config.InitConfigure(&v1alpha1.EnvoyIngressController{
		Context: &v1alpha1.ControllerContext{
			SendModule:     "cloudhub",
			ReceiveModule:  "envoyingresscontroller",
			ResponseModule: "cloudhub",
		},
	})
	manager, _, err := newTestController()
	if err != nil {
		t.Fatalf("error creating EnvoyIngress controller: %v", err)
	}
	ingress := newIngress("ingress-0", annotation, "service-0", ingressv1.ServiceBackendPort{Name: "http", Number: 9000})
	err = manager.ingressStore.Add(ingress)
	if err != nil {
		klog.Error(err)
	}
	service := newService("service-0", selector, "port-0", 9000)
	err = manager.serviceStore.Add(service)
	if err != nil {
		klog.Error(err)
	}
	pod := newPod("pod-0", "node-0", selector)
	err = manager.podStore.Add(pod)
	if err != nil {
		klog.Error(err)
	}
	endpoint := newEndpoint(pod, service)
	newService := newService("service-0", selector, "port-0", 9001)
	err = manager.serviceStore.Update(newService)
	if err != nil {
		klog.Error(err)
	}
	newEndpoint := newEndpoint(pod, newService)
	manager.updateEndpoint(endpoint, newEndpoint)
	if manager.queue.Len() == 0 {
		t.Fatalf("failed to enqueue controller for ingress %v", ingress.Name)
	}
	key, done := manager.queue.Get()
	if key == nil || done {
		t.Fatalf("failed to enqueue controller for ingress %v", ingress.Name)
	}
	expectedKey, _ := controller.KeyFunc(ingress)
	if got, want := key.(string), expectedKey; got != want {
		t.Errorf("queue.Get() = %v, want %v", got, want)
	}
}

func TestDeleteEndpoint(t *testing.T) {
	var (
		selector = map[string]string{
			"app": "envoyingress",
		}
		annotation = map[string]string{
			"nginx.ingress.kubernetes.io/rewrite-target": "/",
			"v1alpha1.kubeedge.io/nodegroup":             "guiyang",
			"kubernetes.io/ingress.class":                "envoyingress",
		}
	)
	config.InitConfigure(&v1alpha1.EnvoyIngressController{
		Context: &v1alpha1.ControllerContext{
			SendModule:     "cloudhub",
			ReceiveModule:  "envoyingresscontroller",
			ResponseModule: "cloudhub",
		},
	})
	manager, _, err := newTestController()
	if err != nil {
		t.Fatalf("error creating EnvoyIngress controller: %v", err)
	}
	ingress := newIngress("ingress-0", annotation, "service-0", ingressv1.ServiceBackendPort{Name: "http", Number: 9000})
	err = manager.ingressStore.Add(ingress)
	if err != nil {
		klog.Error(err)
	}
	service := newService("service-0", selector, "port-0", 9000)
	err = manager.serviceStore.Add(service)
	if err != nil {
		klog.Error(err)
	}
	pod := newPod("pod-0", "node-0", selector)
	err = manager.podStore.Add(pod)
	if err != nil {
		klog.Error(err)
	}
	endpoint := newEndpoint(pod, service)
	manager.deleteEndpoint(endpoint)
	if manager.queue.Len() == 0 {
		t.Fatalf("failed to enqueue controller for ingress %v", ingress.Name)
	}
	key, done := manager.queue.Get()
	if key == nil || done {
		t.Fatalf("failed to enqueue controller for ingress %v", ingress.Name)
	}
	expectedKey, _ := controller.KeyFunc(ingress)
	if got, want := key.(string), expectedKey; got != want {
		t.Errorf("queue.Get() = %v, want %v", got, want)
	}
}

func TestAddSecret(t *testing.T) {
	var (
		annotation = map[string]string{
			"nginx.ingress.kubernetes.io/rewrite-target": "/",
			"v1alpha1.kubeedge.io/nodegroup":             "guiyang",
			"kubernetes.io/ingress.class":                "envoyingress",
		}
	)
	config.InitConfigure(&v1alpha1.EnvoyIngressController{
		Context: &v1alpha1.ControllerContext{
			SendModule:     "cloudhub",
			ReceiveModule:  "envoyingresscontroller",
			ResponseModule: "cloudhub",
		},
	})
	manager, _, err := newTestController()
	if err != nil {
		t.Fatalf("error creating EnvoyIngress controller: %v", err)
	}
	ingress := newIngress("ingress-0", annotation, "service-0", ingressv1.ServiceBackendPort{Name: "http", Number: 9000})
	ingress.Spec.TLS = []ingressv1.IngressTLS{
		ingressv1.IngressTLS{
			Hosts:      []string{"foo.bar.com"},
			SecretName: "fooSecret",
		},
	}
	err = manager.ingressStore.Add(ingress)
	if err != nil {
		klog.Error(err)
	}
	secret := newSecret("fooSecret", "cert", "key")
	manager.addSecret(secret)
	if manager.queue.Len() == 0 {
		t.Fatalf("failed to enqueue controller for ingress %v", ingress.Name)
	}
	key, done := manager.queue.Get()
	if key == nil || done {
		t.Fatalf("failed to enqueue controller for ingress %v", ingress.Name)
	}
	expectedKey, _ := controller.KeyFunc(ingress)
	if got, want := key.(string), expectedKey; got != want {
		t.Errorf("queue.Get() = %v, want %v", got, want)
	}
}

func TestUpdateSecret(t *testing.T) {
	var (
		annotation = map[string]string{
			"nginx.ingress.kubernetes.io/rewrite-target": "/",
			"v1alpha1.kubeedge.io/nodegroup":             "guiyang",
			"kubernetes.io/ingress.class":                "envoyingress",
		}
	)
	config.InitConfigure(&v1alpha1.EnvoyIngressController{
		Context: &v1alpha1.ControllerContext{
			SendModule:     "cloudhub",
			ReceiveModule:  "envoyingresscontroller",
			ResponseModule: "cloudhub",
		},
	})
	manager, _, err := newTestController()
	if err != nil {
		t.Fatalf("error creating EnvoyIngress controller: %v", err)
	}
	ingress := newIngress("ingress-0", annotation, "service-0", ingressv1.ServiceBackendPort{Name: "http", Number: 9000})
	ingress.Spec.TLS = []ingressv1.IngressTLS{
		ingressv1.IngressTLS{
			Hosts:      []string{"foo.bar.com"},
			SecretName: "fooSecret",
		},
	}
	err = manager.ingressStore.Add(ingress)
	if err != nil {
		klog.Error(err)
	}
	secret := newSecret("fooSecret", "cert", "key")
	curSecret := newSecret("fooSecret", "cert1", "key")
	manager.updateSecret(secret, curSecret)
	if manager.queue.Len() == 0 {
		t.Fatalf("failed to enqueue controller for ingress %v", ingress.Name)
	}
	key, done := manager.queue.Get()
	if key == nil || done {
		t.Fatalf("failed to enqueue controller for ingress %v", ingress.Name)
	}
	expectedKey, _ := controller.KeyFunc(ingress)
	if got, want := key.(string), expectedKey; got != want {
		t.Errorf("queue.Get() = %v, want %v", got, want)
	}
}

func TestDeleteSecret(t *testing.T) {
	var (
		annotation = map[string]string{
			"nginx.ingress.kubernetes.io/rewrite-target": "/",
			"v1alpha1.kubeedge.io/nodegroup":             "guiyang",
			"kubernetes.io/ingress.class":                "envoyingress",
		}
	)
	config.InitConfigure(&v1alpha1.EnvoyIngressController{
		Context: &v1alpha1.ControllerContext{
			SendModule:     "cloudhub",
			ReceiveModule:  "envoyingresscontroller",
			ResponseModule: "cloudhub",
		},
	})
	manager, _, err := newTestController()
	if err != nil {
		t.Fatalf("error creating EnvoyIngress controller: %v", err)
	}
	ingress := newIngress("ingress-0", annotation, "service-0", ingressv1.ServiceBackendPort{Name: "http", Number: 9000})
	ingress.Spec.TLS = []ingressv1.IngressTLS{
		ingressv1.IngressTLS{
			Hosts:      []string{"foo.bar.com"},
			SecretName: "fooSecret",
		},
	}
	err = manager.ingressStore.Add(ingress)
	if err != nil {
		klog.Error(err)
	}
	secret := newSecret("fooSecret", "cert", "key")
	manager.deleteSecret(secret)
	if manager.queue.Len() == 0 {
		t.Fatalf("failed to enqueue controller for ingress %v", ingress.Name)
	}
	key, done := manager.queue.Get()
	if key == nil || done {
		t.Fatalf("failed to enqueue controller for ingress %v", ingress.Name)
	}
	expectedKey, _ := controller.KeyFunc(ingress)
	if got, want := key.(string), expectedKey; got != want {
		t.Errorf("queue.Get() = %v, want %v", got, want)
	}
}

func TestGetIngressesForService(t *testing.T) {
	var (
		selector = map[string]string{
			"app": "envoyingress",
		}
		annotation = map[string]string{
			"nginx.ingress.kubernetes.io/rewrite-target": "/",
			"v1alpha1.kubeedge.io/nodegroup":             "guiyang",
			"kubernetes.io/ingress.class":                "envoyingress",
		}
	)
	config.InitConfigure(&v1alpha1.EnvoyIngressController{
		Context: &v1alpha1.ControllerContext{
			SendModule:     "cloudhub",
			ReceiveModule:  "envoyingresscontroller",
			ResponseModule: "cloudhub",
		},
	})
	manager, _, err := newTestController()
	if err != nil {
		t.Fatalf("error creating EnvoyIngress controller: %v", err)
	}
	ingress := newIngress("ingress-0", annotation, "service-0", ingressv1.ServiceBackendPort{Name: "http", Number: 9000})
	err = manager.ingressStore.Add(ingress)
	if err != nil {
		klog.Error(err)
	}
	service := newService("service-0", selector, "port-0", 9000)
	ingresses := manager.getIngressesForService(service)
	if len(ingresses) == 0 {
		t.Fatalf("failed to get ingresses for service %s in namespace %s", service.Name, service.Namespace)
	}
	if ingresses[0] != ingress {
		t.Fatalf("Expected %v, got %v", ingress, ingresses)
	}
}

func TestGetServicesForIngress(t *testing.T) {
	var (
		selector = map[string]string{
			"app": "envoyingress",
		}
		annotation = map[string]string{
			"nginx.ingress.kubernetes.io/rewrite-target": "/",
			"v1alpha1.kubeedge.io/nodegroup":             "guiyang",
			"kubernetes.io/ingress.class":                "envoyingress",
		}
	)
	config.InitConfigure(&v1alpha1.EnvoyIngressController{
		Context: &v1alpha1.ControllerContext{
			SendModule:     "cloudhub",
			ReceiveModule:  "envoyingresscontroller",
			ResponseModule: "cloudhub",
		},
	})
	manager, _, err := newTestController()
	if err != nil {
		t.Fatalf("error creating EnvoyIngress controller: %v", err)
	}
	ingress := newIngress("ingress-0", annotation, "service-0", ingressv1.ServiceBackendPort{Name: "http", Number: 9000})
	err = manager.ingressStore.Add(ingress)
	if err != nil {
		klog.Error(err)
	}
	service := newService("service-0", selector, "port-0", 9000)
	err = manager.serviceStore.Add(service)
	if err != nil {
		klog.Error(err)
	}
	services, _ := manager.getServicesForIngress(ingress)
	if len(services) == 0 {
		t.Fatalf("failed to get ingresses for service %s in namespace %s", service.Name, service.Namespace)
	}
	if services[0] != service {
		t.Fatalf("Expected %v, got %v", service, services)
	}
}

func TestGetIngressesForSecret(t *testing.T) {
	var (
		annotation = map[string]string{
			"nginx.ingress.kubernetes.io/rewrite-target": "/",
			"v1alpha1.kubeedge.io/nodegroup":             "guiyang",
			"kubernetes.io/ingress.class":                "envoyingress",
		}
	)
	config.InitConfigure(&v1alpha1.EnvoyIngressController{
		Context: &v1alpha1.ControllerContext{
			SendModule:     "cloudhub",
			ReceiveModule:  "envoyingresscontroller",
			ResponseModule: "cloudhub",
		},
	})
	manager, _, err := newTestController()
	if err != nil {
		t.Fatalf("error creating EnvoyIngress controller: %v", err)
	}
	ingress := newIngress("ingress-0", annotation, "service-0", ingressv1.ServiceBackendPort{Name: "http", Number: 9000})
	ingress.Spec.TLS = []ingressv1.IngressTLS{
		ingressv1.IngressTLS{
			Hosts:      []string{"foo.bar.com"},
			SecretName: "fooSecret",
		},
	}
	err = manager.ingressStore.Add(ingress)
	if err != nil {
		klog.Error(err)
	}
	secret := newSecret("fooSecret", "cert", "key")
	ingresses := manager.getIngressesForSecret(secret)
	if len(ingresses) == 0 {
		t.Fatalf("failed to get ingresses for secret %s in namespace %s", secret.Name, secret.Namespace)
	}
	if ingresses[0] != ingress {
		t.Fatalf("expected %v, got %v", ingress, ingresses[0])
	}
}

func TestGetNodeGroupForIngress(t *testing.T) {
	var (
		annotation = map[string]string{
			"nginx.ingress.kubernetes.io/rewrite-target": "/",
			"v1alpha1.kubeedge.io/nodegroup":             "guiyang;shanxi;zhejiang",
			"kubernetes.io/ingress.class":                "envoyingress",
		}
	)

	ingress := newIngress("ingress-0", annotation, "service-0", ingressv1.ServiceBackendPort{Name: "http", Number: 9000})
	nodegroups, err := getNodeGroupForIngress(ingress)
	if err != nil {
		t.Fatalf(err.Error())
	}
	if len(nodegroups) == 0 {
		t.Fatalf("failed to get nodegroups for ingress %s in namespace %s", ingress.Name, ingress.Namespace)
	}
	if !reflect.DeepEqual(nodegroups, map[envoy_cache.NodeGroup]bool{envoy_cache.NodeGroup("guiyang"): true, envoy_cache.NodeGroup("shanxi"): true, envoy_cache.NodeGroup("zhejiang"): true}) {
		t.Fatalf("expected %v, got %v", []envoy_cache.NodeGroup{envoy_cache.NodeGroup("guiyang"), envoy_cache.NodeGroup("shanxi"), envoy_cache.NodeGroup("zhejiang")}, nodegroups)
	}
}

func TestGetSecretsForIngress(t *testing.T) {}

func TestGetEndpointsForIngress(t *testing.T) {}

func TestGetClustersForIngress(t *testing.T) {}

func TestGetRoutesForIngress(t *testing.T) {}

func TestGetListenersForIngress(t *testing.T) {}

type fakeCloudhub struct {
	enable bool
	ch     chan interface{}
}

func (fch *fakeCloudhub) Name() string {
	return modules.CloudHubModuleName
}

func (fch *fakeCloudhub) Group() string {
	return modules.CloudHubModuleGroup
}

func (fch *fakeCloudhub) Enable() bool {
	return fch.enable
}

func (fch *fakeCloudhub) Start() {
	for {
		select {
		case <-beehiveContext.Done():
			klog.Warning("Cloudhub channel eventqueue dispatch message loop stoped")
			return
		default:
		}
		msg, _ := beehiveContext.Receive(model.SrcCloudHub)
		klog.Infof("cloudhub has received a message, operation: %v resource: %v, content: %v",
			msg.GetOperation(), msg.GetResource(), msg.GetContent())
		fch.ch <- msg
	}
}

func TestDispatchResource(t *testing.T) {
	var (
		selector = map[string]string{
			"app": "envoyingress",
		}
		annotation = map[string]string{
			"nginx.ingress.kubernetes.io/rewrite-target": "/",
			"v1alpha1.kubeedge.io/nodegroup":             "guiyang",
			"kubernetes.io/ingress.class":                "envoyingress",
		}
	)
	fch := &fakeCloudhub{
		enable: true,
		ch:     make(chan interface{}, 4),
	}
	core.Register(fch)
	config.InitConfigure(&v1alpha1.EnvoyIngressController{
		Context: &v1alpha1.ControllerContext{
			SendModule:     "cloudhub",
			ReceiveModule:  "envoyingresscontroller",
			ResponseModule: "cloudhub",
		},
	})
	manager, _, err := newTestController()
	if err != nil {
		t.Fatalf("error creating EnvoyIngress controller: %v", err)
	}
	core.Register(manager)
	core.StartModules()
	count := 0
	var counter *time.Timer
	node := newNode("node-0", map[string]string{"nodegroup": "guiyang"})
	err = manager.nodeStore.Add(node)
	if err != nil {
		t.Error(err)
	}
	manager.addNode(node)
	ingress := newIngress("ingress-0", annotation, "service-0", ingressv1.ServiceBackendPort{Name: "http", Number: 9000})
	err = manager.ingressStore.Add(ingress)
	if err != nil {
		t.Error(err)
	}
	service := newService("service-0", selector, "port-0", 9000)
	err = manager.serviceStore.Add(service)
	if err != nil {
		t.Error(err)
	}
	pod := newPod("pod-0", "node-0", selector)
	err = manager.podStore.Add(pod)
	endpoint := newEndpoint(pod, service)
	err = manager.endpointStore.Add(endpoint)
	if err != nil {
		t.Error(err)
	}
	manager.addIngress(ingress)
	counter = time.NewTimer(time.Duration(10000000000))
	ch := make(chan int, 1)
	go func() {
		for {
			select {
			case <-beehiveContext.Done():
				return
			case <-fch.ch:
				count++
				if count == 4 {
					ch <- 1
				}
			case <-counter.C:
				ch <- 1
				//t.Fatalf("takes too long to dispatch messages")
			}
		}
	}()
	<-ch
}

func TestConsumer(t *testing.T) {}

func TestSyncEnvoyIngress(t *testing.T) {}

func TestInitEnvoyEnvoyResources(t *testing.T) {
	var (
		podLabel = map[string]string{
			"app": "envoyingresstest",
		}
		annotation = map[string]string{
			"nginx.ingress.kubernetes.io/rewrite-target": "/",
			"v1alpha1.kubeedge.io/nodegroup":             "guiyang",
			"kubernetes.io/ingress.class":                "envoyingress",
		}
		nodeLabel = map[string]string{
			"nodegroup": "guiyang",
		}
	)
	config.InitConfigure(&v1alpha1.EnvoyIngressController{
		Context: &v1alpha1.ControllerContext{
			SendModule:     "cloudhub",
			ReceiveModule:  "envoyingresscontroller",
			ResponseModule: "cloudhub",
		},
	})
	manager, _, err := newTestController()
	if err != nil {
		t.Fatalf("error creating EnvoyIngress controller: %v", err)
	}
	addNodes(manager.nodeStore, 0, 1, nodeLabel)
	nodeList := manager.nodeStore.List()
	for _, _node := range nodeList {
		node := _node.(*v1.Node)
		addPods(manager.podStore, node.ObjectMeta.Name, podLabel, 3)
	}
	addServices(manager.serviceStore, 1, podLabel)
	addEndpoints(manager.endpointStore, manager.serviceStore, manager.podStore)
	for _, _service := range manager.serviceStore.List() {
		service := _service.(*v1.Service)
		err = manager.ingressStore.Add(newIngress("ingressTest", annotation, service.Name,
			ingressv1.ServiceBackendPort{Name: service.Spec.Ports[0].Name, Number: service.Spec.Ports[0].Port}))
		if err != nil {
			t.Error(err)
		}
	}
	addSecrets(manager.secretStore, 1)
	err = manager.initCache()
	if err != nil {
		t.Fatalf("error initing nodegroup relationship: %v", err)
	}
	err = manager.initiateEnvoyResources()
	if err != nil {
		t.Fatalf("error initing envoy resources: %v", err)
	}
	if len(manager.EnvoyIngressController.resourceStore) != 4 {
		t.Errorf("fail to convert k8s endpoint into envoy endpoint, len: got %d, expectd: %d", len(manager.EnvoyIngressController.resourceStore),
			4)
	}
}

// Run test cases
var _ = Describe("Envoy ingress converting to envoy resources in E2E test using envoyingresscontroller", func() {
	var (
		podLabel = map[string]string{
			"app": "envoyingresstest",
		}
		annotation = map[string]string{
			"nginx.ingress.kubernetes.io/rewrite-target": "/",
			"v1alpha1.kubeedge.io/nodegroup":             "guiyang",
			"kubernetes.io/ingress.class":                "envoyingress",
		}
		nodeLabel = map[string]string{
			"nodegroup": "guiyang",
		}
		manager       *envoyIngressController
		err           error
		resourceCount int
		fch           *fakeCloudhub
		waitTime      time.Duration
	)

	Context("Test ingress deployment with tls", func() {
		BeforeEach(func() {
			fch = &fakeCloudhub{
				enable: true,
				ch:     make(chan interface{}, 4),
			}
			core.Register(fch)
			config.InitConfigure(&v1alpha1.EnvoyIngressController{
				Context: &v1alpha1.ControllerContext{
					SendModule:     "cloudhub",
					ReceiveModule:  "envoyingresscontroller",
					ResponseModule: "cloudhub",
				},
			})
			manager, _, err = newTestController()
			Expect(err).To(BeNil())
			core.Register(manager)
			core.StartModules()
			waitTime = 10 * time.Millisecond
		})

		AfterEach(func() {
			Expect(len(manager.resourceStore)).Should(Equal(resourceCount))
			for _, resource := range manager.resourceStore {
				klog.Infof("resourceName: %s", resource.Name)
			}
			utils.PrintTestcaseNameandStatus()
		})

		It("E2E_EIC_TLS_DEPLOYMENT_1: Create ingress, ready node, corresponding service,endpoint,secret and checks all"+
			" the generated envoy resources are correct", func() {
			resourceCount = 6
			// add Node
			node := newNode("node-0", nodeLabel)
			err = manager.nodeStore.Add(node)
			Expect(err).To(BeNil())
			manager.addNode(node)

			// add ingress with tls
			ingress := newIngress("ingress-0", annotation, "service-0", ingressv1.ServiceBackendPort{Name: "http", Number: 9000})
			ingress.Spec.TLS = []ingressv1.IngressTLS{
				{
					Hosts:      []string{"bar.foo.com"},
					SecretName: "secret-0",
				},
			}
			err = manager.ingressStore.Add(ingress)
			Expect(err).To(BeNil())
			manager.addIngress(ingress)

			// add service
			service := newService("service-0", podLabel, "port-0", 9000)
			err = manager.serviceStore.Add(service)
			Expect(err).To(BeNil())
			manager.addService(service)

			// add endpoint
			pod := newPod("pod-0", "node-0", podLabel)
			err = manager.podStore.Add(pod)
			Expect(err).To(BeNil())
			endpoint := newEndpoint(pod, service)
			err = manager.endpointStore.Add(endpoint)
			Expect(err).To(BeNil())
			manager.addEndpoint(endpoint)

			//add secret
			secret := newSecret("secret-0", "cert", "key")
			err = manager.secretStore.Add(secret)
			Expect(err).To(BeNil())
			manager.addSecret(secret)
			counter := time.NewTimer(waitTime)
			count := 0
			ch := make(chan int, 1)
			go func() {
				for {
					select {
					case <-beehiveContext.Done():
						return
					case <-fch.ch:
						count++
						if count == resourceCount {
							ch <- 1
						}
					case <-counter.C:
						ch <- 1
						return
					}
				}
			}()
			<-ch
		})

		It("E2E_EIC_TLS_DEPLOYMENT_2: Create ingress, not ready node, corresponding service, endpoint, secret and "+
			"checks all the generated envoy resources are correct", func() {
			// In this case, the resources is generated, but node dispatched to the not ready node
			resourceCount = 6
			// add Node
			node := newNode("node-0", nodeLabel)
			node.Status = v1.NodeStatus{
				Conditions: []v1.NodeCondition{
					{Type: v1.NodeReady, Status: v1.ConditionFalse},
				},
			}
			err = manager.nodeStore.Add(node)
			Expect(err).To(BeNil())
			manager.addNode(node)

			// add ingress with tls
			ingress := newIngress("ingress-0", annotation, "service-0", ingressv1.ServiceBackendPort{Name: "http", Number: 9000})
			ingress.Spec.TLS = []ingressv1.IngressTLS{
				{
					Hosts:      []string{"bar.foo.com"},
					SecretName: "secret-0",
				},
			}
			err = manager.ingressStore.Add(ingress)
			Expect(err).To(BeNil())
			manager.addIngress(ingress)

			// add service
			service := newService("service-0", podLabel, "port-0", 9000)
			err = manager.serviceStore.Add(service)
			Expect(err).To(BeNil())
			manager.addService(service)

			// add endpoint
			pod := newPod("pod-0", "node-0", podLabel)
			err = manager.podStore.Add(pod)
			Expect(err).To(BeNil())
			endpoint := newEndpoint(pod, service)
			err = manager.endpointStore.Add(endpoint)
			Expect(err).To(BeNil())
			manager.addEndpoint(endpoint)

			//add secret
			secret := newSecret("secret-0", "cert", "key")
			err = manager.secretStore.Add(secret)
			Expect(err).To(BeNil())
			manager.addSecret(secret)
			counter := time.NewTimer(waitTime)
			count := 0
			ch := make(chan int, 1)
			go func() {
				for {
					select {
					case <-beehiveContext.Done():
						return
					case <-fch.ch:
						count++
						if count == resourceCount {
							ch <- 1
						}
					case <-counter.C:
						ch <- 1
						return
					}
				}
			}()
			<-ch
			Expect(count).Should(Equal(0))
		})

		It("E2E_EIC_TLS_DEPLOYMENT_3: Create ingress, ready node, not corresponding service,  corresponding endpoint,"+
			" secret and checks all the generated envoy resources are correct", func() {
			resourceCount = 3
			// add Node
			node := newNode("node-0", nodeLabel)
			err = manager.nodeStore.Add(node)
			Expect(err).To(BeNil())
			manager.addNode(node)

			// add ingress with tls
			ingress := newIngress("ingress-0", annotation, "service-0", ingressv1.ServiceBackendPort{Name: "http", Number: 9000})
			ingress.Spec.TLS = []ingressv1.IngressTLS{
				{
					Hosts:      []string{"bar.foo.com"},
					SecretName: "secret-0",
				},
			}
			err = manager.ingressStore.Add(ingress)
			Expect(err).To(BeNil())
			manager.addIngress(ingress)

			// add service
			service := newService("service-1", podLabel, "port-0", 9000)
			err = manager.serviceStore.Add(service)
			Expect(err).To(BeNil())
			manager.addService(service)

			// add endpoint
			pod := newPod("pod-0", "node-0", podLabel)
			err = manager.podStore.Add(pod)
			Expect(err).To(BeNil())
			endpoint := newEndpoint(pod, service)
			err = manager.endpointStore.Add(endpoint)
			Expect(err).To(BeNil())
			manager.addEndpoint(endpoint)

			//add secret
			secret := newSecret("secret-0", "cert", "key")
			err = manager.secretStore.Add(secret)
			Expect(err).To(BeNil())
			manager.addSecret(secret)
			counter := time.NewTimer(waitTime)
			count := 0
			ch := make(chan int, 1)
			go func() {
				for {
					select {
					case <-beehiveContext.Done():
						return
					case <-fch.ch:
						count++
						if count == resourceCount {
							ch <- 1
						}
					case <-counter.C:
						ch <- 1
						return
					}
				}
			}()
			<-ch
		})

		It("E2E_EIC_TLS_DEPLOYMENT_4: Create ingress, ready node, corresponding service, not corresponding endpoint"+
			"corresponding secret and checks all the generated envoy resources are correct", func() {
			resourceCount = 5
			// add Node
			node := newNode("node-0", nodeLabel)
			err = manager.nodeStore.Add(node)
			Expect(err).To(BeNil())
			manager.addNode(node)

			// add ingress with tls
			ingress := newIngress("ingress-0", annotation, "service-0", ingressv1.ServiceBackendPort{Name: "http", Number: 9000})
			ingress.Spec.TLS = []ingressv1.IngressTLS{
				{
					Hosts:      []string{"bar.foo.com"},
					SecretName: "secret-0",
				},
			}
			err = manager.ingressStore.Add(ingress)
			Expect(err).To(BeNil())
			manager.addIngress(ingress)

			// add service
			service := newService("service-0", podLabel, "port-0", 9000)
			err = manager.serviceStore.Add(service)
			Expect(err).To(BeNil())
			manager.addService(service)

			// add endpoint
			pod := newPod("pod-0", "node-0", podLabel)
			err = manager.podStore.Add(pod)
			Expect(err).To(BeNil())
			endpoint := newEndpoint(pod, service)
			endpoint.Name = "service-1"
			err = manager.endpointStore.Add(endpoint)
			Expect(err).To(BeNil())
			manager.addEndpoint(endpoint)

			//add secret
			secret := newSecret("secret-0", "cert", "key")
			err = manager.secretStore.Add(secret)
			Expect(err).To(BeNil())
			manager.addSecret(secret)
			counter := time.NewTimer(waitTime)
			count := 0
			ch := make(chan int, 1)
			go func() {
				for {
					select {
					case <-beehiveContext.Done():
						return
					case <-fch.ch:
						count++
						if count == resourceCount {
							ch <- 1
						}
					case <-counter.C:
						ch <- 1
						return
					}
				}
			}()
			<-ch
		})

		It("E2E_EIC_TLS_DEPLOYMENT_5: Create ingress, ready node, corresponding service, corresponding endpoint"+
			"not corresponding secret and checks all the generated envoy resources are correct", func() {
			resourceCount = 4
			// add Node
			node := newNode("node-0", nodeLabel)
			err = manager.nodeStore.Add(node)
			Expect(err).To(BeNil())
			manager.addNode(node)

			// add ingress with tls
			ingress := newIngress("ingress-0", annotation, "service-0", ingressv1.ServiceBackendPort{Name: "http", Number: 9000})
			ingress.Spec.TLS = []ingressv1.IngressTLS{
				{
					Hosts:      []string{"bar.foo.com"},
					SecretName: "secret-0",
				},
			}
			err = manager.ingressStore.Add(ingress)
			Expect(err).To(BeNil())
			manager.addIngress(ingress)

			// add service
			service := newService("service-0", podLabel, "port-0", 9000)
			service.Annotations = make(map[string]string)
			service.Annotations["v1alpha1.kubeedge.io/httpprotocol"] = "tls"
			err = manager.serviceStore.Add(service)
			Expect(err).To(BeNil())
			manager.addService(service)

			// add endpoint
			pod := newPod("pod-0", "node-0", podLabel)
			err = manager.podStore.Add(pod)
			Expect(err).To(BeNil())
			endpoint := newEndpoint(pod, service)
			err = manager.endpointStore.Add(endpoint)
			Expect(err).To(BeNil())
			manager.addEndpoint(endpoint)

			//add secret
			secret := newSecret("secret-1", "cert", "key")
			err = manager.secretStore.Add(secret)
			Expect(err).To(BeNil())
			manager.addSecret(secret)
			counter := time.NewTimer(waitTime)
			count := 0
			ch := make(chan int, 1)
			go func() {
				for {
					select {
					case <-beehiveContext.Done():
						return
					case <-fch.ch:
						count++
						if count == resourceCount {
							ch <- 1
						}
					case <-counter.C:
						ch <- 1
						return
					}
				}
			}()
			<-ch
		})

		// add test for ingress deletion
		It("E2E_EIC_RESOURCE_DELETION_1: Create ingress, service, endpoint, secret and delete related service", func() {
			resourceCount = 3
			// add Node
			node := newNode("node-0", nodeLabel)
			err = manager.nodeStore.Add(node)
			Expect(err).To(BeNil())
			manager.addNode(node)

			// add ingress with tls
			ingress := newIngress("ingress-0", annotation, "service-0", ingressv1.ServiceBackendPort{Name: "http", Number: 9000})
			ingress.Spec.TLS = []ingressv1.IngressTLS{
				{
					Hosts:      []string{"bar.foo.com"},
					SecretName: "secret-0",
				},
			}
			err = manager.ingressStore.Add(ingress)
			Expect(err).To(BeNil())
			manager.addIngress(ingress)

			// add service
			service := newService("service-0", podLabel, "port-0", 9000)
			err = manager.serviceStore.Add(service)
			Expect(err).To(BeNil())
			manager.addService(service)

			// add endpoint
			pod := newPod("pod-0", "node-0", podLabel)
			err = manager.podStore.Add(pod)
			Expect(err).To(BeNil())
			endpoint := newEndpoint(pod, service)
			err = manager.endpointStore.Add(endpoint)
			Expect(err).To(BeNil())
			manager.addEndpoint(endpoint)

			//add secret
			secret := newSecret("secret-0", "cert", "key")
			err = manager.secretStore.Add(secret)
			Expect(err).To(BeNil())
			manager.addSecret(secret)
			counter := time.NewTimer(waitTime)
			count := 0
			ch := make(chan int, 1)
			go func() {
				for {
					select {
					case <-beehiveContext.Done():
						return
					case <-fch.ch:
						count++
						if count == resourceCount {
							ch <- 1
						}
					case <-counter.C:
						ch <- 1
						return
					}
				}
			}()
			<-ch
			err = manager.serviceStore.Delete(service)
			Expect(err).To(BeNil())
			manager.deleteService(service)
			counter.Reset(waitTime)
			go func() {
				for {
					select {
					case <-beehiveContext.Done():
						return
					case <-fch.ch:
						count++
						if count == resourceCount {
							ch <- 1
						}
					case <-counter.C:
						ch <- 1
						return
					}
				}
			}()
			<-ch
		})

		It("E2E_EIC_RESOURCE_DELETION_2: Create ingress, service, endpoint, secret and delete unrelated service", func() {
			resourceCount = 3
			// add Node
			node := newNode("node-0", nodeLabel)
			err = manager.nodeStore.Add(node)
			Expect(err).To(BeNil())
			manager.addNode(node)

			// add ingress with tls
			ingress := newIngress("ingress-0", annotation, "service-0", ingressv1.ServiceBackendPort{Name: "http", Number: 9000})
			ingress.Spec.TLS = []ingressv1.IngressTLS{
				{
					Hosts:      []string{"bar.foo.com"},
					SecretName: "secret-0",
				},
			}
			err = manager.ingressStore.Add(ingress)
			Expect(err).To(BeNil())
			manager.addIngress(ingress)

			// add service
			service := newService("service-1", podLabel, "port-0", 9000)
			err = manager.serviceStore.Add(service)
			Expect(err).To(BeNil())
			manager.addService(service)

			// add endpoint
			pod := newPod("pod-0", "node-0", podLabel)
			err = manager.podStore.Add(pod)
			Expect(err).To(BeNil())
			endpoint := newEndpoint(pod, service)
			err = manager.endpointStore.Add(endpoint)
			Expect(err).To(BeNil())
			manager.addEndpoint(endpoint)

			//add secret
			secret := newSecret("secret-0", "cert", "key")
			err = manager.secretStore.Add(secret)
			Expect(err).To(BeNil())
			manager.addSecret(secret)
			counter := time.NewTimer(waitTime)
			count := 0
			ch := make(chan int, 1)
			go func() {
				for {
					select {
					case <-beehiveContext.Done():
						return
					case <-fch.ch:
						count++
						if count == resourceCount {
							ch <- 1
						}
					case <-counter.C:
						ch <- 1
						return
					}
				}
			}()
			<-ch
			err = manager.serviceStore.Delete(service)
			Expect(err).To(BeNil())
			manager.deleteService(service)
			counter.Reset(waitTime)
			go func() {
				for {
					select {
					case <-beehiveContext.Done():
						return
					case <-fch.ch:
						count++
						if count == resourceCount {
							ch <- 1
						}
					case <-counter.C:
						ch <- 1
						return
					}
				}
			}()
			<-ch
		})

		It("E2E_EIC_RESOURCE_DELETION_3: Create ingress, service, endpoint, secret and delete related endpoint", func() {
			resourceCount = 5
			// add Node
			node := newNode("node-0", nodeLabel)
			err = manager.nodeStore.Add(node)
			Expect(err).To(BeNil())
			manager.addNode(node)

			// add ingress with tls
			ingress := newIngress("ingress-0", annotation, "service-0", ingressv1.ServiceBackendPort{Name: "http", Number: 9000})
			ingress.Spec.TLS = []ingressv1.IngressTLS{
				{
					Hosts:      []string{"bar.foo.com"},
					SecretName: "secret-0",
				},
			}
			err = manager.ingressStore.Add(ingress)
			Expect(err).To(BeNil())
			manager.addIngress(ingress)

			// add service
			service := newService("service-0", podLabel, "port-0", 9000)
			err = manager.serviceStore.Add(service)
			Expect(err).To(BeNil())
			manager.addService(service)

			// add endpoint
			pod := newPod("pod-0", "node-0", podLabel)
			err = manager.podStore.Add(pod)
			Expect(err).To(BeNil())
			endpoint := newEndpoint(pod, service)
			err = manager.endpointStore.Add(endpoint)
			Expect(err).To(BeNil())
			manager.addEndpoint(endpoint)

			//add secret
			secret := newSecret("secret-0", "cert", "key")
			err = manager.secretStore.Add(secret)
			Expect(err).To(BeNil())
			manager.addSecret(secret)
			counter := time.NewTimer(waitTime)
			count := 0
			ch := make(chan int, 1)
			go func() {
				for {
					select {
					case <-beehiveContext.Done():
						return
					case <-fch.ch:
						count++
						if count == resourceCount {
							ch <- 1
						}
					case <-counter.C:
						ch <- 1
						return
					}
				}
			}()
			<-ch
			err = manager.endpointStore.Delete(endpoint)
			Expect(err).To(BeNil())
			manager.deleteEndpoint(endpoint)
			counter.Reset(waitTime)
			go func() {
				for {
					select {
					case <-beehiveContext.Done():
						return
					case <-fch.ch:
						count++
						if count == resourceCount {
							ch <- 1
						}
					case <-counter.C:
						ch <- 1
						return
					}
				}
			}()
			<-ch
		})

		It("E2E_EIC_RESOURCE_DELETION_4: Create ingress, service, endpoint, secret and delete unrelated endpoint", func() {
			resourceCount = 5
			// add Node
			node := newNode("node-0", nodeLabel)
			err = manager.nodeStore.Add(node)
			Expect(err).To(BeNil())
			manager.addNode(node)

			// add ingress with tls
			ingress := newIngress("ingress-0", annotation, "service-0", ingressv1.ServiceBackendPort{Name: "http", Number: 9000})
			ingress.Spec.TLS = []ingressv1.IngressTLS{
				{
					Hosts:      []string{"bar.foo.com"},
					SecretName: "secret-0",
				},
			}
			err = manager.ingressStore.Add(ingress)
			Expect(err).To(BeNil())
			manager.addIngress(ingress)

			// add service
			service := newService("service-0", podLabel, "port-0", 9000)
			err = manager.serviceStore.Add(service)
			Expect(err).To(BeNil())
			manager.addService(service)

			// add endpoint
			pod := newPod("pod-0", "node-0", podLabel)
			err = manager.podStore.Add(pod)
			Expect(err).To(BeNil())
			endpoint := newEndpoint(pod, service)
			endpoint.Name = "service-1"
			err = manager.endpointStore.Add(endpoint)
			Expect(err).To(BeNil())
			manager.addEndpoint(endpoint)

			//add secret
			secret := newSecret("secret-0", "cert", "key")
			err = manager.secretStore.Add(secret)
			Expect(err).To(BeNil())
			manager.addSecret(secret)
			counter := time.NewTimer(waitTime)
			count := 0
			ch := make(chan int, 1)
			go func() {
				for {
					select {
					case <-beehiveContext.Done():
						return
					case <-fch.ch:
						count++
						if count == resourceCount {
							ch <- 1
						}
					case <-counter.C:
						ch <- 1
						return
					}
				}
			}()
			<-ch
			err = manager.endpointStore.Delete(endpoint)
			Expect(err).To(BeNil())
			manager.deleteEndpoint(endpoint)
			counter.Reset(waitTime)
			go func() {
				for {
					select {
					case <-beehiveContext.Done():
						return
					case <-fch.ch:
						count++
						if count == resourceCount {
							ch <- 1
						}
					case <-counter.C:
						ch <- 1
						return
					}
				}
			}()
			<-ch
		})

		It("E2E_EIC_RESOURCE_DELETION_5: Create ingress, service, endpoint, secret and delete related secret", func() {
			resourceCount = 4
			// add Node
			node := newNode("node-0", nodeLabel)
			err = manager.nodeStore.Add(node)
			Expect(err).To(BeNil())
			manager.addNode(node)

			// add ingress with tls
			ingress := newIngress("ingress-0", annotation, "service-0", ingressv1.ServiceBackendPort{Name: "http", Number: 9000})
			ingress.Spec.TLS = []ingressv1.IngressTLS{
				{
					Hosts:      []string{"bar.foo.com"},
					SecretName: "secret-0",
				},
			}
			err = manager.ingressStore.Add(ingress)
			Expect(err).To(BeNil())
			manager.addIngress(ingress)

			// add service
			service := newService("service-0", podLabel, "port-0", 9000)
			err = manager.serviceStore.Add(service)
			Expect(err).To(BeNil())
			manager.addService(service)

			// add endpoint
			pod := newPod("pod-0", "node-0", podLabel)
			err = manager.podStore.Add(pod)
			Expect(err).To(BeNil())
			endpoint := newEndpoint(pod, service)
			err = manager.endpointStore.Add(endpoint)
			Expect(err).To(BeNil())
			manager.addEndpoint(endpoint)

			//add secret
			secret := newSecret("secret-0", "cert", "key")
			err = manager.secretStore.Add(secret)
			Expect(err).To(BeNil())
			manager.addSecret(secret)
			counter := time.NewTimer(waitTime)
			count := 0
			ch := make(chan int, 1)
			go func() {
				for {
					select {
					case <-beehiveContext.Done():
						return
					case <-fch.ch:
						count++
						if count == resourceCount {
							ch <- 1
						}
					case <-counter.C:
						ch <- 1
						return
					}
				}
			}()
			<-ch
			err = manager.secretStore.Delete(secret)
			Expect(err).To(BeNil())
			manager.deleteSecret(secret)
			counter.Reset(waitTime)
			go func() {
				for {
					select {
					case <-beehiveContext.Done():
						return
					case <-fch.ch:
						count++
						if count == resourceCount {
							ch <- 1
						}
					case <-counter.C:
						ch <- 1
						return
					}
				}
			}()
			<-ch
		})

		It("E2E_EIC_RESOURCE_DELETION_6: Create ingress, service, endpoint, secret and delete unrelated secret", func() {
			resourceCount = 4
			// add Node
			node := newNode("node-0", nodeLabel)
			err = manager.nodeStore.Add(node)
			Expect(err).To(BeNil())
			manager.addNode(node)

			// add ingress with tls
			ingress := newIngress("ingress-0", annotation, "service-0", ingressv1.ServiceBackendPort{Name: "http", Number: 9000})
			ingress.Spec.TLS = []ingressv1.IngressTLS{
				{
					Hosts:      []string{"bar.foo.com"},
					SecretName: "secret-0",
				},
			}
			err = manager.ingressStore.Add(ingress)
			Expect(err).To(BeNil())
			manager.addIngress(ingress)

			// add service
			service := newService("service-0", podLabel, "port-0", 9000)
			err = manager.serviceStore.Add(service)
			Expect(err).To(BeNil())
			manager.addService(service)

			// add endpoint
			pod := newPod("pod-0", "node-0", podLabel)
			err = manager.podStore.Add(pod)
			Expect(err).To(BeNil())
			endpoint := newEndpoint(pod, service)
			err = manager.endpointStore.Add(endpoint)
			Expect(err).To(BeNil())
			manager.addEndpoint(endpoint)

			//add secret
			secret := newSecret("secret-1", "cert", "key")
			err = manager.secretStore.Add(secret)
			Expect(err).To(BeNil())
			manager.addSecret(secret)
			counter := time.NewTimer(waitTime)
			count := 0
			ch := make(chan int, 1)
			go func() {
				for {
					select {
					case <-beehiveContext.Done():
						return
					case <-fch.ch:
						count++
						if count == resourceCount {
							ch <- 1
						}
					case <-counter.C:
						ch <- 1
						return
					}
				}
			}()
			<-ch
			err = manager.secretStore.Delete(secret)
			Expect(err).To(BeNil())
			manager.deleteSecret(secret)
			counter.Reset(waitTime)
			go func() {
				for {
					select {
					case <-beehiveContext.Done():
						return
					case <-fch.ch:
						count++
						if count == resourceCount {
							ch <- 1
						}
					case <-counter.C:
						ch <- 1
						return
					}
				}
			}()
			<-ch
		})

		It("E2E_EIC_RESOURCE_INSERTION_0: Create ingress, service, endpoint, secret, and insert related service", func() {
			resourceCount = 6
			// add Node
			node := newNode("node-0", nodeLabel)
			err = manager.nodeStore.Add(node)
			Expect(err).To(BeNil())
			manager.addNode(node)
			var g sync.WaitGroup
			var addResource = []func(){
				func() {
					// add ingress with tls
					ingress := newIngress("ingress-0", annotation, "service-0", ingressv1.ServiceBackendPort{Name: "http", Number: 9000})
					ingress.Spec.TLS = []ingressv1.IngressTLS{
						{
							Hosts:      []string{"bar.foo.com"},
							SecretName: "secret-0",
						},
					}
					err = manager.ingressStore.Add(ingress)
					Expect(err).To(BeNil())
					manager.addIngress(ingress)
					klog.Info("succeeded in add ingress")
					g.Done()
				},
				func() {
					// add endpoint
					pod := newPod("pod-0", "node-0", podLabel)
					err = manager.podStore.Add(pod)
					Expect(err).To(BeNil())
					// add service
					service := newService("service-0", podLabel, "port-0", 9000)
					err = manager.serviceStore.Add(service)
					Expect(err).To(BeNil())
					manager.addService(service)
					endpoint := newEndpoint(pod, service)
					err = manager.endpointStore.Add(endpoint)
					Expect(err).To(BeNil())
					manager.addEndpoint(endpoint)
					klog.Info("succeeded in add service and endpoint")
					g.Done()
				},
				func() {
					//add secret
					secret := newSecret("secret-0", "cert", "key")
					err = manager.secretStore.Add(secret)
					Expect(err).To(BeNil())
					manager.addSecret(secret)
					klog.Info("succeeded in secret")
					g.Done()
				},
			}
			for _, function := range addResource {
				g.Add(1)
				go function()
			}
			g.Wait()
			counter := time.NewTimer(waitTime)
			count := 0
			ch := make(chan int, 1)
			go func() {
				for {
					select {
					case <-beehiveContext.Done():
						return
					case <-fch.ch:
						count++
						if count == resourceCount {
							ch <- 1
						}
					case <-counter.C:
						ch <- 1
						return
					}
				}
			}()
			<-ch
		})

		It("E2E_EIC_RESOURCE_INSERTION_1: Create ingress, service, endpoint, secret, and insert related service", func() {
			resourceCount = 6
			// add Node
			node := newNode("node-0", nodeLabel)
			err = manager.nodeStore.Add(node)
			Expect(err).To(BeNil())
			manager.addNode(node)

			// add ingress with tls
			ingress := newIngress("ingress-0", annotation, "service-0", ingressv1.ServiceBackendPort{Name: "http", Number: 9000})
			ingress.Spec.TLS = []ingressv1.IngressTLS{
				{
					Hosts:      []string{"bar.foo.com"},
					SecretName: "secret-0",
				},
			}
			err = manager.ingressStore.Add(ingress)
			Expect(err).To(BeNil())
			manager.addIngress(ingress)

			// add endpoint
			pod := newPod("pod-0", "node-0", podLabel)
			err = manager.podStore.Add(pod)
			Expect(err).To(BeNil())

			//add secret
			secret := newSecret("secret-0", "cert", "key")
			err = manager.secretStore.Add(secret)
			Expect(err).To(BeNil())
			manager.addSecret(secret)
			counter := time.NewTimer(waitTime)
			count := 0
			ch := make(chan int, 1)
			go func() {
				for {
					select {
					case <-beehiveContext.Done():
						return
					case <-fch.ch:
						count++
						if count == resourceCount {
							ch <- 1
						}
					case <-counter.C:
						ch <- 1
						return
					}
				}
			}()
			<-ch
			// add service
			service := newService("service-0", podLabel, "port-0", 9000)
			err = manager.serviceStore.Add(service)
			Expect(err).To(BeNil())
			manager.addService(service)
			endpoint := newEndpoint(pod, service)
			err = manager.endpointStore.Add(endpoint)
			Expect(err).To(BeNil())
			manager.addEndpoint(endpoint)
			counter.Reset(waitTime)
			go func() {
				for {
					select {
					case <-beehiveContext.Done():
						return
					case <-fch.ch:
						count++
						if count == resourceCount {
							ch <- 1
						}
					case <-counter.C:
						ch <- 1
						return
					}
				}
			}()
			<-ch
		})

		It("E2E_EIC_RESOURCE_INSERTION_2: Create ingress, service, endpoint, secret, and insert related endpoint", func() {
			resourceCount = 6
			// add Node
			node := newNode("node-0", nodeLabel)
			err = manager.nodeStore.Add(node)
			Expect(err).To(BeNil())
			manager.addNode(node)

			// add ingress with tls
			ingress := newIngress("ingress-0", annotation, "service-0", ingressv1.ServiceBackendPort{Name: "http", Number: 9000})
			ingress.Spec.TLS = []ingressv1.IngressTLS{
				{
					Hosts:      []string{"bar.foo.com"},
					SecretName: "secret-0",
				},
			}
			err = manager.ingressStore.Add(ingress)
			Expect(err).To(BeNil())
			manager.addIngress(ingress)

			// add service
			service := newService("service-0", podLabel, "port-0", 9000)
			err = manager.serviceStore.Add(service)
			Expect(err).To(BeNil())
			manager.addService(service)

			//add secret
			secret := newSecret("secret-0", "cert", "key")
			err = manager.secretStore.Add(secret)
			Expect(err).To(BeNil())
			manager.addSecret(secret)
			counter := time.NewTimer(waitTime)
			count := 0
			ch := make(chan int, 1)
			go func() {
				for {
					select {
					case <-beehiveContext.Done():
						return
					case <-fch.ch:
						count++
						if count == resourceCount {
							ch <- 1
						}
					case <-counter.C:
						ch <- 1
						return
					}
				}
			}()
			<-ch
			// add endpoint
			pod := newPod("pod-0", "node-0", podLabel)
			err = manager.podStore.Add(pod)
			Expect(err).To(BeNil())
			endpoint := newEndpoint(pod, service)
			err = manager.endpointStore.Add(endpoint)
			Expect(err).To(BeNil())
			manager.addEndpoint(endpoint)
			counter.Reset(waitTime)
			go func() {
				for {
					select {
					case <-beehiveContext.Done():
						return
					case <-fch.ch:
						count++
						if count == resourceCount {
							ch <- 1
						}
					case <-counter.C:
						ch <- 1
						return
					}
				}
			}()
			<-ch
		})

		It("E2E_EIC_RESOURCE_INSERTION_3: Create ingress, service, endpoint, secret, and insert related secret", func() {
			resourceCount = 6
			// add Node
			node := newNode("node-0", nodeLabel)
			err = manager.nodeStore.Add(node)
			Expect(err).To(BeNil())
			manager.addNode(node)

			// add ingress with tls
			ingress := newIngress("ingress-0", annotation, "service-0", ingressv1.ServiceBackendPort{Name: "http", Number: 9000})
			ingress.Spec.TLS = []ingressv1.IngressTLS{
				{
					Hosts:      []string{"bar.foo.com"},
					SecretName: "secret-0",
				},
			}
			err = manager.ingressStore.Add(ingress)
			Expect(err).To(BeNil())
			manager.addIngress(ingress)

			// add service
			service := newService("service-0", podLabel, "port-0", 9000)
			err = manager.serviceStore.Add(service)
			Expect(err).To(BeNil())
			manager.addService(service)

			// add endpoint
			pod := newPod("pod-0", "node-0", podLabel)
			err = manager.podStore.Add(pod)
			Expect(err).To(BeNil())
			endpoint := newEndpoint(pod, service)
			err = manager.endpointStore.Add(endpoint)
			Expect(err).To(BeNil())
			manager.addEndpoint(endpoint)
			counter := time.NewTimer(waitTime)
			count := 0
			ch := make(chan int, 1)
			go func() {
				for {
					select {
					case <-beehiveContext.Done():
						return
					case <-fch.ch:
						count++
						if count == resourceCount {
							ch <- 1
						}
					case <-counter.C:
						ch <- 1
						return
					}
				}
			}()
			<-ch
			//add secret
			secret := newSecret("secret-0", "cert", "key")
			err = manager.secretStore.Add(secret)
			Expect(err).To(BeNil())
			manager.addSecret(secret)
			counter.Reset(waitTime)
			go func() {
				for {
					select {
					case <-beehiveContext.Done():
						return
					case <-fch.ch:
						count++
						if count == resourceCount {
							ch <- 1
						}
					case <-counter.C:
						ch <- 1
						return
					}
				}
			}()
			<-ch
		})

		It("E2E_EIC_RESOURCE_INSERTION_4: Create ingress, service, endpoint, secret, delete the service and then reinsert it", func() {
			resourceCount = 6
			// add Node
			node := newNode("node-0", nodeLabel)
			err = manager.nodeStore.Add(node)
			Expect(err).To(BeNil())
			manager.addNode(node)

			// add ingress with tls
			ingress := newIngress("ingress-0", annotation, "service-0", ingressv1.ServiceBackendPort{Name: "http", Number: 9000})
			ingress.Spec.TLS = []ingressv1.IngressTLS{
				{
					Hosts:      []string{"bar.foo.com"},
					SecretName: "secret-0",
				},
			}
			err = manager.ingressStore.Add(ingress)
			Expect(err).To(BeNil())
			manager.addIngress(ingress)

			// add service
			service := newService("service-0", podLabel, "port-0", 9000)
			err = manager.serviceStore.Add(service)
			Expect(err).To(BeNil())
			manager.addService(service)

			// add endpoint
			pod := newPod("pod-0", "node-0", podLabel)
			err = manager.podStore.Add(pod)
			Expect(err).To(BeNil())
			endpoint := newEndpoint(pod, service)
			err = manager.endpointStore.Add(endpoint)
			Expect(err).To(BeNil())
			manager.addEndpoint(endpoint)

			//add secret
			secret := newSecret("secret-0", "cert", "key")
			err = manager.secretStore.Add(secret)
			Expect(err).To(BeNil())
			manager.addSecret(secret)
			counter := time.NewTimer(waitTime)
			count := 0
			ch := make(chan int, 1)
			go func() {
				for {
					select {
					case <-beehiveContext.Done():
						return
					case <-fch.ch:
						count++
						if count == resourceCount {
							ch <- 1
						}
					case <-counter.C:
						ch <- 1
						return
					}
				}
			}()
			<-ch
			err = manager.serviceStore.Delete(service)
			Expect(err).To(BeNil())
			manager.deleteService(service)
			counter.Reset(waitTime)
			go func() {
				for {
					select {
					case <-beehiveContext.Done():
						return
					case <-fch.ch:
						count++
						if count == resourceCount {
							ch <- 1
						}
					case <-counter.C:
						ch <- 1
						return
					}
				}
			}()
			<-ch
			// add service
			err = manager.serviceStore.Add(service)
			Expect(err).To(BeNil())
			manager.addService(service)
			counter.Reset(waitTime)
			go func() {
				for {
					select {
					case <-beehiveContext.Done():
						return
					case <-fch.ch:
						count++
						if count == resourceCount {
							ch <- 1
						}
					case <-counter.C:
						ch <- 1
						return
					}
				}
			}()
			<-ch
		})
	})

	Context("Test ingress deployment without tls", func() {
		It("E2E_EIC_NONTLS_DEPLOYMENT_1: Create ingress, ready node, corresponding service,endpoint and checks all"+
			" the generated envoy resources are correct", func() {

		})

		It("E2E_EIC_NONTLS_DEPLOYMENT_2: Create ingress, not ready node, corresponding service, endpoint and "+
			"checks all the generated envoy resources are correct", func() {

		})

		It("E2E_EIC_NONTLS_DEPLOYMENT_3: Create ingress, ready node, not corresponding service,  corresponding endpoint,"+
			"and checks all the generated envoy resources are correct", func() {

		})

		It("E2E_EIC_NONTLS_DEPLOYMENT_4: Create ingress, ready node, corresponding service, not corresponding endpoint"+
			"corresponding secret and checks all the generated envoy resources are correct", func() {

		})
	})

	//Context("Test ingress deployment with resource insertion", func() {
	//
	//})

	//Context("Test ingress deployment with resource deletion", func() {
	//
	//})
})
