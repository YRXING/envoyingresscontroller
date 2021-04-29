package dao

import (
	"github.com/kubeedge/kubeedge/edge/pkg/common/dbm"
	"k8s.io/klog/v2"
)

type Secret struct {
	Name string `orm:"column(name);null;type(text)";pk`
	Value string `orm:"column(Value);null;type(text)"`
}

//SaveSecret save Secret
func SaveSecret(secret *Secret) error  {
	num,err := dbm.DBAccess.Insert(secret)
	klog.V(4).Infof("Insert affected Num: %d, %v", num, err)
	return err
}

func DeleteSecretByName(name string) error  {
	num,err := dbm.DBAccess.QueryTable(SecretTableName).Filter("name",name).Delete()
	klog.V(4).Infof("Delete affected Num: %d,%v",num,err)
	return err
}

func UpdateSecret(secret *Secret) error  {
	num,err := dbm.DBAccess.Update(secret) //will update all field
	klog.V(4).Infof("Update affected Num: %d,%v",num,err)
	return err
}

func InsertOrUpdateSecret(secret *Secret) error {
	_,err := dbm.DBAccess.Raw("INSERT OR REPLACE INTO cluster (name, value) VALUES (?,?)",secret.Name,secret.Value).Exec()
	klog.V(4).Infof("update result %v",err)
	return err
}

//update special field
func UpdateSecretField(name string,col string,value interface{}) error{
	num,err := dbm.DBAccess.QueryTable(SecretTableName).Filter("name",name).Update(map[string]interface{}{col:value})
	klog.V(4).Infof("Update affected Num: %d,%v",num,err)
	return err
}

//update special fields
func UpdateSecretFields(name string,cols map[string]interface{}) error{
	num,err := dbm.DBAccess.QueryTable(SecretTableName).Filter("name",name).Update(cols)
	klog.V(4).Infof("Update affected Num: %d,%v",num,err)
	return err
}


func QuerySecret(name string,condition string)(*[]Secret,error)  {
	secrets := new([]Secret)
	_,err := dbm.DBAccess.QueryTable(SecretTableName).Filter(name,condition).All(Secrets)
	if err != nil{
		return nil,err
	}
	return secrets,nil
}

func QueryAllSecret()(*[]Secret,error){
	secrets := new([]Secret)
	_,err := dbm.DBAccess.QueryTable(SecretTableName).All(Secrets)
	if err != nil{
		return nil,err
	}
	return secrets,nil
}