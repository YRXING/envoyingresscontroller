package types

import (
	envoyv2 "github.com/envoyproxy/go-control-plane/envoy/api/v2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)



type EnvoyService struct {
	metav1.TypeMeta
	metav1.ObjectMeta
	IngressRef string
	Clusters   []envoyv2.Cluster
	Listeners  []envoyv2.Listener
}
