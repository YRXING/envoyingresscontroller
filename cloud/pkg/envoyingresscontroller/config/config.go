package config

import (
	"github.com/kubeedge/kubeedge/cloud/pkg/envoyingresscontroller/config/v1alpha1"

	"sync"

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