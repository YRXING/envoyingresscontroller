package dao

type Cluster struct {
	ID        string `orm:"column(id); size(64); pk"`
	Name      string `orm:"column(name);null;type(text)";pk`
	Value     string `orm:"column(Value);null;type(text)"`
	JsonValue string `orm:"column(JsonValue);null;type(text)"` //add for test and debug
}

func (cluster *Cluster) Type() string {
	return ClusterType
}

func (cluster *Cluster) TableName() string {
	return ClusterTableName
}

func (cluster *Cluster) GetID() string {
	return cluster.ID
}

func (cluster *Cluster) GetName() string {
	return cluster.Name
}

func (cluster *Cluster) GetValue() string {
	return cluster.Value
}

func (cluster *Cluster) GetJsonValue() string {
	return cluster.JsonValue
}
