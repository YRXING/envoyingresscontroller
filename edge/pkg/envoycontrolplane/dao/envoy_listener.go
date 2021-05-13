package dao

import (
	"github.com/kubeedge/kubeedge/edge/pkg/common/dbm"
	"k8s.io/klog/v2"
)

type Listener struct {
	ID    string `orm:"column(id); size(64); pk"`
	Name  string `orm:"column(name);null;type(text)";pk`
	Value string `orm:"column(Value);null;type(text)"`
}

//SaveListener save Listener
func SaveListener(listener *Listener) error {
	num, err := dbm.DBAccess.Insert(listener)
	klog.V(4).Infof("Insert affected Num: %d, %v", num, err)
	return err
}

func DeleteListenerByName(name string) error {
	num, err := dbm.DBAccess.QueryTable(ListenerTableName).Filter("name", name).Delete()
	klog.V(4).Infof("Delete affected Num: %d,%v", num, err)
	return err
}

func UpdateListener(listener *Listener) error {
	num, err := dbm.DBAccess.Update(listener) //will update all field
	klog.V(4).Infof("Update affected Num: %d,%v", num, err)
	return err
}

func InsertOrUpdateListener(listener *Listener) error {
	_, err := dbm.DBAccess.Raw("INSERT OR REPLACE INTO listener (name, value) VALUES (?,?)", listener.Name, listener.Value).Exec()
	klog.V(4).Infof("update result %v", err)
	return err
}

//update special field
func UpdateListenerField(name string, col string, value interface{}) error {
	num, err := dbm.DBAccess.QueryTable(ListenerTableName).Filter("name", name).Update(map[string]interface{}{col: value})
	klog.V(4).Infof("Update affected Num: %d,%v", num, err)
	return err
}

//update special fields
func UpdateListenerFields(name string, cols map[string]interface{}) error {
	num, err := dbm.DBAccess.QueryTable(ListenerTableName).Filter("name", name).Update(cols)
	klog.V(4).Infof("Update affected Num: %d,%v", num, err)
	return err
}

func QueryListener(name string, condition string) (*[]Listener, error) {
	listeners := new([]Listener)
	_, err := dbm.DBAccess.QueryTable(ListenerTableName).Filter(name, condition).All(listeners)
	if err != nil {
		return nil, err
	}
	return listeners, nil
}

func QueryAllListener() (*[]Listener, error) {
	listeners := new([]Listener)
	_, err := dbm.DBAccess.QueryTable(ListenerTableName).All(listeners)
	if err != nil {
		return nil, err
	}
	return listeners, nil
}
