package dao

type Router struct {
	ID        string `orm:"column(id); size(64); pk"`
	Name      string `orm:"column(name);null;type(text)";pk`
	Value     string `orm:"column(Value);null;type(text)"`
	JsonValue string `orm:"column(JsonValue);null;type(text)"` //add for test and debug
}

func (router *Router) Type() string {
	return RouterType
}

func (router *Router) TableName() string {
	return RouterTableName
}

func (router *Router) GetID() string {
	return router.ID
}

func (router *Router) GetName() string {
	return router.Name
}

func (router *Router) GetValue() string {
	return router.Value
}

func (router *Router) GetJsonValue() string {
	return router.JsonValue
}
