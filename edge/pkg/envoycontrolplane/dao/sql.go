package dao

import (
	"github.com/astaxie/beego/orm"
	"github.com/kubeedge/beehive/pkg/core"
	"k8s.io/klog/v2"
)

const (
	ClusterTableName  = "cluster"
	EndpointTableName = "endpoint"
	ListenerTableName = "listener"
	RouterTableName   = "router"
	SecretTableName   = "secret"
)

//InitDBTable create table
func InitDBTable(module core.Module) {
	klog.Infof("Begin to register %v db model", module.Name())

	if !module.Enable() {
		klog.Infof("Module %s is disabled, DB meta for it will not be registered", module.Name())
		return
	}
	orm.RegisterModel(new(Cluster))
	orm.RegisterModel(new(Endpoint))
	orm.RegisterModel(new(Listener))
	orm.RegisterModel(new(Router))
	orm.RegisterModel(new(Secret))
}
