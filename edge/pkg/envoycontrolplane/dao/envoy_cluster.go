package dao

import (
	"github.com/kubeedge/kubeedge/edge/pkg/common/dbm"
	"k8s.io/klog/v2"
)

type Cluster struct {
	Name string `orm:"column(name);null;type(text)";pk`
	Value string `orm:"column(Value);null;type(text)"`
}

//SaveCluster save cluster
func SaveCluster(cluster *Cluster) error  {
	num,err := dbm.DBAccess.Insert(cluster)
	klog.V(4).Infof("Insert affected Num: %d, %v", num, err)
	return err
}

func DeleteClusterByName(name string) error  {
	num,err := dbm.DBAccess.QueryTable(ClusterTableName).Filter("name",name).Delete()
	klog.V(4).Infof("Delete affected Num: %d,%v",num,err)
	return err
}

func UpdateCluster(cluster *Cluster) error  {
	num,err := dbm.DBAccess.Update(cluster) //will update all field
	klog.V(4).Infof("Update affected Num: %d,%v",num,err)
	return err
}

//update special field
func UpdateClusterField(name string,col string,value interface{}) error{
	num,err := dbm.DBAccess.QueryTable(ClusterTableName).Filter("name",name).Update(map[string]interface{}{col:value})
	klog.V(4).Infof("Update affected Num: %d,%v",num,err)
	return err
}

//update special fields
func UpdateClusterFields(name string,cols map[string]interface{}) error{
	num,err := dbm.DBAccess.QueryTable(ClusterTableName).Filter("name",name).Update(cols)
	klog.V(4).Infof("Update affected Num: %d,%v",num,err)
	return err
}


func QueryCluster(name string,condition string)(*[]Cluster,error)  {
	clusters := new([]Cluster)
	_,err := dbm.DBAccess.QueryTable(ClusterTableName).Filter(name,condition).All(clusters)
	if err != nil{
		return nil,err
	}
	return clusters,nil
}

func QueryAllCluster()(*[]Cluster,error){
	clusters := new([]Cluster)
	_,err := dbm.DBAccess.QueryTable(ClusterTableName).All(clusters)
	if err != nil{
		return nil,err
	}
	return clusters,nil
}