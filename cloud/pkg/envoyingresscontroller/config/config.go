package config

import (
	"sync"

	"github.com/kubeedge/kubeedge/pkg/apis/componentconfig/cloudcore/v1alpha1"
)

var Config Configure
var once sync.Once

type Configure struct {
	v1alpha1.EnvoyIngressController
}

func InitConfigure(eic *v1alpha1.EnvoyIngressController) {
	once.Do(func() {
		Config = Configure{
			EnvoyIngressController: *eic,
		}
	})
}
