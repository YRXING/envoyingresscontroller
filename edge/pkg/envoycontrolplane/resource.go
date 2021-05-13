package envoycontrolplane

import (
	envoy_cluster_v3 "github.com/envoyproxy/go-control-plane/envoy/config/cluster/v3"
	envoy_endpoint_v3 "github.com/envoyproxy/go-control-plane/envoy/config/endpoint/v3"
	envoy_listener_v3 "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	envoy_route_v3 "github.com/envoyproxy/go-control-plane/envoy/config/route/v3"
	envoy_tls_v3 "github.com/envoyproxy/go-control-plane/envoy/extensions/transport_sockets/tls/v3"
)

type EnvoyResourceInterface interface {
	GetName() string
	GetNamespace() string
	SetName(string)
	SetNamespace(string)
}

type NodeGroup string

type EnvoySecret struct {
	Name            string              `json:"name,omitempty"`
	Namespace       string              `json:"namespace,omitempty"`
	ResourceVersion string              `json:"resourceVersion,omitempty"`
	NodeGroup       []NodeGroup         `json:"-"`
	Secret          envoy_tls_v3.Secret `json:"secret,omitempty"`
}

func (envoySecret *EnvoySecret) GetName() string {
	return envoySecret.Name
}

func (envoySecret *EnvoySecret) GetNamespace() string {
	return envoySecret.Namespace
}

func (envoySecret *EnvoySecret) SetName(name string) {
	envoySecret.Name = name
}

func (envoySecret *EnvoySecret) SetNamespace(namespace string) {
	envoySecret.Namespace = namespace
}

type EnvoyEndpoint struct {
	Name                  string      `json:"name,omitempty"`
	Namespace             string      `json:"namespace,omitempty"`
	ResourceVersion       string      `json:"resourceVersion,omitempty"`
	NodeGroup             []NodeGroup `json:"-"`
	ClusterLoadAssignment envoy_endpoint_v3.ClusterLoadAssignment
}

func (envoyEndpoint *EnvoyEndpoint) GetName() string {
	return envoyEndpoint.Name
}

func (envoyEndpoint *EnvoyEndpoint) GetNamespace() string {
	return envoyEndpoint.Namespace
}

func (envoyEndpoint *EnvoyEndpoint) SetName(name string) {
	envoyEndpoint.Name = name
}

func (envoyEndpoint *EnvoyEndpoint) SetNamespace(namespace string) {
	envoyEndpoint.Namespace = namespace
}

type EnvoyCluster struct {
	Name            string      `json:"name,omitempty"`
	Namespace       string      `json:"namespace,omitempty"`
	ResourceVersion string      `json:"resourceVersion,omitempty"`
	NodeGroup       []NodeGroup `json:"-"`
	Cluster         envoy_cluster_v3.Cluster
}

func (envoyCluster *EnvoyCluster) GetName() string {
	return envoyCluster.Name
}

func (envoyCluster *EnvoyCluster) GetNamespace() string {
	return envoyCluster.Namespace
}

func (envoyCluster *EnvoyCluster) SetName(name string) {
	envoyCluster.Name = name
}

func (envoyCluster *EnvoyCluster) SetNamespace(namespace string) {
	envoyCluster.Namespace = namespace
}

type EnvoyRoute struct {
	Name               string      `json:"name,omitempty"`
	Namespace          string      `json:"namespace,omitempty"`
	ResourceVersion    string      `json:"resourceVersion,omitempty"`
	NodeGroup          []NodeGroup `json:"-"`
	RouteConfiguration envoy_route_v3.RouteConfiguration
}

func (envoyRoute *EnvoyRoute) GetName() string {
	return envoyRoute.Name
}

func (envoyRoute *EnvoyRoute) GetNamespace() string {
	return envoyRoute.Namespace
}

func (envoyRoute *EnvoyRoute) SetName(name string) {
	envoyRoute.Name = name
}

func (envoyRoute *EnvoyRoute) SetNamespace(namespace string) {
	envoyRoute.Namespace = namespace
}

type EnvoyListener struct {
	Name            string      `json:"name,omitempty"`
	Namespace       string      `json:"namespace,omitempty"`
	ResourceVersion string      `json:"resourceVersion,omitempty"`
	NodeGroup       []NodeGroup `json:"-"`
	Listener        envoy_listener_v3.Listener
}

func (envoyListener *EnvoyListener) GetName() string {
	return envoyListener.Name
}

func (envoyListener *EnvoyListener) GetNamespace() string {
	return envoyListener.Namespace
}

func (envoyListener *EnvoyListener) SetName(name string) {
	envoyListener.Name = name
}

func (envoyListener *EnvoyListener) SetNamespace(namespace string) {
	envoyListener.Namespace = namespace
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
