package envoycontrolplane

import (
	"encoding/json"
	"fmt"
	beehiveContext "github.com/kubeedge/beehive/pkg/core/context"
	"github.com/kubeedge/beehive/pkg/core/model"
	"github.com/kubeedge/kubeedge/cloud/pkg/envoyingresscontroller/constants"
	"github.com/kubeedge/kubeedge/edge/pkg/envoycontrolplane/config"
	"github.com/kubeedge/kubeedge/edge/pkg/envoycontrolplane/dao"
	"k8s.io/klog/v2"
	"strings"
)

func feedBackError(err error,info string,request model.Message){

}
func sendToCloud(message *model.Message){
	beehiveContext.SendToGroup(string(config.Config.ContextSendGroup),*message)
}


func validation(message *model.Message){

}

func isEnvoyResource(resType string) bool{
	//TODO: return resType == envoyConfig
	return resType == constants.ResourceTypeConfigMap || resType == constants.ResourceTypeConfigMapList
}

func msgDebugInfo(message *model.Message) string  {
	return fmt.Sprintf("msgID[%s] resource[%s]",message.GetID(),message.GetResource())
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

func generateContent(message model.Message) ([]byte,error){
	var err error
	var content []byte
	switch message.GetContent().(type) {
	case []uint8:
		content = message.GetContent().([]byte)
	default:
		content,err = json.Marshal(message.GetContent())
		if err != nil{
			klog.Errorf("marshal message content failed, %s",msgDebugInfo(&message))
			//TODO: feedback error
			return nil,err
		}
	}
	return content,err
}

func (e *envoyControlPlane) processInsert(message model.Message)  {
	content,err:= generateContent(message)
	if err != nil {
		klog.Errorf("insert message failed, %s",msgDebugInfo(&message))
	}

	resKey,_,_ := parseResource(message.GetResource())
	//TODO: switch resTpe cluster/endpoint/listener/router/secret
	cluster := &dao.Cluster{
		Name: resKey,
		Value: string(content),
	}
	err = dao.SaveCluster(cluster)
	if err != nil {
		klog.Errorf("save meta failed, %s: %v",msgDebugInfo(&message),err)
		//TODO: feedback error
		return
	}
}

func (e *envoyControlPlane) processUpdate(message model.Message)  {
	content,err:= generateContent(message)
	if err != nil {
		klog.Errorf("insert message failed, %s",msgDebugInfo(&message))
	}
	
}


