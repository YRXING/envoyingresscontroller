package dao

type Endpoint struct {
	ID    string `orm:"column(id); size(64); pk"`
	Name  string `orm:"column(name);null;type(text)";pk`
	Value string `orm:"column(Value);null;type(text)"`
}

func (endpoint *Endpoint) Type() string {
	return EndpointType
}

func (endpoint *Endpoint) TableName() string {
	return EndpointTableName
}

func (endpoint *Endpoint) GetID() string {
	return endpoint.ID
}

func (endpoint *Endpoint) GetName() string {
	return endpoint.Name
}

func (endpoint *Endpoint) GetValue() string {
	return endpoint.Value
}
