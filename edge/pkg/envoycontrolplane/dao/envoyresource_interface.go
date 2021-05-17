package dao

type DaoResource interface {
	Type() string
	TableName() string
	GetID() string
	GetName() string
	GetValue() string
	GetJsonValue() string
}

const (
	SecretType        = "secret"
	EndpointType      = "endpoint"
	ClusterType       = "cluster"
	RouterType        = "router"
	ListenerType      = "listener"
	ClusterTableName  = "cluster"
	EndpointTableName = "endpoint"
	ListenerTableName = "listener"
	RouterTableName   = "router"
	SecretTableName   = "secret"
)
