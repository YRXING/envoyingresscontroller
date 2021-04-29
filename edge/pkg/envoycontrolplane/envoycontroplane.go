package envoycontrolplane

import (
	"github.com/kubeedge/beehive/pkg/core"
	beehiveContext "github.com/kubeedge/beehive/pkg/core/context"
	"github.com/kubeedge/kubeedge/pkg/apis/componentconfig/edgecore/v1alpha1"
	"k8s.io/klog/v2"
)
const(
	EnvoyCpntrolPlane = "envoyControlPlane"
)

type envoyControlPlane struct {
	enable bool
}

func newControlPlane(enable bool) *envoyControlPlane{
	return &envoyControlPlane{enable: enable}
}

func Register(ecpc *v1alpha1.EnvoyControlPlaneConfig){
	controlplane := newControlPlane(ecpc.Enable)
	core.Register(controlplane)
}

func (*envoyControlPlane) Name() string{
	return EnvoyCpntrolPlane
}

func (*envoyControlPlane) Group() string{
	return "controlplane"
}

func (e *envoyControlPlane) Enable() bool {
	return e.enable
}

func (e *envoyControlPlane) Start(){
	go func() {
		for{
			select {
			case <-beehiveContext.Done():
				klog.Warning("envoyControlPlane stop")
				return
			}
		}
	}()
	e.runEnvoyControlPlane()
}