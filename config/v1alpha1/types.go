package v1alpha1

import (
	"time"
)

type EnvoyIngressController struct{
	Enable bool
	SyncInterval time.Duration
	//envoy related fields
	IngressSyncWorkerNumber int
	ClusterSyncWorkerNumber int
	ListenerSyncWorkerNumber int
}
