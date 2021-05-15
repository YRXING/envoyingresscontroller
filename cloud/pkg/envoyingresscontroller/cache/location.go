package cache

import (
	"strings"
	"sync"

	"github.com/kubeedge/kubeedge/cloud/pkg/envoyingresscontroller/constants"
	v1 "k8s.io/api/core/v1"
)

// NodeGroup represents a node group which should be unique
type NodeGroup string

// LocationCache cache the map of node, envoy resources
type LocationCache struct {
	// EdgeNodes is a map, key is nodeName, value is Status
	EdgeNodes sync.Map
	// secrets is a map, key is secretsName, value is secrets
	Secrets sync.Map
	// endpoints is a map, key is endpointsName, value is endpoints
	Endpoints sync.Map
	// clusters is a map, key is clustersName, value is clusters
	Clusters sync.Map
	// routes is a map, key is routesName, value is routes
	Roultes sync.Map
	// listeners is a map, key is listenersName, value is listeners
	Listeners sync.Map
	// node2group saves the 1 to n relationship of a node's groups
	// So a node can join not only one node group
	// Because the label in k8s is map[string]string, the nodegroup label can only contain one string.
	// In case a node belongs to more than one group, the groups should be separated by ; signal.
	// For example, node A belongs to nodegroup x and y, and its nodegroup label can be in the format: nodegroup: a;b
	Node2group map[string][]NodeGroup
	//group2node save2 the 1 to n relationship of a group's node members
	Group2node map[NodeGroup][]string

	ngLock sync.RWMutex
}

func (lc *LocationCache) IsEdgeNode(nodeName string) bool {
	_, ok := lc.EdgeNodes.Load(nodeName)
	return ok
}

func (lc *LocationCache) GetNodeStatus(nodeName string) (string, bool) {
	value, ok := lc.EdgeNodes.Load(nodeName)
	status, ok := value.(string)
	return status, ok
}

func (lc *LocationCache) UpdateEdgeNode(node *v1.Node) {
	var (
		status, nodeName string
		nodeReady        = false
	)
	nodeName = node.ObjectMeta.Name
	for _, nsc := range node.Status.Conditions {
		if nsc.Type == "Ready" {
			status = string(nsc.Status)
			nodeReady = true
			break
		}
	}
	if nodeReady {
		lc.EdgeNodes.Store(nodeName, status)
	}
}

func (lc *LocationCache) DeleteEdgeNode(node *v1.Node) {
	nodeName := node.ObjectMeta.Name
	lc.EdgeNodes.Delete(nodeName)
}

func (lc *LocationCache) DeleteNodeGroup(node *v1.Node) {
	if _, ok := node.Labels[constants.NODEGROUPLABEL]; ok {
		lc.ngLock.Lock()
		defer lc.ngLock.Unlock()

		nodegroup := strings.Split(node.Labels[constants.NODEGROUPLABEL], ";")

		// Ensure that the node has been recorded in nodegroup relationship
		if _, ok := lc.Node2group[node.Name]; ok {
			delete(lc.Node2group, node.Name)
		}

		for _, v := range nodegroup {
			//delete the old relationship between this node and group
			if len(v) != 0 {
				nodeGroup := NodeGroup(v)
				if _, ok := lc.Group2node[nodeGroup]; ok {
					var nodeList = make([]string, 0, 10)
					for _, nodeName := range lc.Group2node[nodeGroup] {
						if nodeName == node.Name {
							continue
						}
						nodeList = append(nodeList, nodeName)
					}
					lc.Group2node[nodeGroup] = nodeList
				}
			}
		}
	}
}

func (lc *LocationCache) UpdateNodeGroup(node *v1.Node) {
	if node.Labels != nil {
		if _, ok := node.Labels[constants.NODEGROUPLABEL]; ok {
			lc.ngLock.Lock()
			defer lc.ngLock.Unlock()
			nodegroup := strings.Split(node.Labels[constants.NODEGROUPLABEL], ";")
			lc.Node2group[node.Name] = nil
			for _, v := range nodegroup {
				if len(v) != 0 {
					nodeGroup := NodeGroup(v)
					// TODO: ensure that the nodename is unique in the corresponding nodegroup
					lc.Node2group[node.Name] = append(lc.Node2group[node.Name], nodeGroup)
					lc.Group2node[nodeGroup] = append(lc.Group2node[nodeGroup], node.Name)
				}
			}
		}
	}
}
