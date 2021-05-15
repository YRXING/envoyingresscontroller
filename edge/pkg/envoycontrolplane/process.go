package envoycontrolplane

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	controller "github.com/kubeedge/kubeedge/cloud/pkg/edgecontroller/constants"
	"github.com/kubeedge/kubeedge/common/constants"

	"github.com/golang/protobuf/proto"

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
	default:
		resType = tokens[len(tokens)-2]
		resID = tokens[len(tokens)-1]
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

// GetNamespace from "beehive/pkg/core/model".Model.Router.Resource
func GetNamespace(msg model.Message) (string, error) {
	sli := strings.Split(msg.GetResource(), constants.ResourceSep)
	if len(sli) <= controller.ResourceNamespaceIndex {
		return "", fmt.Errorf("namespace not found")
	}

	res := sli[controller.ResourceNamespaceIndex]
	index := controller.ResourceNamespaceIndex

	klog.V(4).Infof("The namespace is %s, %d", res, index)
	return res, nil
}

func (e *envoyControlPlane) processInsert(message model.Message) error {
	content, err := generateContent(message)
	if err != nil {
		klog.Errorf("insert message failed, %s", msgDebugInfo(&message))
		return err
	}
	resKey, resType, _ := parseResource(message.GetResource())

	//TODO: switch resTpe cluster/endpoint/listener/router/secret
	unquotedContent, err := strconv.Unquote(string(content))
	if err != nil {
		klog.Errorf("failed to unquote content, err: %v", err)
		return err
	}
	envoyResourceString, err := base64.StdEncoding.DecodeString(unquotedContent)
	if err != nil {
		klog.Errorf("failed to decode base64 encoded content into %s, err: %v", resType, err)
		return err
	}
	var daoResource dao.DaoResource
	var envoyResource EnvoyResourceInterface
	switch resType {
	case string(SECRET):
		envoyResource = &EnvoySecret{}
	case string(ENDPOINT):
		envoyResource = &EnvoyEndpoint{}
	case string(CLUSTER):
		envoyResource = &EnvoyCluster{}
	case string(ROUTE):
		envoyResource = &EnvoyRoute{}
	case string(LISTENER):
		envoyResource = &EnvoyListener{}
	default:
		return fmt.Errorf("unknown resource type")
	}
	envoyResource.SetName(resKey)
	namespace, err := GetNamespace(message)
	if err != nil {
		klog.Errorf("failed to get namespace from message, err: %v", err)
		namespace = "default"
	}
	envoyResource.SetNamespace(namespace)
	switch resType {
	case string(SECRET):
		err = proto.Unmarshal(envoyResourceString, &((envoyResource).(*EnvoySecret).Secret))
		if err != nil {
			klog.Errorf("failed to unmarshal content into envoy secret, error: %v", err)
			return err
		}
		e.envoySecretLock.Lock()
		e.envoySecrets[envoyResource.GetName()] = envoyResource.(*EnvoySecret)
		e.envoySecretLock.Unlock()
		daoResource = &dao.Secret{
			ID:    resKey,
			Name:  resKey,
			Value: unquotedContent,
		}
	case string(ENDPOINT):
		err = proto.Unmarshal(envoyResourceString, &((envoyResource.(*EnvoyEndpoint)).ClusterLoadAssignment))
		if err != nil {
			klog.Errorf("failed to unmarshal content into envoy endpoint, error: %v", err)
			return err
		}
		e.envoyEndpointLock.Lock()
		e.envoyEndpoints[envoyResource.GetName()] = envoyResource.(*EnvoyEndpoint)
		e.envoyEndpointLock.Unlock()
		daoResource = &dao.Endpoint{
			ID:    resKey,
			Name:  resKey,
			Value: unquotedContent,
		}
	case string(CLUSTER):
		err = proto.Unmarshal(envoyResourceString, &((envoyResource.(*EnvoyCluster)).Cluster))
		if err != nil {
			klog.Errorf("failed to unmarshal content into envoy cluster, error: %v", err)
			return err
		}
		e.envoyClusterLock.Lock()
		e.envoyClusters[envoyResource.GetName()] = envoyResource.(*EnvoyCluster)
		e.envoyClusterLock.Unlock()
		daoResource = &dao.Cluster{
			ID:    resKey,
			Name:  resKey,
			Value: unquotedContent,
		}
	case string(ROUTE):
		err = proto.Unmarshal(envoyResourceString, &((envoyResource.(*EnvoyRoute)).RouteConfiguration))
		if err != nil {
			klog.Errorf("failed to unmarshal content into envoy route, error: %v", err)
			return err
		}
		e.envoyRouteLock.Lock()
		e.envoyRoutes[envoyResource.GetName()] = envoyResource.(*EnvoyRoute)
		e.envoyRouteLock.Unlock()
		daoResource = &dao.Router{
			ID:    resKey,
			Name:  resKey,
			Value: unquotedContent,
		}
	case string(LISTENER):
		err = proto.Unmarshal(envoyResourceString, &((envoyResource.(*EnvoyListener)).Listener))
		if err != nil {
			klog.Errorf("failed to unmarshal content into envoy listener, error: %v", err)
			return err
		}
		e.envoyListenerLock.Lock()
		e.envoyListeners[envoyResource.GetName()] = envoyResource.(*EnvoyListener)
		e.envoyListenerLock.Unlock()
		daoResource = &dao.Listener{
			ID:    resKey,
			Name:  resKey,
			Value: unquotedContent,
		}
	}
	err = dao.InsertOrUpdateResource(daoResource)
	if err != nil {
		klog.Errorf("save meta failed, %s: %v", msgDebugInfo(&message), err)
		return err
	}
	return nil
}

