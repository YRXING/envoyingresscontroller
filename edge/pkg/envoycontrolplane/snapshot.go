package envoycontrolplane

import (
	envoy_types "github.com/envoyproxy/go-control-plane/pkg/cache/types"
	envoy_cache_v3 "github.com/envoyproxy/go-control-plane/pkg/cache/v3"
)

type Snapshotter interface {
	envoy_cache_v3.SnapshotCache
	Generate(version string, resources map[envoy_types.ResponseType][]envoy_types.Resource, nodeName string) error
}

type snapshotter struct {
	envoy_cache_v3.SnapshotCache
}

// Generate freshes snapshotter
func (s *snapshotter) Generate(version string, resources map[envoy_types.ResponseType][]envoy_types.Resource, nodeName string) error {
	// Create a snapshot with all xDS resources.
	snapshot := envoy_cache_v3.NewSnapshot(
		version,
		resources[envoy_types.Endpoint],
		resources[envoy_types.Cluster],
		resources[envoy_types.Route],
		resources[envoy_types.Listener],
		nil,
		resources[envoy_types.Secret],
	)

	return s.SetSnapshot(nodeName, snapshot)
}
