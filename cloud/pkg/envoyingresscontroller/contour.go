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

package envoyingresscontroller

import (
	"crypto/sha1"
	"crypto/sha256"
	"fmt"
	"strconv"
	"strings"
	"time"

	envoy_cluster_v3 "github.com/envoyproxy/go-control-plane/envoy/config/cluster/v3"
	envoy_api_v3_core "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	envoy_core_v3 "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	envoy_endpoint_v3 "github.com/envoyproxy/go-control-plane/envoy/config/endpoint/v3"
	envoy_listener_v3 "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	envoy_route_v3 "github.com/envoyproxy/go-control-plane/envoy/config/route/v3"
	envoy_tls_v3 "github.com/envoyproxy/go-control-plane/envoy/extensions/transport_sockets/tls/v3"
	envoy_extensions_upstream_http_v3 "github.com/envoyproxy/go-control-plane/envoy/extensions/upstreams/http/v3"
	matcher "github.com/envoyproxy/go-control-plane/envoy/type/matcher/v3"
	envoy_type "github.com/envoyproxy/go-control-plane/envoy/type/v3"
	"github.com/golang/protobuf/proto"
	"github.com/golang/protobuf/ptypes"
	"github.com/golang/protobuf/ptypes/any"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/wrapperspb"
	"k8s.io/apimachinery/pkg/types"

	envoy_v3_tls "github.com/envoyproxy/go-control-plane/envoy/extensions/transport_sockets/tls/v3"
	v1 "k8s.io/api/core/v1"
)

// PeerValidationContext defines how to validate the certificate on the upstream service.
type PeerValidationContext struct {
	// CACertificate holds a reference to the Secret containing the CA to be used to
	// verify the upstream connection.
	CACertificate *v1.Secret
	// SubjectName holds an optional subject name which Envoy will check against the
	// certificate presented by the upstream.
	SubjectName string
}

// GetCACertificate returns the CA certificate from PeerValidationContext.
func (pvc *PeerValidationContext) GetCACertificate() []byte {
	if pvc == nil || pvc.CACertificate == nil {
		// No validation required.
		return nil
	}
	return pvc.CACertificate.Data[CACertificateKey]
}

// GetSubjectName returns the SubjectName from PeerValidationContext.
func (pvc *PeerValidationContext) GetSubjectName() string {
	if pvc == nil {
		// No validation required.
		return ""
	}
	return pvc.SubjectName
}

// HTTPHealthCheckPolicy http health check policy
type HTTPHealthCheckPolicy struct {
	Path               string
	Host               string
	Interval           time.Duration
	Timeout            time.Duration
	UnhealthyThreshold uint32
	HealthyThreshold   uint32
}
type MatchCondition interface {
	fmt.Stringer
}

// PrefixMatchType represents different types of prefix matching alternatives.
type PrefixMatchType int

const (
	// PrefixMatchString represents a prefix match that functions like a
	// string prefix match, i.e. prefix /foo matches /foobar
	PrefixMatchString PrefixMatchType = iota
	// PrefixMatchSegment represents a prefix match that only matches full path
	// segments, i.e. prefix /foo matches /foo/bar but not /foobar
	PrefixMatchSegment
)

var prefixMatchTypeToName = map[PrefixMatchType]string{
	PrefixMatchString:  "string",
	PrefixMatchSegment: "segment",
}

// PrefixMatchCondition matches the start of a URL.
type PrefixMatchCondition struct {
	Prefix          string
	PrefixMatchType PrefixMatchType
}

func (ec *ExactMatchCondition) String() string {
	return "exact: " + ec.Path
}

// ExactMatchCondition matches the entire path of a URL.
type ExactMatchCondition struct {
	Path string
}

func (pc *PrefixMatchCondition) String() string {
	str := "prefix: " + pc.Prefix
	if typeStr, ok := prefixMatchTypeToName[pc.PrefixMatchType]; ok {
		str += " type: " + typeStr
	}
	return str
}

// RegexMatchCondition matches the URL by regular expression.
type RegexMatchCondition struct {
	Regex string
}

func (rc *RegexMatchCondition) String() string {
	return "regex: " + rc.Regex
}

