package types

import (
envoyv2 "github.com/envoyproxy/go-control-plane/envoy/api/v2"
metaconfig "github.com/kubeedge/kubeedge/pkg/apis/componentconfig/meta/v1alpha1"
metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
"time"
)


// EnvoyIngressController indicates the config of EnvoyIngressController module
type EnvoyIngressControllerConfiguration struct {
	// Enable indicates whether EnvoyIngressController is enabled,
	// if set to false (for debugging etc.), skip checking other EnvoyIngressController configs.
	// default true
	Enable bool `json:"enable,omitempty"`
	// NodeUpdateFrequency indicates node update frequency (second)
	// default 10
	NodeUpdateFrequency int32 `json:"nodeUpdateFrequency,omitempty"`
	// Buffer indicates k8s resource buffer
	Buffer *EnvoyIngressControllerBuffer `json:"buffer,omitempty"`
	// Context indicates send,receive,response modules for EnvoyIngressController module
	Context *ControllerContext `json:"context,omitempty"`
	// Load indicates EnvoyIngressController load
	Load *EnvoyIngressControllerLoad `json:"load,omitempty"`

	SyncInterval time.Duration `json:"syncInterval"`
	//envoy related fields
	IngressSyncWorkerNumber      int `json:"ingressSyncWorkerNumber"`
	EnvoyServiceSyncWorkerNumber int `json:"envoyServiceSyncWorkerNumber"`
}

// EnvoyIngressControllerBuffer indicates the EnvoyIngressController buffer
type EnvoyIngressControllerBuffer struct {
	// UpdatePodStatus indicates the buffer of pod status
	// default 1024
	UpdatePodStatus int32 `json:"updatePodStatus,omitempty"`
	// UpdateNodeStatus indicates the buffer of update node status
	// default 1024
	UpdateNodeStatus int32 `json:"updateNodeStatus,omitempty"`
	// QueryConfigMap indicates the buffer of query configMap
	// default 1024
	QueryConfigMap int32 `json:"queryConfigMap,omitempty"`
	// QuerySecret indicates the buffer of query secret
	// default 1024
	QuerySecret int32 `json:"querySecret,omitempty"`
	// QueryService indicates the buffer of query service
	// default 1024
	QueryService int32 `json:"queryService,omitempty"`
	// QueryEndpoints indicates the buffer of query endpoint
	// default 1024
	QueryEndpoints int32 `json:"queryEndpoints,omitempty"`
	// PodEvent indicates the buffer of pod event
	// default 1
	PodEvent int32 `json:"podEvent,omitempty"`
	// ConfigMapEvent indicates the buffer of configMap event
	// default 1
	ConfigMapEvent int32 `json:"configMapEvent,omitempty"`
	// SecretEvent indicates the buffer of secret event
	// default 1
	SecretEvent int32 `json:"secretEvent,omitempty"`
	// ServiceEvent indicates the buffer of service event
	// default 1
	ServiceEvent int32 `json:"serviceEvent,omitempty"`
	// EndpointsEvent indicates the buffer of endpoint event
	// default 1
	EndpointsEvent int32 `json:"endpointsEvent,omitempty"`
	// RulesEvent indicates the buffer of rule event
	// default 1
	RulesEvent int32 `json:"rulesEvent,omitempty"`
	// RuleEndpointsEvent indicates the buffer of endpoint event
	// default 1
	RuleEndpointsEvent int32 `json:"ruleEndpointsEvent,omitempty"`
	// QueryPersistentVolume indicates the buffer of query persistent volume
	// default 1024
	QueryPersistentVolume int32 `json:"queryPersistentVolume,omitempty"`
	// QueryPersistentVolumeClaim indicates the buffer of query persistent volume claim
	// default 1024
	QueryPersistentVolumeClaim int32 `json:"queryPersistentVolumeClaim,omitempty"`
	// QueryVolumeAttachment indicates the buffer of query volume attachment
	// default 1024
	QueryVolumeAttachment int32 `json:"queryVolumeAttachment,omitempty"`
	// QueryNode indicates the buffer of query node
	// default 1024
	QueryNode int32 `json:"queryNode,omitempty"`
	// UpdateNode indicates the buffer of update node
	// default 1024
	UpdateNode int32 `json:"updateNode,omitempty"`
	// DeletePod indicates the buffer of delete pod message from edge
	// default 1024
	DeletePod int32 `json:"deletePod,omitempty"`
}

// ControllerContext indicates the message layer context for all controllers
type ControllerContext struct {
	// SendModule indicates which module will send message to
	SendModule metaconfig.ModuleName `json:"sendModule,omitempty"`
	// SendRouterModule indicates which module will send router message to
	SendRouterModule metaconfig.ModuleName `json:"sendRouterModule,omitempty"`
	// ReceiveModule indicates which module will receive message from
	ReceiveModule metaconfig.ModuleName `json:"receiveModule,omitempty"`
	// ResponseModule indicates which module will response message to
	ResponseModule metaconfig.ModuleName `json:"responseModule,omitempty"`
}

// EnvoyIngressControllerLoad indicates the EnvoyIngressController load
type EnvoyIngressControllerLoad struct {
	// UpdatePodStatusWorkers indicates the load of update pod status workers
	// default 1
	UpdatePodStatusWorkers int32 `json:"updatePodStatusWorkers,omitempty"`
	// UpdateNodeStatusWorkers indicates the load of update node status workers
	// default 1
	UpdateNodeStatusWorkers int32 `json:"updateNodeStatusWorkers,omitempty"`
	// QueryConfigMapWorkers indicates the load of query config map workers
	// default 4
	QueryConfigMapWorkers int32 `json:"queryConfigMapWorkers,omitempty"`
	// QuerySecretWorkers indicates the load of query secret workers
	// default 4
	QuerySecretWorkers int32 `json:"querySecretWorkers,omitempty"`
	// QueryServiceWorkers indicates the load of query service workers
	// default 4
	QueryServiceWorkers int32 `json:"queryServiceWorkers,omitempty"`
	// QueryEndpointsWorkers indicates the load of query endpoint workers
	// default 4
	QueryEndpointsWorkers int32 `json:"queryEndpointsWorkers,omitempty"`
	// QueryPersistentVolumeWorkers indicates the load of query persistent volume workers
	// default 4
	QueryPersistentVolumeWorkers int32 `json:"queryPersistentVolumeWorkers,omitempty"`
	// QueryPersistentVolumeClaimWorkers indicates the load of query persistent volume claim workers
	// default 4
	QueryPersistentVolumeClaimWorkers int32 `json:"queryPersistentVolumeClaimWorkers,omitempty"`
	// QueryVolumeAttachmentWorkers indicates the load of query volume attachment workers
	// default 4
	QueryVolumeAttachmentWorkers int32 `json:"queryVolumeAttachmentWorkers,omitempty"`
	// QueryNodeWorkers indicates the load of query node workers
	// default 4
	QueryNodeWorkers int32 `json:"queryNodeWorkers,omitempty"`
	// UpdateNodeWorkers indicates the load of update node workers
	// default 4
	UpdateNodeWorkers int32 `json:"updateNodeWorkers,omitempty"`
	// DeletePodWorkers indicates the load of delete pod workers
	// default 4
	DeletePodWorkers int32 `json:"deletePodWorkers,omitempty"`
}

type EnvoyService struct {
	metav1.TypeMeta
	metav1.ObjectMeta
	IngressRef string
	Clusters   []envoyv2.Cluster
	Listeners  []envoyv2.Listener
}
