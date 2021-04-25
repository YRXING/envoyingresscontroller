package manager

import (
	"fmt"
	"sync"

	v1 "k8s.io/api/core/v1"
)

// LocationCache cache the map of node configmap
type LocationCache struct {
	// EdgeNodes is a map, key is nodeName, value is Status
	EdgeNodes sync.Map
	// configMapNode is a map, key is namespace/configMapName, value is nodeName
	configMapNode sync.Map

}

// PodConfigMapsAndSecrets return configmaps used by pod
func (lc *LocationCache) PodConfigMaps(pod v1.Pod) (configMaps []string) {
	for _, v := range pod.Spec.Volumes {
		if v.ConfigMap != nil {
			configMaps = append(configMaps, v.ConfigMap.Name)
		}
	}
	// used by envs
	for _, s := range pod.Spec.Containers {
		for _, ef := range s.EnvFrom {
			if ef.ConfigMapRef != nil {
				configMaps = append(configMaps, ef.ConfigMapRef.Name)
			}
		}
	}
	return
}

func (lc *LocationCache) newNodes(oldNodes []string, node string) []string {
	for _, n := range oldNodes {
		if n == node {
			return oldNodes
		}
	}
	return append(oldNodes, node)
}

// AddOrUpdatePod add pod to node, pod to configmap, configmap to pod, pod to secret, secret to pod relation
func (lc *LocationCache) AddOrUpdatePod(pod v1.Pod) {
	configMaps := lc.PodConfigMaps(pod)
	for _, c := range configMaps {
		configMapKey := fmt.Sprintf("%s/%s", pod.Namespace, c)
		// update configMapPod
		value, ok := lc.configMapNode.Load(configMapKey)
		var newNodes []string
		if ok {
			nodes, _ := value.([]string)
			newNodes = lc.newNodes(nodes, pod.Spec.NodeName)
		} else {
			newNodes = []string{pod.Spec.NodeName}
		}
		lc.configMapNode.Store(configMapKey, newNodes)
	}

}

// ConfigMapNodes return all nodes which deploy pod on with configmap
func (lc *LocationCache) ConfigMapNodes(namespace, name string) (nodes []string) {
	configMapKey := fmt.Sprintf("%s/%s", namespace, name)
	value, ok := lc.configMapNode.Load(configMapKey)
	if ok {
		if nodes, ok := value.([]string); ok {
			return nodes
		}
	}
	return
}


//
func (lc *LocationCache) GetNodeStatus(nodeName string) (string, bool) {
	value, ok := lc.EdgeNodes.Load(nodeName)
	status, ok := value.(string)
	return status, ok
}

// UpdateEdgeNode is to maintain edge nodes name upto-date by querying kubernetes client
func (lc *LocationCache) UpdateEdgeNode(nodeName string, status string) {
	lc.EdgeNodes.Store(nodeName, status)
}

// DeleteConfigMap from cache
func (lc *LocationCache) DeleteConfigMap(namespace, name string) {
	lc.configMapNode.Delete(fmt.Sprintf("%s/%s", namespace, name))
}


// DeleteNode from cache
func (lc *LocationCache) DeleteNode(nodeName string) {
	lc.EdgeNodes.Delete(nodeName)
}

// GetAll edge nodes from cache
func (lc *LocationCache) GetAllEdgeNodes() []string  {
	edgenodes := []string{}
	lc.EdgeNodes.Range(func(key interface{},value interface{}) bool{
		en := value.(v1.Node).Name
		edgenodes = append(edgenodes,en)
		return true
	})

	return edgenodes
}

