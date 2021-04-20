package config

import (
	"github.com/kubeedge/kubeedge/cloud/pkg/envoyingresscontroller/types"
	"sync"

)

var Config Configure
var once sync.Once

type Configure struct {
	types.EnvoyIngressControllerConfiguration
}

func InitConfigure(eicc *types.EnvoyIngressControllerConfiguration) {
	once.Do(func() {
		Config = Configure{
			EnvoyIngressControllerConfiguration: *eicc,
		}
	})
}