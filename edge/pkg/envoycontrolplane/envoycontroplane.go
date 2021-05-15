// Copyright Project Contour Authors
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package envoycontrolplane

import (
	"fmt"
	"net"
	"reflect"
	"sync"

	"github.com/kubeedge/kubeedge/edge/pkg/envoycontrolplane/dao"

	"k8s.io/apimachinery/pkg/util/wait"

	envoy_types "github.com/envoyproxy/go-control-plane/pkg/cache/types"

	envoy_service_cluster_v3 "github.com/envoyproxy/go-control-plane/envoy/service/cluster/v3"
	envoy_service_discovery_v3 "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"
	envoy_service_endpoint_v3 "github.com/envoyproxy/go-control-plane/envoy/service/endpoint/v3"
	envoy_service_listener_v3 "github.com/envoyproxy/go-control-plane/envoy/service/listener/v3"
	envoy_service_route_v3 "github.com/envoyproxy/go-control-plane/envoy/service/route/v3"
	envoy_service_secret_v3 "github.com/envoyproxy/go-control-plane/envoy/service/secret/v3"
	envoy_cache_v3 "github.com/envoyproxy/go-control-plane/pkg/cache/v3"
	envoy_server_v3 "github.com/envoyproxy/go-control-plane/pkg/server/v3"
	"github.com/kubeedge/beehive/pkg/core"
	beehiveContext "github.com/kubeedge/beehive/pkg/core/context"
	"github.com/kubeedge/kubeedge/edge/pkg/common/modules"
	"github.com/kubeedge/kubeedge/pkg/apis/componentconfig/edgecore/v1alpha1"
	"github.com/sirupsen/logrus"
	log "github.com/sirupsen/logrus"
	"golang.org/x/net/context"
	"google.golang.org/grpc"
	"k8s.io/klog/v2"
)

// TODO: when restarting and disconnect from cloud, read objects from sqlite3

type envoyControlPlane struct {
	enable            bool
	envoySecrets      map[string]*EnvoySecret
	envoySecretLock   sync.RWMutex
	envoyEndpoints    map[string]*EnvoyEndpoint
	envoyEndpointLock sync.RWMutex
	envoyClusters     map[string]*EnvoyCluster
	envoyClusterLock  sync.RWMutex
	envoyRoutes       map[string]*EnvoyRoute
	envoyRouteLock    sync.RWMutex
	envoyListeners    map[string]*EnvoyListener
	envoyListenerLock sync.RWMutex
	xdsCache          snapshotter
	xdsAddr           string
	xdsPort           string
	nodeName          string
	version           int
}

func newControlPlane(enable bool, xdsAddr, xdsPort, nodeName string) *envoyControlPlane {
	return &envoyControlPlane{
		enable:         enable,
		xdsAddr:        xdsAddr,
		xdsPort:        xdsPort,
		nodeName:       nodeName,
		envoySecrets:   make(map[string]*EnvoySecret),
		envoyEndpoints: make(map[string]*EnvoyEndpoint),
		envoyClusters:  make(map[string]*EnvoyCluster),
		envoyRoutes:    make(map[string]*EnvoyRoute),
		envoyListeners: make(map[string]*EnvoyListener),
	}
}

func Register(ecpc *v1alpha1.EnvoyControlPlaneConfig) {
	controlplane := newControlPlane(ecpc.Enable, ecpc.XdsAddr, fmt.Sprintf("%d", ecpc.XdsPort), ecpc.NodeName)
	// register db table
	dao.InitDBTable(controlplane)
	core.Register(controlplane)
}

func (*envoyControlPlane) Name() string {
	return modules.EnvoyControlPlaneGroup
}

func (*envoyControlPlane) Group() string {
	return modules.EnvoyControlPlaneGroup
}

func (e *envoyControlPlane) Enable() bool {
	return e.enable
}

func (e *envoyControlPlane) Start() {
	taskCtx := context.Background()
	go func() {
		for {
			select {
			case <-beehiveContext.Done():
				klog.Warning("envoyControlPlane stop")
				return
			}
		}
	}()
	go e.runEnvoyControlPlane()
	// flush xdscache every minute
	go wait.Until(e.FlushXDSCache, 60, beehiveContext.Done())
	err := e.StartGrpcServer(taskCtx)
	if err != nil {
		klog.Error(err)
	}
}