func (e *envoyControlPlane) processDelete(message model.Message) error {
	content, err := generateContent(message)
	if err != nil {
		klog.Errorf("insert message failed, %s", msgDebugInfo(&message))
	}
	resKey, resType, _ := parseResource(message.GetResource())

	unquotedContent, err := strconv.Unquote(string(content))
	if err != nil {
		klog.Errorf("failed to unquote content, err: %v", err)
		return err
	}
	envoyResourceString, err := base64.StdEncoding.DecodeString(unquotedContent)
	if err != nil {
		klog.Errorf("failed to decode base64 encoded content into %s, err: %v", resType, err)
		return err
	}
	var daoResource dao.DaoResource
	var envoyResource EnvoyResourceInterface
	switch resType {
	case string(SECRET):
		envoyResource = &EnvoySecret{}
	case string(ENDPOINT):
		envoyResource = &EnvoyEndpoint{}
	case string(CLUSTER):
		envoyResource = &EnvoyCluster{}
	case string(ROUTE):
		envoyResource = &EnvoyRoute{}
	case string(LISTENER):
		envoyResource = &EnvoyListener{}
	default:
		return fmt.Errorf("unknown resource type")
	}
	envoyResource.SetName(resKey)
	namespace, err := GetNamespace(message)
	if err != nil {
		klog.Errorf("failed to get namespace from message, err: %v", err)
		namespace = "default"
	}
	envoyResource.SetNamespace(namespace)
	switch resType {
	case string(SECRET):
		err = proto.Unmarshal(envoyResourceString, &((envoyResource.(*EnvoySecret)).Secret))
		if err != nil {
			klog.Errorf("failed to unmarshal content into envoy secret, error: %v", err)
		}
		e.envoySecretLock.Lock()
		if _, ok := e.envoySecrets[envoyResource.GetName()]; ok {
			delete(e.envoySecrets, envoyResource.GetName())
		}
		e.envoySecretLock.Unlock()
		daoResource = &dao.Secret{
			ID:   resKey,
			Name: resKey,
		}
	case string(ENDPOINT):
		err = json.Unmarshal(envoyResourceString, &((envoyResource.(*EnvoyEndpoint)).ClusterLoadAssignment))
		if err != nil {
			klog.Errorf("failed to unmarshal content into envoy endpoint, error: %v", err)
		}
		e.envoyEndpointLock.Lock()
		if _, ok := e.envoyEndpoints[envoyResource.GetName()]; ok {
			delete(e.envoyEndpoints, envoyResource.GetName())
		}
		e.envoyEndpointLock.Unlock()
		daoResource = &dao.Endpoint{
			ID:   resKey,
			Name: resKey,
		}
	case string(CLUSTER):
		err = proto.Unmarshal(envoyResourceString, &((envoyResource.(*EnvoyCluster)).Cluster))
		if err != nil {
			klog.Errorf("failed to unmarshal content into envoy cluster, error: %v", err)
		}
		e.envoyClusterLock.Lock()
		if _, ok := e.envoyClusters[envoyResource.GetName()]; ok {
			delete(e.envoyClusters, envoyResource.GetName())
		}
		e.envoyClusterLock.Unlock()
		daoResource = &dao.Cluster{
			ID:   resKey,
			Name: resKey,
		}
	case string(ROUTE):
		err = proto.Unmarshal(envoyResourceString, &((envoyResource.(*EnvoyRoute)).RouteConfiguration))
		if err != nil {
			klog.Errorf("failed to unmarshal content into envoy route, error: %v", err)
		}
		e.envoyRouteLock.Lock()
		if _, ok := e.envoyRoutes[envoyResource.GetName()]; ok {
			delete(e.envoyRoutes, envoyResource.GetName())
		}
		e.envoyRouteLock.Unlock()
		daoResource = &dao.Router{
			ID:   resKey,
			Name: resKey,
		}
	case string(LISTENER):
		err = proto.Unmarshal(envoyResourceString, &((envoyResource.(*EnvoyListener)).Listener))
		if err != nil {
			klog.Errorf("failed to unmarshal content into envoy listener, error: %v", err)
		}
		e.envoyListenerLock.Lock()
		if _, ok := e.envoyListeners[envoyResource.GetName()]; ok {
			delete(e.envoyListeners, envoyResource.GetName())
		}
		e.envoyListenerLock.Unlock()
		daoResource = &dao.Listener{
			ID:   resKey,
			Name: resKey,
		}
	}
	err = dao.DeleteResource(daoResource)
	if err != nil {
		klog.Errorf("delete secret failed,%s", msgDebugInfo(&message))
		return err
	}
	return nil
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
	klog.Info("envoy control plane mainloop start")
	for {
		select {
		case <-beehiveContext.Done():
			klog.Warning("EnvoyControlPlane mainloop stop")
			return
		default:
		}
		if msg, err := beehiveContext.Receive(e.Name()); err == nil {
			klog.Infof("get a message %+v", msg)
			e.process(msg)
		} else {
			klog.Errorf("get a message %+v: %v", msg, err)
		}
	}
}
