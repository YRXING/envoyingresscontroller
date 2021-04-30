package controller

import (
	"context"
	beehiveContext "github.com/kubeedge/beehive/pkg/core/context"
	"github.com/kubeedge/beehive/pkg/core/model"
	"github.com/kubeedge/kubeedge/cloud/pkg/common/client"
	"github.com/kubeedge/kubeedge/cloud/pkg/common/informers"
	"github.com/kubeedge/kubeedge/cloud/pkg/envoyingresscontroller/modules"
	metaV1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	k8sinformers "k8s.io/client-go/informers"
	clientgov1 "k8s.io/client-go/listers/core/v1"

	"github.com/kubeedge/kubeedge/cloud/pkg/envoyingresscontroller/constants"
	"github.com/kubeedge/kubeedge/cloud/pkg/envoyingresscontroller/manager"
	"github.com/kubeedge/kubeedge/cloud/pkg/envoyingresscontroller/messagelayer"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes"
	"k8s.io/klog/v2"
)

//Downstream Controller watch kubernetes api server and send change to edge
type DownstreamController struct {
	//it is a clientSet
	kubeClient kubernetes.Interface

	//send and receive message
	messageLayer messagelayer.MessageLayer

	//manage configmap events
	configmapManager *manager.ConfigMapManager

	nodeManager *manager.NodesManager

	// use it to querey which node to send
	lc *manager.LocationCache

	svcLister clientgov1.ServiceLister

	podLister clientgov1.PodLister

	configmaplister clientgov1.ConfigMapLister

}

func (dc *DownstreamController) syncConfigMap(){
	for{
		select {
		case <-beehiveContext.Done():
			klog.Warning("Stop envoyingresscontroller downstream syncConfigMap")
			return
		case e := <-dc.configmapManager.Events():
			configMap,ok := e.Object.(*v1.ConfigMap)
			if !ok{
				klog.Warningf("object type: %T unsupported",configMap)
				continue
			}

			if configMap.Namespace != constants.EnvoyNamespace{
				continue
			}
			var operation string
			switch e.Type {
			case watch.Added:
				operation = model.InsertOperation
			case watch.Modified:
				operation = model.UpdateOperation
			case watch.Deleted:
				operation = model.DeleteOperation
			default:
				// unsupported operation, no need to send to any node
				klog.Warningf("config map event type: %s unsupported",e.Type)
				continue //continue to next select
			}

			//TODO: send configmap to edge node which it cares in the future
			nodes := dc.lc.GetAllEdgeNodes()
			//if e.Type ==watch.Deleted{
			//	dc.lc.DeleteConfigMap(configMap.Namespace,configMap.Name)
			//}
			klog.V(4).Infof("there are %d nodes need to sync config map,operation:%s",len(nodes),e.Type)
			for _, n := range nodes{
				msg := model.NewMessage("")
				msg.SetResourceVersion(configMap.ResourceVersion)
				resource , err := messagelayer.BuildResource(n,constants.EnvoyNamespace,model.ResourceTypeConfigmap,configMap.Name)
				if err != nil{
					klog.Warningf("build message resource failed with err: %s",err)
					continue
				}
				msg.BuildRouter(modules.EnvoyIngressControllerModuleName, constants.EnvoyGroup,resource,operation)
				msg.Content = configMap
				err = dc.messageLayer.Send(*msg)
				if err != nil{
					klog.Warningf("send message failed with error: %s,operation:%s, resource: %s",err,msg.GetOperation(),msg.GetResource())
				}else{
					klog.V(4).Infof("send message successfully, operation: %s, resource: %s",msg.GetOperation(),msg.GetResource())
				}
			}
		}
	}
}

