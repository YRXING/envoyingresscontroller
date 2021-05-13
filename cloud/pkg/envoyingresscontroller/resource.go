package envoyingresscontroller

import (
	envoy_cluster_v3 "github.com/envoyproxy/go-control-plane/envoy/config/cluster/v3"
	envoy_endpoint_v3 "github.com/envoyproxy/go-control-plane/envoy/config/endpoint/v3"
	envoy_listener_v3 "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	envoy_route_v3 "github.com/envoyproxy/go-control-plane/envoy/config/route/v3"
	envoy_tls_v3 "github.com/envoyproxy/go-control-plane/envoy/extensions/transport_sockets/tls/v3"
)

type EnvoySecret struct {
	Name            string      `json:"name,omitempty"`
	Namespace       string      `json:"namespace,omitempty"`
	ResourceVersion string      `json:"resourceVersion,omitempty"`
	NodeGroup       []NodeGroup `json:"-"`
	Secret          envoy_tls_v3.Secret
}

type EnvoyEndpoint struct {
	Name                  string      `json:"name,omitempty"`
	Namespace             string      `json:"namespace,omitempty"`
	ResourceVersion       string      `json:"resourceVersion,omitempty"`
	NodeGroup             []NodeGroup `json:"-"`
	ClusterLoadAssignment envoy_endpoint_v3.ClusterLoadAssignment
}

type EnvoyCluster struct {
	Name            string      `json:"name,omitempty"`
	Namespace       string      `json:"namespace,omitempty"`
	ResourceVersion string      `json:"resourceVersion,omitempty"`
	NodeGroup       []NodeGroup `json:"-"`
	Cluster         envoy_cluster_v3.Cluster
}

type EnvoyRoute struct {
	Name               string      `json:"name,omitempty"`
	Namespace          string      `json:"namespace,omitempty"`
	ResourceVersion    string      `json:"resourceVersion,omitempty"`
	NodeGroup          []NodeGroup `json:"-"`
	RouteConfiguration envoy_route_v3.RouteConfiguration
}

type EnvoyListener struct {
	Name            string      `json:"name,omitempty"`
	Namespace       string      `json:"namespace,omitempty"`
	ResourceVersion string      `json:"resourceVersion,omitempty"`
	NodeGroup       []NodeGroup `json:"-"`
	Listener        envoy_listener_v3.Listener
}

type ResourceKind string

const (
	SECRET   ResourceKind = "Secret"
	ENDPOINT ResourceKind = "Endpoint"
	CLUSTER  ResourceKind = "Cluster"
	ROUTE    ResourceKind = "Route"
	LISTENER ResourceKind = "Listener"
)

type EnvoyResource struct {
	Name string
	Kind ResourceKind
}
