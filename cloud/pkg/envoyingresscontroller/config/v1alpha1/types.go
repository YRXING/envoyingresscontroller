package v1alpha1

import (
	envoyv2 "github.com/envoyproxy/go-control-plane/envoy/api/v2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"time"
)

type EnvoyIngressController struct {
	Enable       bool          `json:"enable"`
	SyncInterval time.Duration `json:"syncInterval"`
	//envoy related fields
	IngressSyncWorkerNumber      int `json:"ingressSyncWorkerNumber"`
	EnvoyServiceSyncWorkerNumber int `json:"envoyServiceSyncWorkerNumber"`
}

type EnvoyService struct {
	metav1.TypeMeta
	metav1.ObjectMeta
	IngressRef string
	Clusters   []envoyv2.Cluster
	Listeners  []envoyv2.Listener
}
