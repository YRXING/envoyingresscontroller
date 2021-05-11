package dao

import (
	"github.com/kubeedge/kubeedge/edge/pkg/common/dbm"
	"k8s.io/klog/v2"
)

type Endpoint struct {
	Name  string `orm:"column(name);null;type(text)";pk`
	Value string `orm:"column(Value);null;type(text)"`
}

//SaveEndpoint save endpoint
func SaveEndpoint(endpoint *Endpoint) error {
	num, err := dbm.DBAccess.Insert(endpoint)
	klog.V(4).Infof("Insert affected Num: %d, %v", num, err)
	return err
}

func DeleteEndpointByName(name string) error {
	num, err := dbm.DBAccess.QueryTable(EndpointTableName).Filter("name", name).Delete()
	klog.V(4).Infof("Delete affected Num: %d,%v", num, err)
	return err
}

func UpdateEndpoint(endpoint *Endpoint) error {
	num, err := dbm.DBAccess.Update(endpoint) //will update all field
	klog.V(4).Infof("Update affected Num: %d,%v", num, err)
	return err
}

func InsertOrUpdateEndpoint(endpoint *Endpoint) error {
	_, err := dbm.DBAccess.Raw("INSERT OR REPLACE INTO cluster (name, value) VALUES (?,?)", endpoint.Name, endpoint.Value).Exec()
	klog.V(4).Infof("update result %v", err)
	return err
}

//update special field
func UpdateEndpointField(name string, col string, value interface{}) error {
	num, err := dbm.DBAccess.QueryTable(EndpointTableName).Filter("name", name).Update(map[string]interface{}{col: value})
	klog.V(4).Infof("Update affected Num: %d,%v", num, err)
	return err
}

//update special fields
func UpdateEndpointFields(name string, cols map[string]interface{}) error {
	num, err := dbm.DBAccess.QueryTable(EndpointTableName).Filter("name", name).Update(cols)
	klog.V(4).Infof("Update affected Num: %d,%v", num, err)
	return err
}

func QueryEndpoint(name string, condition string) (*[]Endpoint, error) {
	endpoints := new([]Endpoint)
	_, err := dbm.DBAccess.QueryTable(EndpointTableName).Filter(name, condition).All(endpoints)
	if err != nil {
		return nil, err
	}
	return endpoints, nil
}

func QueryAllEndpoint() (*[]Endpoint, error) {
	endpoints := new([]Endpoint)
	_, err := dbm.DBAccess.QueryTable(EndpointTableName).All(endpoints)
	if err != nil {
		return nil, err
	}
	return endpoints, nil
}