func (dc *DownstreamController) syncEdgeNodes() {
	for {
		select {
		case <-beehiveContext.Done():
			klog.Warning("Stop edgecontroller downstream syncEdgeNodes loop")
			return
		case e := <-dc.nodeManager.Events():
			node, ok := e.Object.(*v1.Node)
			if !ok {
				klog.Warningf("Object type: %T unsupported", e.Object)
				continue
			}
			switch e.Type {
			case watch.Added:
				fallthrough
			case watch.Modified:
				// When node comes to running, send all configmaps related to envoy to edge
				for _, nsc := range node.Status.Conditions {
					if nsc.Type != v1.NodeReady {
						continue
					}
					nstatus := string(nsc.Status)
					status, _ := dc.lc.GetNodeStatus(node.ObjectMeta.Name)
					dc.lc.UpdateEdgeNode(node.ObjectMeta.Name, nstatus)
					if nsc.Status != v1.ConditionTrue || status == nstatus {
						continue
					}

					//send all configmaps related to envoy to edge
					msg := model.NewMessage("").SetRoute(modules.EnvoyIngressControllerModuleName,constants.EnvoyGroup)
					resource ,err := messagelayer.BuildResource(node.Name,constants.EnvoyNamespace,constants.ResourceTypeConfigMapList,constants.ResourceTypeConfigMap)
					if err != nil{
						klog.Warningf("Built message resource failed with error: %s",err)
						break
					}

					configmaps , err := dc.configmaplister.ConfigMaps(constants.EnvoyNamespace).List(labels.Everything())
					if err != nil {
						klog.Warningf("Send message failed with list configmap error: %s",err)
						break
					}
					cmMsg := msg.Clone(msg).SetResourceOperation(resource,model.UpdateOperation).FillBody(configmaps)
					if err := dc.messageLayer.Send(*cmMsg);err!=nil{
						klog.Warningf("Send message failed with error: %s,operation: %s, resource: %s",err,msg.GetOperation(),msg.GetResource())
					}else{
						klog.V(4).Infof("Send message successfully, operation: %s, resource: %s", msg.GetOperation(), msg.GetResource())
					}

					break
				}
			case watch.Deleted:
				dc.lc.DeleteNode(node.ObjectMeta.Name)

				resource, err := messagelayer.BuildResource(node.Name, "namespace", constants.ResourceNode, node.Name)
				if err != nil {
					klog.Warningf("Built message resource failed with error: %s", err)
					break
				}
				msg := model.NewMessage("").
					BuildRouter(modules.EnvoyIngressControllerModuleName, constants.GroupResource, resource, model.DeleteOperation)
				err = dc.messageLayer.Send(*msg)
				if err != nil {
					klog.Warningf("send message failed with error: %s, operation: %s, resource: %s", err, msg.GetOperation(), msg.GetResource())
				} else {
					klog.V(4).Infof("send message successfully, operation: %s, resource: %s", msg.GetOperation(), msg.GetResource())
				}
			default:
				// unsupported operation, no need to send to any node
				klog.Warningf("Node event type: %s unsupported", e.Type)
			}
		}
	}
}


//Start DownstreamController
func (dc *DownstreamController) Start() error  {
	klog.Info("start downstream controller")

	go dc.syncConfigMap()
	go dc.syncEdgeNodes()

	return nil
}

// initLocating to know configmap should send to which nodes
func (dc *DownstreamController) initLocating() error  {
	set := labels.Set{manager.NodeRoleKey:manager.NodeRoleValue}
	selector := labels.SelectorFromSet(set)
	nodes ,err := dc.kubeClient.CoreV1().Nodes().List(context.Background(),metaV1.ListOptions{LabelSelector: selector.String()})
	if err != nil{
		return err
	}
	var status string
	for _, node := range nodes.Items{
		for _, nsc := range node.Status.Conditions{
			if nsc.Type == "Ready"{
				status = string(nsc.Status)
				break
			}
		}
		dc.lc.UpdateEdgeNode(node.ObjectMeta.Name,status)
	}

	return nil
}

// NewDownstreamController create a DownstreamController from config
func NewDownStreamController(k8sInformerFactory k8sinformers.SharedInformerFactory,
	keInformerFactory informers.KubeEdgeCustomeInformer,) (*DownstreamController,error){

		lc := &manager.LocationCache{}

		configMapInformer := k8sInformerFactory.Core().V1().ConfigMaps()
		configMapManager, err := manager.NewConfigMapManager(configMapInformer.Informer())
		if err != nil {
			klog.Warningf("create configmap manager failed with error: %s", err)
			return nil, err
		}

		nodeInformer := keInformerFactory.EdgeNode()
		nodesManager, err := manager.NewNodesManager(nodeInformer)
		if err != nil {
			klog.Warningf("Create nodes manager failed with error: %s", err)
			return nil, err
		}

		dc := &DownstreamController{
			kubeClient:           client.GetKubeClient(),
			configmapManager:     configMapManager,
			nodeManager:          nodesManager,
			messageLayer:         messagelayer.NewContextMessageLayer(),
			lc:                   lc,

		}
		if err := dc.initLocating(); err != nil {
			return nil, err
		}

		return dc, nil
}