// Secret creates new envoy_tls_v3.Secret from secret.
func Secret(s *v1.Secret) *envoy_tls_v3.Secret {
	return &envoy_tls_v3.Secret{
		Name: Secretname(s),
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

// Secretname returns the name of the SDS secret for this secret.
func Secretname(s *v1.Secret) string {
	// This isn't a crypto hash, we just want a unique name.
	hash := sha1.Sum(s.Data[v1.TLSCertKey]) // nolint:gosec
	ns := s.Namespace
	name := s.Name
	return Hashname(60, ns, name, fmt.Sprintf("%x", hash[:5]))
}

// Hashname takes a length l and a varargs of strings s and returns a string whose length
// which does not exceed l. Internally s is joined with strings.Join(s, "/"). If the
// combined length exceeds l then hashname truncates each element in s, starting from the
// end using a hash derived from the contents of s (not the current element). This process
// continues until the length of s does not exceed l, or all elements have been truncated.
// In which case, the entire string is replaced with a hash not exceeding the length of l.
func Hashname(l int, s ...string) string {
	const shorthash = 6 // the length of the shorthash

	r := strings.Join(s, "/")
	if l > len(r) {
		// we're under the limit, nothing to do
		return r
	}
	hash := fmt.Sprintf("%x", sha256.Sum256([]byte(r)))
	for n := len(s) - 1; n >= 0; n-- {
		s[n] = truncate(l/len(s), s[n], hash[:shorthash])
		r = strings.Join(s, "/")
		if l > len(r) {
			return r
		}
	}
	// truncated everything, but we're still too long
	// just return the hash truncated to l.
	return hash[:min(len(hash), l)]
}

// truncate truncates s to l length by replacing the
// end of s with -suffix.
func truncate(l int, s, suffix string) string {
	if l >= len(s) {
		// under the limit, nothing to do
		return s
	}
	if l <= len(suffix) {
		// easy case, just return the start of the suffix
		return suffix[:min(l, len(suffix))]
	}
	return s[:l-len(suffix)-1] + "-" + suffix
}

func min(a, b int) int {
	if a > b {
		return b
	}
	return a
}

func LBEndpoint(addr *envoy_core_v3.Address) *envoy_endpoint_v3.LbEndpoint {
	return &envoy_endpoint_v3.LbEndpoint{
		HostIdentifier: &envoy_endpoint_v3.LbEndpoint_Endpoint{
			Endpoint: &envoy_endpoint_v3.Endpoint{
				Address: addr,
			},
		},
	}
}

// SocketAddress creates a new TCP envoy_core_v3.Address.
func SocketAddress(address string, port int) *envoy_core_v3.Address {
	if address == "::" {
		return &envoy_core_v3.Address{
			Address: &envoy_core_v3.Address_SocketAddress{
				SocketAddress: &envoy_core_v3.SocketAddress{
					Protocol:   envoy_core_v3.SocketAddress_TCP,
					Address:    address,
					Ipv4Compat: true,
					PortSpecifier: &envoy_core_v3.SocketAddress_PortValue{
						PortValue: uint32(port),
					},
				},
			},
		}
	}
	return &envoy_core_v3.Address{
		Address: &envoy_core_v3.Address_SocketAddress{
			SocketAddress: &envoy_core_v3.SocketAddress{
				Protocol: envoy_core_v3.SocketAddress_TCP,
				Address:  address,
				PortSpecifier: &envoy_core_v3.SocketAddress_PortValue{
					PortValue: uint32(port),
				},
			},
		},
	}
}

// UpstreamTLSContext creates an envoy_v3_tls.UpstreamTlsContext. By default
// UpstreamTLSContext returns a HTTP/1.1 TLS enabled context. A list of
// additional ALPN protocols can be provided.
func UpstreamTLSContext(peerValidationContext *PeerValidationContext, sni string, clientSecret *v1.Secret, alpnProtocols ...string) *envoy_v3_tls.UpstreamTlsContext {
	var clientSecretConfigs []*envoy_v3_tls.SdsSecretConfig
	if clientSecret != nil {
		clientSecretConfigs = []*envoy_v3_tls.SdsSecretConfig{{
			Name:      Secretname(clientSecret),
			SdsConfig: ConfigSource("envoyingresscontroller"),
		}}
	}

	context := &envoy_v3_tls.UpstreamTlsContext{
		CommonTlsContext: &envoy_v3_tls.CommonTlsContext{
			AlpnProtocols:                  alpnProtocols,
			TlsCertificateSdsSecretConfigs: clientSecretConfigs,
		},
		Sni: sni,
	}

	if peerValidationContext.GetCACertificate() != nil && len(peerValidationContext.GetSubjectName()) > 0 {
		// We have to explicitly assign the value from validationContext
		// to context.CommonTlsContext.ValidationContextType because the
		// latter is an interface. Returning nil from validationContext
		// directly into this field boxes the nil into the unexported
		// type of this grpc OneOf field which causes proto marshaling
		// to explode later on.
		vc := validationContext(peerValidationContext.GetCACertificate(), peerValidationContext.GetSubjectName())
		if vc != nil {
			context.CommonTlsContext.ValidationContextType = vc
		}
	}

	return context
}

func validationContext(ca []byte, subjectName string) *envoy_v3_tls.CommonTlsContext_ValidationContext {
	vc := &envoy_v3_tls.CommonTlsContext_ValidationContext{
		ValidationContext: &envoy_v3_tls.CertificateValidationContext{
			TrustedCa: &envoy_api_v3_core.DataSource{
				// TODO(dfc) update this for SDSSDS
				Specifier: &envoy_api_v3_core.DataSource_InlineBytes{
					InlineBytes: ca,
				},
			},
		},
	}

	if len(subjectName) > 0 {
		vc.ValidationContext.MatchSubjectAltNames = []*matcher.StringMatcher{{
			MatchPattern: &matcher.StringMatcher_Exact{
				Exact: subjectName,
			}},
		}
	}

	return vc
}

// UpstreamTLSTransportSocket returns a custom transport socket using the UpstreamTlsContext provided.
func UpstreamTLSTransportSocket(tls *envoy_tls_v3.UpstreamTlsContext) *envoy_core_v3.TransportSocket {
	return &envoy_core_v3.TransportSocket{
		Name: "envoy.transport_sockets.tls",
		ConfigType: &envoy_core_v3.TransportSocket_TypedConfig{
			TypedConfig: MustMarshalAny(tls),
		},
	}
}

func http2ProtocolOptions() map[string]*any.Any {
	return map[string]*any.Any{
		"envoy.extensions.upstreams.http.v3.HttpProtocolOptions": MustMarshalAny(
			&envoy_extensions_upstream_http_v3.HttpProtocolOptions{
				UpstreamProtocolOptions: &envoy_extensions_upstream_http_v3.HttpProtocolOptions_ExplicitHttpConfig_{
					ExplicitHttpConfig: &envoy_extensions_upstream_http_v3.HttpProtocolOptions_ExplicitHttpConfig{
						ProtocolConfig: &envoy_extensions_upstream_http_v3.HttpProtocolOptions_ExplicitHttpConfig_Http2ProtocolOptions{},
					},
				},
			}),
	}
}

// MustMarshalAny marshals a protobug into an any.Any type, panicking
// if that operation fails.
func MustMarshalAny(pb proto.Message) *any.Any {
	a, err := ptypes.MarshalAny(pb)
	if err != nil {
		panic(err.Error())
	}

	return a
}

// StaticClusterLoadAssignment creates a *envoy_endpoint_v3.ClusterLoadAssignment pointing to the external DNS address of the service
func StaticClusterLoadAssignment(service *v1.Service, servicePort *v1.ServicePort) *envoy_endpoint_v3.ClusterLoadAssignment {
	addr := SocketAddress(service.Spec.ExternalName, int(servicePort.Port))
	return &envoy_endpoint_v3.ClusterLoadAssignment{
		Endpoints: Endpoints(addr),
		ClusterName: ClusterLoadAssignmentName(
			types.NamespacedName{Name: service.Name, Namespace: service.Namespace},
			servicePort.Name,
		),
	}
}

// Endpoints returns a slice of LocalityLbEndpoints.
// The slice contains one entry, with one LbEndpoint per
// *envoy_core_v3.Address supplied.
func Endpoints(addrs ...*envoy_core_v3.Address) []*envoy_endpoint_v3.LocalityLbEndpoints {
	lbendpoints := make([]*envoy_endpoint_v3.LbEndpoint, 0, len(addrs))
	for _, addr := range addrs {
		lbendpoints = append(lbendpoints, LBEndpoint(addr))
	}
	return []*envoy_endpoint_v3.LocalityLbEndpoints{{
		LbEndpoints: lbendpoints,
	}}
}

func edsconfig(cluster string, service *v1.Service, servicePort *v1.ServicePort) *envoy_cluster_v3.Cluster_EdsClusterConfig {
	return &envoy_cluster_v3.Cluster_EdsClusterConfig{
		EdsConfig: ConfigSource(cluster),
		ServiceName: ClusterLoadAssignmentName(
			types.NamespacedName{Name: service.Name, Namespace: service.Namespace},
			servicePort.Name,
		),
	}
}

// ClusterLoadAssignmentName generates the name used for an EDS
// ClusterLoadAssignment, given a fully qualified Service name and
// port. This name is a contract between the producer of a cluster
// (i.e. the EDS service) and the consumer of a cluster (most likely
// a HTTP Route Action).
func ClusterLoadAssignmentName(service types.NamespacedName, portName string) string {
	name := []string{
		service.Namespace,
		service.Name,
		portName,
	}

	// If the port is empty, omit it.
	if portName == "" {
		return strings.Join(name[:2], "/")
	}

	return strings.Join(name, "/")
}

// ConfigSource returns a *envoy_core_v3.ConfigSource for cluster.
func ConfigSource(cluster string) *envoy_core_v3.ConfigSource {
	return &envoy_core_v3.ConfigSource{
		ResourceApiVersion: envoy_core_v3.ApiVersion_V3,
		ConfigSourceSpecifier: &envoy_core_v3.ConfigSource_ApiConfigSource{
			ApiConfigSource: &envoy_core_v3.ApiConfigSource{
				ApiType:             envoy_core_v3.ApiConfigSource_GRPC,
				TransportApiVersion: envoy_core_v3.ApiVersion_V3,
				GrpcServices: []*envoy_core_v3.GrpcService{{
					TargetSpecifier: &envoy_core_v3.GrpcService_EnvoyGrpc_{
						EnvoyGrpc: &envoy_core_v3.GrpcService_EnvoyGrpc{
							ClusterName: cluster,
						},
					},
				}},
			},
		},
	}
}

// ClusterDiscoveryType returns the type of a ClusterDiscovery as a Cluster_type.
func ClusterDiscoveryType(t envoy_cluster_v3.Cluster_DiscoveryType) *envoy_cluster_v3.Cluster_Type {
	return &envoy_cluster_v3.Cluster_Type{Type: t}
}

// AltStatName generates an alternative stat name for the service
// using format ns_name_port
func AltStatName(service *v1.Service, servicePort *v1.ServicePort) string {
	parts := []string{service.Namespace, service.Name, strconv.Itoa(int(servicePort.Port))}
	return strings.Join(parts, "_")
}

// Clustername returns the name of the CDS cluster for this service.
func Clustername(service *v1.Service, servicePort *v1.ServicePort, loadBalancerPolicy string,
	HTTPHealthCheckPolicy *HTTPHealthCheckPolicy, UpstreamValidation *PeerValidationContext) string {
	buf := loadBalancerPolicy
	if hc := HTTPHealthCheckPolicy; hc != nil {
		if hc.Timeout > 0 {
			buf += hc.Timeout.String()
		}
		if hc.Interval > 0 {
			buf += hc.Interval.String()
		}
		if hc.UnhealthyThreshold > 0 {
			buf += strconv.Itoa(int(hc.UnhealthyThreshold))
		}
		if hc.HealthyThreshold > 0 {
			buf += strconv.Itoa(int(hc.HealthyThreshold))
		}
		buf += hc.Path
	}
	if uv := UpstreamValidation; uv != nil {
		buf += uv.CACertificate.ObjectMeta.Name
		buf += uv.SubjectName
	}

	// This isn't a crypto hash, we just want a unique name.
	hash := sha1.Sum([]byte(buf)) // nolint:gosec

	ns := service.Namespace
	name := service.Name
	return Hashname(60, ns, name, strconv.Itoa(int(servicePort.Port)), fmt.Sprintf("%x", hash[:5]))
}

func clusterDefaults() *envoy_cluster_v3.Cluster {
	return &envoy_cluster_v3.Cluster{
		ConnectTimeout: &durationpb.Duration{
			Nanos: 250000,
		},
		CommonLbConfig: ClusterCommonLBConfig(),
		LbPolicy:       lbPolicy(LoadBalancerPolicyRoundRobin),
	}
}

// ClusterCommonLBConfig creates a *envoy_cluster_v3.Cluster_CommonLbConfig with HealthyPanicThreshold disabled.
func ClusterCommonLBConfig() *envoy_cluster_v3.Cluster_CommonLbConfig {
	return &envoy_cluster_v3.Cluster_CommonLbConfig{
		HealthyPanicThreshold: &envoy_type.Percent{ // Disable HealthyPanicThreshold
			Value: 0,
		},
	}
}

func lbPolicy(strategy string) envoy_cluster_v3.Cluster_LbPolicy {
	switch strategy {
	case LoadBalancerPolicyWeightedLeastRequest:
		return envoy_cluster_v3.Cluster_LEAST_REQUEST
	case LoadBalancerPolicyRandom:
		return envoy_cluster_v3.Cluster_RANDOM
	case LoadBalancerPolicyCookie, LoadBalancerPolicyRequestHash:
		return envoy_cluster_v3.Cluster_RING_HASH
	default:
		return envoy_cluster_v3.Cluster_ROUND_ROBIN
	}
}

// RouteConfiguration returns a *envoy_route_v3.RouteConfiguration.
func RouteConfiguration(name string, virtualhosts ...*envoy_route_v3.VirtualHost) *envoy_route_v3.RouteConfiguration {
	return &envoy_route_v3.RouteConfiguration{
		Name:         name,
		VirtualHosts: virtualhosts,
		RequestHeadersToAdd: Headers(
			AppendHeader("x-request-start", "t=%START_TIME(%s.%3f)%"),
		),
	}
}

func Headers(first *envoy_core_v3.HeaderValueOption, rest ...*envoy_core_v3.HeaderValueOption) []*envoy_core_v3.HeaderValueOption {
	return append([]*envoy_core_v3.HeaderValueOption{first}, rest...)
}

func AppendHeader(key, value string) *envoy_core_v3.HeaderValueOption {
	return &envoy_core_v3.HeaderValueOption{
		Header: &envoy_core_v3.HeaderValue{
			Key:   key,
			Value: value,
		},
		Append: &wrapperspb.BoolValue{
			Value: true,
		},
	}
}

// Listener returns a new envoy_listener_v3.Listener for the supplied address, port, and filters.
func Listener(name, address string, port int, lf []*envoy_listener_v3.ListenerFilter, filters ...*envoy_listener_v3.Filter) *envoy_listener_v3.Listener {
	l := &envoy_listener_v3.Listener{
		Name:            name,
		Address:         SocketAddress(address, port),
		ListenerFilters: lf,
		SocketOptions:   TCPKeepaliveSocketOptions(),
	}
	if len(filters) > 0 {
		l.FilterChains = append(
			l.FilterChains,
			&envoy_listener_v3.FilterChain{
				Filters: filters,
			},
		)
	}
	return l
}

func TCPKeepaliveSocketOptions() []*envoy_core_v3.SocketOption {
	// Note: TCP_KEEPIDLE + (TCP_KEEPINTVL * TCP_KEEPCNT) must be greater than
	// the grpc.KeepaliveParams time + timeout (currently 60 + 20 = 80 seconds)
	// otherwise TestGRPC/StreamClusters fails.
	return []*envoy_core_v3.SocketOption{
		// Enable TCP keep-alive.
		{
			Description: "Enable TCP keep-alive",
			Level:       SOL_SOCKET,
			Name:        SO_KEEPALIVE,
			Value:       &envoy_core_v3.SocketOption_IntValue{IntValue: 1},
			State:       envoy_core_v3.SocketOption_STATE_LISTENING,
		},
		// The time (in seconds) the connection needs to remain idle
		// before TCP starts sending keepalive probes.
		{
			Description: "TCP keep-alive initial idle time",
			Level:       IPPROTO_TCP,
			Name:        TCP_KEEPIDLE,
			Value:       &envoy_core_v3.SocketOption_IntValue{IntValue: 45},
			State:       envoy_core_v3.SocketOption_STATE_LISTENING,
		},
		// The time (in seconds) between individual keepalive probes.
		{
			Description: "TCP keep-alive time between probes",
			Level:       IPPROTO_TCP,
			Name:        TCP_KEEPINTVL,
			Value:       &envoy_core_v3.SocketOption_IntValue{IntValue: 5},
			State:       envoy_core_v3.SocketOption_STATE_LISTENING,
		},
		// The maximum number of TCP keep-alive probes to send before
		// giving up and killing the connection if no response is
		// obtained from the other end.
		{
			Description: "TCP keep-alive probe count",
			Level:       IPPROTO_TCP,
			Name:        TCP_KEEPCNT,
			Value:       &envoy_core_v3.SocketOption_IntValue{IntValue: 9},
			State:       envoy_core_v3.SocketOption_STATE_LISTENING,
		},
	}
}

// FilterChainTLS returns a TLS enabled envoy_listener_v3.FilterChain.
func FilterChainTLS(domain string, downstream *envoy_tls_v3.DownstreamTlsContext, filters []*envoy_listener_v3.Filter) *envoy_listener_v3.FilterChain {
	fc := &envoy_listener_v3.FilterChain{
		Filters: filters,
		FilterChainMatch: &envoy_listener_v3.FilterChainMatch{
			ServerNames: []string{domain},
		},
	}
	// Attach TLS data to this listener if provided.
	if downstream != nil {
		fc.TransportSocket = DownstreamTLSTransportSocket(downstream)
	}
	return fc
}

// DownstreamTLSTransportSocket returns a custom transport socket using the DownstreamTlsContext provided.
func DownstreamTLSTransportSocket(tls *envoy_tls_v3.DownstreamTlsContext) *envoy_core_v3.TransportSocket {
	return &envoy_core_v3.TransportSocket{
		Name: "envoy.transport_sockets.tls",
		ConfigType: &envoy_core_v3.TransportSocket_TypedConfig{
			TypedConfig: MustMarshalAny(tls),
		},
	}
}

// DownstreamTLSContext creates a new DownstreamTlsContext.
func DownstreamTLSContext(serverSecret *v1.Secret, tlsMinProtoVersion envoy_v3_tls.TlsParameters_TlsProtocol, peerValidationContext *PeerValidationContext, alpnProtos ...string) *envoy_v3_tls.DownstreamTlsContext {
	context := &envoy_v3_tls.DownstreamTlsContext{
		CommonTlsContext: &envoy_v3_tls.CommonTlsContext{
			TlsParams: &envoy_v3_tls.TlsParameters{
				TlsMinimumProtocolVersion: tlsMinProtoVersion,
				TlsMaximumProtocolVersion: envoy_v3_tls.TlsParameters_TLSv1_3,
			},
			TlsCertificateSdsSecretConfigs: []*envoy_v3_tls.SdsSecretConfig{{
				Name:      Secretname(serverSecret),
				SdsConfig: ConfigSource("envoyingresscontroller"),
			}},
			AlpnProtocols: alpnProtos,
		},
	}

	if peerValidationContext.GetCACertificate() != nil {
		vc := validationContext(peerValidationContext.GetCACertificate(), "")
		if vc != nil {
			context.CommonTlsContext.ValidationContextType = vc
			context.RequireClientCertificate = &wrapperspb.BoolValue{
				Value: true,
			}
		}
	}

	return context
}

// Filters returns a []*envoy_listener_v3.Filter for the supplied filters.
func Filters(filters ...*envoy_listener_v3.Filter) []*envoy_listener_v3.Filter {
	if len(filters) == 0 {
		return nil
	}
	return filters
}

// ProtoNamesForVersions returns the slice of ALPN protocol names for the give HTTP versions.
func ProtoNamesForVersions(versions ...HTTPVersionType) []string {
	protocols := map[HTTPVersionType]string{
		HTTPVersion1: "http/1.1",
		HTTPVersion2: "h2",
		HTTPVersion3: "",
	}
	defaultVersions := []string{"h2", "http/1.1"}
	wantedVersions := map[HTTPVersionType]struct{}{}

	if versions == nil {
		return defaultVersions
	}

	for _, v := range versions {
		wantedVersions[v] = struct{}{}
	}

	var alpn []string

	// Check for versions in preference order.
	for _, v := range []HTTPVersionType{HTTPVersionAuto, HTTPVersion2, HTTPVersion1} {
		if _, ok := wantedVersions[v]; ok {
			if v == HTTPVersionAuto {
				return defaultVersions
			}

			//log.Printf("wanted %d -> %s", v, protocols[v])
			alpn = append(alpn, protocols[v])
		}
	}

	return alpn
}
