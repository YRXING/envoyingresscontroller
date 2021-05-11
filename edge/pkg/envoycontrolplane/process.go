package envoycontrolplane

import (
	"encoding/json"
	"fmt"
	"strings"

	beehiveContext "github.com/kubeedge/beehive/pkg/core/context"
	"github.com/kubeedge/beehive/pkg/core/model"
	"github.com/kubeedge/kubeedge/edge/pkg/envoycontrolplane/dao"
	"k8s.io/klog/v2"
)

func feedBackError(err error, info string, request model.Message) {

}
func sendToCloud(message *model.Message) {
	//beehiveContext.SendToGroup(string(config.Config.ContextSendGroup), *message)
}

func validation(message *model.Message) {

}

//func isEnvoyResource(resType string) bool {
//	//TODO: return resType == envoyConfig
//	return resType == constants.ResourceTypeConfigMap || resType == constants.ResourceTypeConfigMapList
//}

func msgDebugInfo(message *model.Message) string {
	return fmt.Sprintf("msgID[%s] resource[%s]", message.GetID(), message.GetResource())
}

// Resource format: <namespace>/<restype>[/resid]
// return <reskey, restype, resid>
func parseResource(resource string) (string, string, string) {
	tokens := strings.Split(resource, "/")
	resType := ""
	resID := ""
	switch len(tokens) {
	case 2:
		resType = tokens[len(tokens)-1]
	case 3:
		resType = tokens[len(tokens)-2]
		resID = tokens[len(tokens)-1]
	default:
	}
	return resource, resType, resID
}

func generateContent(message model.Message) ([]byte, error) {
	var err error
	var content []byte
	switch message.GetContent().(type) {
	case []uint8:
		content = message.GetContent().([]byte)
	default:
		content, err = json.Marshal(message.GetContent())
		if err != nil {
			klog.Errorf("marshal message content failed, %s", msgDebugInfo(&message))
			//TODO: feedback error
			return nil, err
		}
	}
	return content, err
}

func (e *envoyControlPlane) processInsert(message model.Message) {
	content, err := generateContent(message)
	if err != nil {
		klog.Errorf("insert message failed, %s", msgDebugInfo(&message))
	}
	resKey, resType, _ := parseResource(message.GetResource())

	//TODO: switch resTpe cluster/endpoint/listener/router/secret
	switch resType {
	case string(SECRET):
		var envoySecret = &EnvoySecret{}
		err = json.Unmarshal(content, envoySecret)
		if err != nil {
			klog.Errorf("failed to unmarshal content into envoy secret, error: %v", err)
		}
		e.envoySecretLock.Lock()
		e.envoySecrets[envoySecret.Name] = envoySecret
		e.envoySecretLock.Unlock()
		secret := &dao.Secret{
			Name:  resKey,
			Value: string(content),
		}
		err = dao.InsertOrUpdateSecret(secret)
		if err != nil {
			klog.Errorf("save meta failed, %s: %v", msgDebugInfo(&message), err)
			return
		}
	case string(ENDPOINT):
		var envoyEndpoint = &EnvoyEndpoint{}
		err = json.Unmarshal(content, envoyEndpoint)
		if err != nil {
			klog.Errorf("failed to unmarshal content into envoy endpoint, error: %v", err)
		}
		e.envoyEndpointLock.Lock()
		e.envoyEndpoints[envoyEndpoint.Name] = envoyEndpoint
		e.envoyEndpointLock.Unlock()
		endpoint := &dao.Endpoint{
			Name:  resKey,
			Value: string(content),
		}
		err = dao.InsertOrUpdateEndpoint(endpoint)
		if err != nil {
			klog.Errorf("save meta failed, %s: %v", msgDebugInfo(&message), err)
			return
		}
	case string(CLUSTER):
		var envoyCluster = &EnvoyCluster{}
		err = json.Unmarshal(content, envoyCluster)
		if err != nil {
			klog.Errorf("failed to unmarshal content into envoy cluster, error: %v", err)
		}
		e.envoyClusterLock.Lock()
		e.envoyClusters[envoyCluster.Name] = envoyCluster
		e.envoyClusterLock.Unlock()
		cluster := &dao.Cluster{
			Name:  resKey,
			Value: string(content),
		}
		err = dao.InsertOrUpdateCluster(cluster)
		if err != nil {
			klog.Errorf("save meta failed, %s: %v", msgDebugInfo(&message), err)
			return
		}
	case string(ROUTE):
		var envoyRoute = &EnvoyRoute{}
		err = json.Unmarshal(content, envoyRoute)
		if err != nil {
			klog.Errorf("failed to unmarshal content into envoy route, error: %v", err)
		}
		e.envoyRouteLock.Lock()
		e.envoyRoutes[envoyRoute.Name] = envoyRoute
		e.envoyRouteLock.Unlock()
		route := &dao.Router{
			Name:  resKey,
			Value: string(content),
		}
		err = dao.InsertOrUpdateRouter(route)
		if err != nil {
			klog.Errorf("save meta failed, %s: %v", msgDebugInfo(&message), err)
			return
		}
	case string(LISTENER):
		var envoyListener = &EnvoyListener{}
		err = json.Unmarshal(content, envoyListener)
		if err != nil {
			klog.Errorf("failed to unmarshal content into envoy listener, error: %v", err)
		}
		e.envoyListenerLock.Lock()
		e.envoyListeners[envoyListener.Name] = envoyListener
		e.envoyListenerLock.Unlock()
		listener := &dao.Listener{
			Name:  resKey,
			Value: string(content),
		}
		err = dao.InsertOrUpdateListener(listener)
		if err != nil {
			klog.Errorf("save meta failed, %s: %v", msgDebugInfo(&message), err)
			return
		}
	}
}

