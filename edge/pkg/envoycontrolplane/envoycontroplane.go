package envoycontrolplane

import (
	"github.com/kubeedge/beehive/pkg/core"
	"github.com/kubeedge/kubeedge/pkg/apis/componentconfig/edgecore/v1alpha1"
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
	return ""
}

func (m *envoyControlPlane) Enable() bool {
	return m.enable
}

func (m *envoyControlPlane) Start(){

}