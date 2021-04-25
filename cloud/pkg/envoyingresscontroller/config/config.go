package config

import (
	"github.com/kubeedge/kubeedge/pkg/apis/componentconfig/cloudcore/v1alpha1"
	"sync"

)

var Config Configure
var once sync.Once

type Configure struct {
	v1alpha1.EnvoyIngressControllerConfiguration
}

func InitConfigure(eicc *v1alpha1.EnvoyIngressControllerConfiguration) {
	once.Do(func() {
		Config = Configure{
			EnvoyIngressControllerConfiguration: *eicc,
		}
	})
}