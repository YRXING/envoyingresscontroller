package envoycontrolplane

import (
	"fmt"
	"testing"
	"time"

	"github.com/golang/protobuf/proto"

	"github.com/kubeedge/kubeedge/edge/pkg/common/dbm"

	controller "github.com/kubeedge/kubeedge/cloud/pkg/edgecontroller/constants"
	"github.com/kubeedge/kubeedge/common/constants"
	"github.com/kubeedge/kubeedge/edge/pkg/envoycontrolplane/dao"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	v1 "k8s.io/api/core/v1"

	"github.com/envoyproxy/go-control-plane/pkg/resource/v3"

	envoy_core_v3 "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	listener "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	route "github.com/envoyproxy/go-control-plane/envoy/config/route/v3"
	hcm "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/http_connection_manager/v3"
	envoy_tls_v3 "github.com/envoyproxy/go-control-plane/envoy/extensions/transport_sockets/tls/v3"
	"github.com/envoyproxy/go-control-plane/pkg/wellknown"

	core "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	endpoint "github.com/envoyproxy/go-control-plane/envoy/config/endpoint/v3"

	"github.com/kubeedge/kubeedge/cloud/pkg/envoyingresscontroller"

	"github.com/golang/protobuf/ptypes"

	cluster "github.com/envoyproxy/go-control-plane/envoy/config/cluster/v3"
)

const (
	ListenerPort = 10000
	UpstreamHost = "www.envoyproxy.io"
)

func newEnvoySecret(secretName string) *EnvoySecret {
	secret := newSecret(secretName, "cert", "key")
	return &EnvoySecret{
		Name:      secretName,
		Namespace: "default",
		Secret:    makeSecret(secret),
	}
}

func newSecret(name, cert, key string) *v1.Secret {
	return &v1.Secret{
		TypeMeta: metav1.TypeMeta{APIVersion: "v1"},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: metav1.NamespaceDefault,
		},
		StringData: map[string]string{
			v1.TLSCertKey:       cert,
			v1.TLSPrivateKeyKey: key,
		},
		Type: v1.SecretTypeTLS,
	}
}

func makeSecret(s *v1.Secret) envoy_tls_v3.Secret {
	return envoy_tls_v3.Secret{
		Name: envoyingresscontroller.Secretname(s),
		Type: &envoy_tls_v3.Secret_TlsCertificate{
			TlsCertificate: &envoy_tls_v3.TlsCertificate{
				PrivateKey: &envoy_core_v3.DataSource{
					Specifier: &envoy_core_v3.DataSource_InlineBytes{
						InlineBytes: s.Data[v1.TLSPrivateKeyKey],
					},
				},
				CertificateChain: &envoy_core_v3.DataSource{
					Specifier: &envoy_core_v3.DataSource_InlineBytes{
						InlineBytes: s.Data[v1.TLSCertKey],
					},
				},
			},
		},
	}
}

func newEnvoyEndpoint(endpointName, address string, port uint32) *EnvoyEndpoint {
	endpoint := makeEndpoint(endpointName, address, port)
	return &EnvoyEndpoint{
		Name:                  endpointName,
		Namespace:             "default",
		ClusterLoadAssignment: *endpoint,
	}
}

func makeEndpoint(endpointName, address string, port uint32) *endpoint.ClusterLoadAssignment {
	return &endpoint.ClusterLoadAssignment{
		ClusterName: endpointName,
		Endpoints: []*endpoint.LocalityLbEndpoints{{
			LbEndpoints: []*endpoint.LbEndpoint{{
				HostIdentifier: &endpoint.LbEndpoint_Endpoint{
					Endpoint: &endpoint.Endpoint{
						Address: &core.Address{
							Address: &core.Address_SocketAddress{
								SocketAddress: &core.SocketAddress{
									Protocol: core.SocketAddress_TCP,
									Address:  address,
									PortSpecifier: &core.SocketAddress_PortValue{
										PortValue: port,
									},
								},
							},
						},
					},
				},
			}},
		}},
	}
}

func newEnvoyCluster(clusterName, address string, port uint32) *EnvoyCluster {
	cluster := cluster.Cluster{
		Name:                 clusterName,
		ConnectTimeout:       ptypes.DurationProto(5 * time.Second),
		ClusterDiscoveryType: &cluster.Cluster_Type{Type: cluster.Cluster_LOGICAL_DNS},
		LbPolicy:             cluster.Cluster_ROUND_ROBIN,
		LoadAssignment:       makeEndpoint(clusterName, address, port),
		DnsLookupFamily:      cluster.Cluster_V4_ONLY,
	}
	return &EnvoyCluster{
		Name:      clusterName,
		Namespace: "default",
		Cluster:   cluster,
	}
}

func newEnvoyRoute(routeName string, clusterName string) *EnvoyRoute {
	route := makeRoute(routeName, clusterName)
	return &EnvoyRoute{
		Name:               routeName,
		Namespace:          "default",
		RouteConfiguration: *route,
	}
}

func newEnvoyListener(listenerName string, route string) *EnvoyListener {
	listener := makeHTTPListener(listenerName, route)
	return &EnvoyListener{
		Name:      listenerName,
		Namespace: "default",
		Listener:  *listener,
	}
}

