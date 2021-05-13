package dao

type Listener struct {
	ID    string `orm:"column(id); size(64); pk"`
	Name  string `orm:"column(name);null;type(text)";pk`
	Value string `orm:"column(Value);null;type(text)"`
}

func (listener *Listener) Type() string {
	return ListenerType
}

func (listener *Listener) TableName() string {
	return ListenerTableName
}

func (listener *Listener) GetID() string {
	return listener.ID
}

func (listener *Listener) GetName() string {
	return listener.Name
}

func (listener *Listener) GetValue() string {
	return listener.Value
}
