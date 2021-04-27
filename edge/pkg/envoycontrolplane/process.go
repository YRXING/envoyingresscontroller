package envoycontrolplane

import (
	beehiveContext "github.com/kubeedge/beehive/pkg/core/context"
	"github.com/kubeedge/beehive/pkg/core/model"
	"github.com/kubeedge/kubeedge/cloud/pkg/envoyingresscontroller/constants"
	"github.com/kubeedge/kubeedge/edge/pkg/envoycontrolplane/config"
)

func feedBackError(err error,info string,request model.Message){

}
func sendToCloud(message *model.Message){
	beehiveContext.SendToGroup(string(config.Config.ContextSendGroup),*message)
}

func isEnvoyResource(resType string) bool{
	//TODO: return resType == envoyConfig
	return resType == constants.ResourceTypeConfigMap || resType == constants.ResourceTypeConfigMapList
}

