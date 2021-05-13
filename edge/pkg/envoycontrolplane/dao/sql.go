package dao

import (
	"fmt"

	"github.com/astaxie/beego/orm"
	"github.com/kubeedge/beehive/pkg/core"
	"github.com/kubeedge/kubeedge/edge/pkg/common/dbm"
	"k8s.io/klog/v2"
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

func DeleteResource(daoResource DaoResource) error {
	num, err := dbm.DBAccess.QueryTable(daoResource.TableName()).Filter("name", daoResource.GetName()).Delete()
	klog.V(4).Infof("Delete affected Num: %d,%v", num, err)
	return err
}

func UpdateResource(daoResource DaoResource) error {
	num, err := dbm.DBAccess.Update(daoResource)
	klog.V(4).Infof("Update affected Num: %d,%v", num, err)
	return err
}

func InsertOrUpdateResource(daoResource DaoResource) error {
	_, err := dbm.DBAccess.Raw("INSERT OR REPLACE INTO "+daoResource.Type()+" (id, name, value) VALUES (?,?,?)",
		daoResource.GetID(), daoResource.GetName(), daoResource.GetValue()).Exec()
	klog.V(4).Infof("update result %v", err)
	return err
}

func QueryResource(daoResource DaoResource, colName, value string) (*[]DaoResource, error) {
	var daoResources interface{}
	var daoSlice *[]DaoResource
	switch daoResource.Type() {
	case SecretType:
		daoResources = new([]*Secret)
	case EndpointType:
		daoResources = new([]*Endpoint)
	case ClusterType:
		daoResources = new([]*Cluster)
	case RouterType:
		daoResources = new([]*Router)
	case ListenerType:
		daoResources = new([]*Listener)
	default:
		return nil, fmt.Errorf("unknown resource type")
	}
	_, err := dbm.DBAccess.QueryTable(daoResource.TableName()).Filter(colName, value).All(daoResources)
	if err != nil {
		return nil, err
	}
	daoSlice = new([]DaoResource)
	switch daoResource.Type() {
	case SecretType:
		for _, value := range *(daoResources.(*[]*Secret)) {
			(*daoSlice) = append((*daoSlice), value)
		}
	case EndpointType:
		for _, value := range *(daoResources.(*[]*Endpoint)) {
			(*daoSlice) = append((*daoSlice), value)
		}
	case ClusterType:
		for _, value := range *(daoResources.(*[]*Cluster)) {
			(*daoSlice) = append((*daoSlice), value)
		}
	case RouterType:
		for _, value := range *(daoResources.(*[]*Router)) {
			(*daoSlice) = append((*daoSlice), value)
		}
	case ListenerType:
		for _, value := range *(daoResources.(*[]*Listener)) {
			(*daoSlice) = append((*daoSlice), value)
		}
	}
	return daoSlice, nil
}

func QueryAllResources(daoResource DaoResource) (*[]DaoResource, error) {
	var daoResources interface{}
	var daoSlice *[]DaoResource
	switch daoResource.Type() {
	case SecretType:
		daoResources = new([]*Secret)
	case EndpointType:
		daoResources = new([]*Endpoint)
	case ClusterType:
		daoResources = new([]*Cluster)
	case RouterType:
		daoResources = new([]*Router)
	case ListenerType:
		daoResources = new([]*Listener)
	default:
		return nil, fmt.Errorf("unknown resource type")
	}
	_, err := dbm.DBAccess.QueryTable(daoResource.TableName()).All(daoResources)
	if err != nil {
		return nil, err
	}
	daoSlice = new([]DaoResource)
	switch daoResource.Type() {
	case SecretType:
		for _, value := range *(daoResources.(*[]*Secret)) {
			(*daoSlice) = append((*daoSlice), value)
		}
	case EndpointType:
		for _, value := range *(daoResources.(*[]*Endpoint)) {
			(*daoSlice) = append((*daoSlice), value)
		}
	case ClusterType:
		for _, value := range *(daoResources.(*[]*Cluster)) {
			(*daoSlice) = append((*daoSlice), value)
		}
	case RouterType:
		for _, value := range *(daoResources.(*[]*Router)) {
			(*daoSlice) = append((*daoSlice), value)
		}
	case ListenerType:
		for _, value := range *(daoResources.(*[]*Listener)) {
			(*daoSlice) = append((*daoSlice), value)
		}
	}
	return daoSlice, nil
}
