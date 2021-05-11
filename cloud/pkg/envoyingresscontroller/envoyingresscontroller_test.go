package envoyingresscontroller

import (
	"fmt"
	"math/rand"
	"reflect"
	"testing"
	"time"

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
		nodeStore.Add(newNode(fmt.Sprintf("node-%d", i), label))
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
							[]ingressv1.HTTPIngressPath{
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
		ingressStore.Add(newIngress(fmt.Sprintf("ingress-%d", i), annotation, serviceName[i], serviceBackendPort[i]))
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
		v1beta1IngressStore.Add(newV1beta1Ingress(fmt.Sprintf("v1beta1Ingress-%d", i), annotation, serviceName[i], serviceBackendPort[i]))
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
		serviceStore.Add(newService(fmt.Sprintf("service-%d", i), selector, "servicePort-%d", int32(rand.Intn(1000)+100)))
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
		podStore.Add(newPod(fmt.Sprintf("pod-%dd", i), nodeName, label))
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
		secretStore.Add(newSecret(fmt.Sprintf("secret-%d", i), "cert", "key"))
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
		EnvoyIngressControllerConfiguration{
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
	err = manager.EnvoyIngressController.initiateNodeGroupsWithNodes()
	if err != nil {
		t.Fatalf("Failed to initiate nodegroups with nodes, err: %v", err)
	}
	if string(manager.EnvoyIngressController.node2group["node-0"][0]) != "guiyang" || manager.EnvoyIngressController.group2node[NodeGroup("guiyang")][0] != "node-0" {
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
	if !reflect.DeepEqual(manager.EnvoyIngressController.node2group["node-0"], []NodeGroup{"guiyang", "shanxi"}) ||
		!reflect.DeepEqual(manager.EnvoyIngressController.group2node[NodeGroup("guiyang")], []string{"node-0", "node-1"}) {
		t.Errorf("Fail to add nodes to nodegroup, %v, %v", manager.EnvoyIngressController.node2group["node-0"],
			manager.EnvoyIngressController.group2node[NodeGroup("guiyang")])
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
	if !reflect.DeepEqual(manager.EnvoyIngressController.node2group["node-0"], []NodeGroup{"zhejiang"}) {
		t.Errorf("Fail to update nodes to nodegroup, %v", manager.EnvoyIngressController.group2node["zhejiang"])
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
	if len(manager.node2group["node-0"]) != 0 || len(manager.group2node["guiyang"]) != 0 ||
		len(manager.group2node["shanxi"]) != 0 {
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
	manager.ingressStore.Add(ingress)
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
	manager.ingressStore.Add(ingress)
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
	manager.ingressStore.Add(ingress)
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
	manager.ingressStore.Add(ingress)
	service := newService("service-0", selector, "port-0", 9000)
	manager.serviceStore.Add(service)
	pod := newPod("pod-0", "node-0", selector)
	manager.podStore.Add(pod)
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
	manager.ingressStore.Add(ingress)
	service := newService("service-0", selector, "port-0", 9000)
	manager.serviceStore.Add(service)
	pod := newPod("pod-0", "node-0", selector)
	manager.podStore.Add(pod)
	endpoint := newEndpoint(pod, service)
	newService := newService("service-0", selector, "port-0", 9001)
	manager.serviceStore.Update(newService)
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
	manager.ingressStore.Add(ingress)
	service := newService("service-0", selector, "port-0", 9000)
	manager.serviceStore.Add(service)
	pod := newPod("pod-0", "node-0", selector)
	manager.podStore.Add(pod)
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
	manager.ingressStore.Add(ingress)
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
	manager.ingressStore.Add(ingress)
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
	manager.ingressStore.Add(ingress)
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
	manager.ingressStore.Add(ingress)
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
	manager.ingressStore.Add(ingress)
	service := newService("service-0", selector, "port-0", 9000)
	manager.serviceStore.Add(service)
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
	manager.ingressStore.Add(ingress)
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
	if !reflect.DeepEqual(nodegroups, []NodeGroup{NodeGroup("guiyang"), NodeGroup("shanxi"), NodeGroup("zhejiang")}) {
		t.Fatalf("expected %v, got %v", []NodeGroup{NodeGroup("guiyang"), NodeGroup("shanxi"), NodeGroup("zhejiang")}, nodegroups)
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
		klog.Infof("cloudhub has received a message")
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
	manager.nodeStore.Add(node)
	manager.addNode(node)
	ingress := newIngress("ingress-0", annotation, "service-0", ingressv1.ServiceBackendPort{Name: "http", Number: 9000})
	manager.ingressStore.Add(ingress)
	service := newService("service-0", selector, "port-0", 9000)
	manager.serviceStore.Add(service)
	pod := newPod("pod-0", "node-0", selector)
	manager.podStore.Add(pod)
	endpoint := newEndpoint(pod, service)
	manager.endpointStore.Add(endpoint)
	manager.addIngress(ingress)
	counter = time.NewTimer(time.Duration(10000000000))
	ch := make(chan int, 1)
	go func() {
		for {
			select {
			case <-beehiveContext.Done():
				return
			case _ = <-fch.ch:
				count++
				if count == 4 {
					ch <- 1
				}
			case _ = <-counter.C:
				t.Fatalf("takes too long to dispatch messages")
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
		manager.ingressStore.Add(newIngress("ingressTest", annotation, service.Name,
			ingressv1.ServiceBackendPort{Name: service.Spec.Ports[0].Name, Number: service.Spec.Ports[0].Port}))
	}
	addSecrets(manager.secretStore, 1)
	err = manager.initiateNodeGroupsWithNodes()
	if err != nil {
		t.Fatalf("error initing nodegroup relationship: %v", err)
	}
	err = manager.initiateEnvoyResources()
	if err != nil {
		t.Fatalf("error initing envoy resources: %v", err)
	}
	if len(manager.EnvoyIngressController.endpointStore) != 1 {
		t.Errorf("fail to convert k8s endpoint into envoy endpoint, len: %d, %d", len(manager.EnvoyIngressController.endpointStore),
			len(manager.endpointStore.List()))
	}
}