func makeRoute(routeName string, clusterName string) *route.RouteConfiguration {
	return &route.RouteConfiguration{
		Name: routeName,
		VirtualHosts: []*route.VirtualHost{{
			Name:    "local_service",
			Domains: []string{"*"},
			Routes: []*route.Route{{
				Match: &route.RouteMatch{
					PathSpecifier: &route.RouteMatch_Prefix{
						Prefix: "/",
					},
				},
				Action: &route.Route_Route{
					Route: &route.RouteAction{
						ClusterSpecifier: &route.RouteAction_Cluster{
							Cluster: clusterName,
						},
						HostRewriteSpecifier: &route.RouteAction_HostRewriteLiteral{
							HostRewriteLiteral: UpstreamHost,
						},
					},
				},
			}},
		}},
	}
}

func makeHTTPListener(listenerName string, route string) *listener.Listener {
	// HTTP filter configuration
	manager := &hcm.HttpConnectionManager{
		CodecType:  hcm.HttpConnectionManager_AUTO,
		StatPrefix: "http",
		RouteSpecifier: &hcm.HttpConnectionManager_Rds{
			Rds: &hcm.Rds{
				ConfigSource:    makeConfigSource(),
				RouteConfigName: route,
			},
		},
		HttpFilters: []*hcm.HttpFilter{{
			Name: wellknown.Router,
		}},
	}
	pbst, err := ptypes.MarshalAny(manager)
	if err != nil {
		panic(err)
	}

	return &listener.Listener{
		Name: listenerName,
		Address: &core.Address{
			Address: &core.Address_SocketAddress{
				SocketAddress: &core.SocketAddress{
					Protocol: core.SocketAddress_TCP,
					Address:  "0.0.0.0",
					PortSpecifier: &core.SocketAddress_PortValue{
						PortValue: ListenerPort,
					},
				},
			},
		},
		FilterChains: []*listener.FilterChain{{
			Filters: []*listener.Filter{{
				Name: wellknown.HTTPConnectionManager,
				ConfigType: &listener.Filter_TypedConfig{
					TypedConfig: pbst,
				},
			}},
		}},
	}
}

func makeConfigSource() *core.ConfigSource {
	source := &core.ConfigSource{}
	source.ResourceApiVersion = resource.DefaultAPIVersion
	source.ConfigSourceSpecifier = &core.ConfigSource_ApiConfigSource{
		ApiConfigSource: &core.ApiConfigSource{
			TransportApiVersion:       resource.DefaultAPIVersion,
			ApiType:                   core.ApiConfigSource_GRPC,
			SetNodeOnFirstMessageOnly: true,
			GrpcServices: []*core.GrpcService{{
				TargetSpecifier: &core.GrpcService_EnvoyGrpc_{
					EnvoyGrpc: &core.GrpcService_EnvoyGrpc{ClusterName: "xds_cluster"},
				},
			}},
		},
	}
	return source
}

// BuildResource return a string as "beehive/pkg/core/model".Message.Router.Resource
func BuildResource(nodeID, namespace, resourceType, resourceID string) (resource string, err error) {
	if namespace == "" || resourceType == "" || nodeID == "" {
		err = fmt.Errorf("required parameter are not set (node id, namespace or resource type)")
		return
	}

	resource = fmt.Sprintf("%s%s%s%s%s%s%s", controller.ResourceNode, constants.ResourceSep, nodeID, constants.ResourceSep, namespace, constants.ResourceSep, resourceType)
	if resourceID != "" {
		resource += fmt.Sprintf("%s%s", constants.ResourceSep, resourceID)
	}
	return
}

func TestProcessInsert(t *testing.T) {
	ecp := newControlPlane(true, "127.0.0.1", "18000", "node-0")
	dao.InitDBTable(ecp)
	dbm.InitDBConfig("sqlite3", "default", "/home/hoshino/edgecore.db")
	secret := newEnvoySecret("testsecret")
	//resource, err := BuildResource("node-0", secret.Namespace, string(SECRET), secret.Name)
	//if err != nil {
	//	t.Fatalf("built message resource failed with error: %s", err)
	//}
	//if err != nil {
	//	t.Fatalf("failed to marshal secret into json, err: %v", err)
	//}
	v, err := proto.Marshal(&secret.Secret)
	if err != nil {
		t.Fatalf("")
	}
	var newSecret = &envoy_tls_v3.Secret{}
	err = proto.Unmarshal(v, newSecret)
	if true {
		t.Fatalf("")
	}
	//msg := model.NewMessage("").SetResourceVersion(secret.ResourceVersion).
	//	BuildRouter("envoycontrolplane", "envoycontrolplane", resource, model.InsertOperation).
	//	FillBody(secretJson)
	//err = ecp.processInsert(*msg)
	//if err != nil {
	//	t.Fatalf("insert secret into database failed with error: %s", err)
	//}
	//secrets, err := dao.QuerySecret("testsecret", "test")
	//if err != nil {
	//	t.Fatalf("failed to query secrets from database, err: %v", err)
	//}
	//if len(*secrets) == 0 {
	//	t.Fatalf("failed to query secrets from database, got empty secret slice")
	//}
}

func TestProcessDelete(t *testing.T) {}
