package envoyingresscontroller

import (
	"sync"

	envoy_cache "github.com/kubeedge/kubeedge/cloud/pkg/envoyingresscontroller/cache"
)

type IsEnvoyResource interface {
	String() string
	Reset()
	ProtoMessage()
}

type Resource struct {
	Name            string                         `json:"name,omitempty"`
	Namespace       string                         `json:"namespace,omitempty"`
	ResourceVersion string                         `json:"resourceVersion,omitempty"`
	NodeGroup       map[envoy_cache.NodeGroup]bool `json:"-"`
	IngressRef      map[string]bool                `json:"-"`
	Spec            IsEnvoyResource
	RWLock          sync.RWMutex
}

const (
	SECRET   string = "Secret"
	ENDPOINT string = "Endpoint"
	CLUSTER  string = "Cluster"
	ROUTE    string = "Route"
	LISTENER string = "Listener"
)