func (e *envoyControlPlane) FlushXDSCache() {
	var (
		resources map[envoy_types.ResponseType][]envoy_types.Resource
		index     int
	)
	resources = make(map[envoy_types.ResponseType][]envoy_types.Resource)
	// secret
	e.envoySecretLock.RLock()
	resources[envoy_types.Secret] = make([]envoy_types.Resource, len(e.envoySecrets))
	index = 0
	for _, envoySecret := range e.envoySecrets {
		v := reflect.ValueOf(&envoySecret.Secret)
		resources[envoy_types.Secret][index] = v.Interface().(envoy_types.Resource)
		index++
	}
	e.envoySecretLock.RUnlock()
	// endpoint
	e.envoyEndpointLock.RLock()
	resources[envoy_types.Endpoint] = make([]envoy_types.Resource, len(e.envoyEndpoints))
	index = 0
	for _, envoyEndpoint := range e.envoyEndpoints {
		v := reflect.ValueOf(&envoyEndpoint.ClusterLoadAssignment)
		resources[envoy_types.Endpoint][index] = v.Interface().(envoy_types.Resource)
		index++
	}
	e.envoyEndpointLock.RUnlock()
	// cluster
	e.envoyClusterLock.RLock()
	resources[envoy_types.Cluster] = make([]envoy_types.Resource, len(e.envoyClusters))
	index = 0
	for _, envoyCluster := range e.envoyClusters {
		v := reflect.ValueOf(&envoyCluster.Cluster)
		resources[envoy_types.Cluster][index] = v.Interface().(envoy_types.Resource)
		index++
	}
	e.envoyClusterLock.RUnlock()
	// route
	e.envoyRouteLock.RLock()
	resources[envoy_types.Route] = make([]envoy_types.Resource, len(e.envoyRoutes))
	index = 0
	for _, envoyRoute := range e.envoyRoutes {
		v := reflect.ValueOf(&envoyRoute.RouteConfiguration)
		resources[envoy_types.Route][index] = v.Interface().(envoy_types.Resource)
		index++
	}
	e.envoyRouteLock.RUnlock()
	// listener
	e.envoyListenerLock.RLock()
	resources[envoy_types.Listener] = make([]envoy_types.Resource, len(e.envoyListeners))
	index = 0
	for _, envoyListener := range e.envoyListeners {
		v := reflect.ValueOf(&envoyListener.Listener)
		resources[envoy_types.Listener][index] = v.Interface().(envoy_types.Resource)
		index++
	}
	e.envoyListenerLock.RUnlock()
	e.version++
	err := e.xdsCache.Generate(fmt.Sprintf("%d", e.version), resources, e.nodeName)
	if err != nil {
		klog.Error(err)
	}
}

func (e *envoyControlPlane) StartGrpcServer(taskCtx context.Context) error {
	var (
		ads = true
	)
	log := log.WithField("context", "xds")
	grpcServer := grpc.NewServer()
	e.xdsCache.SnapshotCache = envoy_cache_v3.NewSnapshotCache(ads, &envoy_cache_v3.IDHash{}, log)
	srv := envoy_server_v3.NewServer(taskCtx, e.xdsCache, NewRequestLoggingCallbacks(log))
	// register services
	envoy_service_discovery_v3.RegisterAggregatedDiscoveryServiceServer(grpcServer, srv)
	envoy_service_secret_v3.RegisterSecretDiscoveryServiceServer(grpcServer, srv)
	envoy_service_cluster_v3.RegisterClusterDiscoveryServiceServer(grpcServer, srv)
	envoy_service_endpoint_v3.RegisterEndpointDiscoveryServiceServer(grpcServer, srv)
	envoy_service_listener_v3.RegisterListenerDiscoveryServiceServer(grpcServer, srv)
	envoy_service_route_v3.RegisterRouteDiscoveryServiceServer(grpcServer, srv)

	addr := net.JoinHostPort(e.xdsAddr, e.xdsPort)
	l, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}

	go func() {
		<-taskCtx.Done()

		// We don't use GracefulStop here because envoy
		// has long-lived hanging xDS requests. There's no
		// mechanism to make those pending requests fail,
		// so we forcibly terminate the TCP sessions.
		grpcServer.Stop()
	}()

	err = grpcServer.Serve(l)
	if err != nil {
		return err
	}
	return nil
}

func NewRequestLoggingCallbacks(log logrus.FieldLogger) envoy_server_v3.Callbacks {
	return &envoy_server_v3.CallbackFuncs{
		StreamRequestFunc: func(streamID int64, req *envoy_service_discovery_v3.DiscoveryRequest) error {
			logDiscoveryRequestDetails(log, req)
			return nil
		},
	}
}

func logDiscoveryRequestDetails(l logrus.FieldLogger, req *envoy_service_discovery_v3.DiscoveryRequest) *logrus.Entry {
	log := l.WithField("version_info", req.VersionInfo).WithField("response_nonce", req.ResponseNonce)
	if req.Node != nil {
		log = log.WithField("node_id", req.Node.Id)

		if bv := req.Node.GetUserAgentBuildVersion(); bv != nil && bv.Version != nil {
			log = log.WithField("node_version", fmt.Sprintf("v%d.%d.%d", bv.Version.MajorNumber, bv.Version.MinorNumber, bv.Version.Patch))
		}
	}

	if status := req.ErrorDetail; status != nil {
		// if Envoy rejected the last update log the details here.
		// TODO(dfc) issue 1176: handle xDS ACK/NACK
		log.WithField("code", status.Code).Error(status.Message)
	}

	log = log.WithField("resource_names", req.ResourceNames).WithField("type_url", req.GetTypeUrl())

	log.Debug("handling v3 xDS resource request")

	return log
}