func (e *envoyControlPlane) processDelete(message model.Message) {
	content, err := generateContent(message)
	if err != nil {
		klog.Errorf("insert message failed, %s", msgDebugInfo(&message))
	}
	resKey, resType, _ := parseResource(message.GetResource())

	//TODO: switch resTpe cluster/endpoint/listener/router/secret
	switch resType {
	case string(SECRET):
		var envoySecret EnvoySecret
		err = json.Unmarshal(content, &envoySecret)
		if err != nil {
			klog.Errorf("failed to unmarshal content into envoy secret, error: %v", err)
		}
		e.envoySecretLock.Lock()
		if _, ok := e.envoySecrets[envoySecret.Name]; ok {
			delete(e.envoySecrets, envoySecret.Name)
			e.envoySecretLock.Unlock()
			err = dao.DeleteSecretByName(resKey)
			if err != nil {
				klog.Errorf("delete secret failed,%s", msgDebugInfo(&message))
				return
			}
		}
	case string(ENDPOINT):
		var envoyEndpoint EnvoyEndpoint
		err = json.Unmarshal(content, &envoyEndpoint)
		if err != nil {
			klog.Errorf("failed to unmarshal content into envoy endpoint, error: %v", err)
		}
		e.envoyEndpointLock.Lock()
		if _, ok := e.envoyEndpoints[envoyEndpoint.Name]; ok {
			delete(e.envoyEndpoints, envoyEndpoint.Name)
			e.envoyEndpointLock.Unlock()
			err = dao.DeleteEndpointByName(resKey)
			if err != nil {
				klog.Errorf("delete endpoint failed,%s", msgDebugInfo(&message))
				return
			}
		}
	case string(CLUSTER):
		var envoyCluster EnvoyCluster
		err = json.Unmarshal(content, &envoyCluster)
		if err != nil {
			klog.Errorf("failed to unmarshal content into envoy cluster, error: %v", err)
		}
		e.envoyClusterLock.Lock()
		if _, ok := e.envoyClusters[envoyCluster.Name]; ok {
			delete(e.envoyClusters, envoyCluster.Name)
			e.envoyClusterLock.Unlock()
			err = dao.DeleteClusterByName(resKey)
			if err != nil {
				klog.Errorf("delete cluster failed,%s", msgDebugInfo(&message))
				return
			}
		}
	case string(ROUTE):
		var envoyRoute EnvoyRoute
		err = json.Unmarshal(content, &envoyRoute)
		if err != nil {
			klog.Errorf("failed to unmarshal content into envoy route, error: %v", err)
		}
		e.envoyRouteLock.Lock()
		if _, ok := e.envoyRoutes[envoyRoute.Name]; ok {
			delete(e.envoyRoutes, envoyRoute.Name)
			e.envoyRouteLock.Unlock()
			err = dao.DeleteRouterByName(resKey)
			if err != nil {
				klog.Errorf("delete route failed,%s", msgDebugInfo(&message))
				return
			}
		}
	case string(LISTENER):
		var envoyListener EnvoyListener
		err = json.Unmarshal(content, &envoyListener)
		if err != nil {
			klog.Errorf("failed to unmarshal content into envoy listener, error: %v", err)
		}
		e.envoyListenerLock.Lock()
		if _, ok := e.envoyListeners[envoyListener.Name]; ok {
			delete(e.envoyListeners, envoyListener.Name)
			e.envoyListenerLock.Unlock()
			err = dao.DeleteListenerByName(resKey)
			if err != nil {
				klog.Errorf("delete listener failed,%s", msgDebugInfo(&message))
				return
			}
		}
	}
}

func (e *envoyControlPlane) process(message model.Message) {
	operation := message.GetOperation()
	switch operation {
	case model.InsertOperation:
		e.processInsert(message)
	case model.DeleteOperation:
		e.processDelete(message)
	}
}

func (e *envoyControlPlane) runEnvoyControlPlane() {
	go func() {
		for {
			select {
			case <-beehiveContext.Done():
				klog.Warning("EnvoyControlPlane mainloop stop")
				return
			default:
			}
			if msg, err := beehiveContext.Receive(e.Name()); err != nil {
				klog.V(2).Infof("get a message %+v", msg)
				e.process(msg)
			} else {
				klog.Errorf("get a message %+v: %v", msg, err)
			}
		}
	}()
}
