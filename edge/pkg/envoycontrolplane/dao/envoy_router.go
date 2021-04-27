package dao

import (
	"github.com/kubeedge/kubeedge/edge/pkg/common/dbm"
	"k8s.io/klog/v2"
)

type Router struct {
	Name string `orm:"column(name);null;type(text)";pk`
	Value string `orm:"column(Value);null;type(text)"`
}

//SaveRouter save Router
func SaveRouter(router *Router) error  {
	num,err := dbm.DBAccess.Insert(router)
	klog.V(4).Infof("Insert affected Num: %d, %v", num, err)
	return err
}

func DeleteRouterByName(name string) error  {
	num,err := dbm.DBAccess.QueryTable(RouterTableName).Filter("name",name).Delete()
	klog.V(4).Infof("Delete affected Num: %d,%v",num,err)
	return err
}

func UpdateRouter(router *Router) error  {
	num,err := dbm.DBAccess.Update(router) //will update all field
	klog.V(4).Infof("Update affected Num: %d,%v",num,err)
	return err
}

//update special field
func UpdateRouterField(name string,col string,value interface{}) error{
	num,err := dbm.DBAccess.QueryTable(RouterTableName).Filter("name",name).Update(map[string]interface{}{col:value})
	klog.V(4).Infof("Update affected Num: %d,%v",num,err)
	return err
}

//update special fields
func UpdateRouterFields(name string,cols map[string]interface{}) error{
	num,err := dbm.DBAccess.QueryTable(RouterTableName).Filter("name",name).Update(cols)
	klog.V(4).Infof("Update affected Num: %d,%v",num,err)
	return err
}


func QueryRouter(name string,condition string)(*[]Router,error)  {
	routers := new([]Router)
	_,err := dbm.DBAccess.QueryTable(RouterTableName).Filter(name,condition).All(Routers)
	if err != nil{
		return nil,err
	}
	return routers,nil
}

func QueryAllRouter()(*[]Router,error){
	routers := new([]Router)
	_,err := dbm.DBAccess.QueryTable(RouterTableName).All(routers)
	if err != nil{
		return nil,err
	}
	return routers,nil
}