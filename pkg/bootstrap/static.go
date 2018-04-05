// Copyright 2018 Istio Authors
//
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

package bootstrap

import (
	"fmt"
	"net"
	"strconv"
	"time"

	"github.com/envoyproxy/go-control-plane/envoy/api/v2"
	"github.com/envoyproxy/go-control-plane/envoy/api/v2/auth"
	"github.com/envoyproxy/go-control-plane/envoy/api/v2/cluster"
	"github.com/envoyproxy/go-control-plane/envoy/api/v2/core"
	"github.com/envoyproxy/go-control-plane/envoy/api/v2/listener"
	"github.com/envoyproxy/go-control-plane/envoy/api/v2/route"
	bootstrap "github.com/envoyproxy/go-control-plane/envoy/config/bootstrap/v2"
	accesslog "github.com/envoyproxy/go-control-plane/envoy/config/filter/accesslog/v2"
	hcm "github.com/envoyproxy/go-control-plane/envoy/config/filter/network/http_connection_manager/v2"
	trace "github.com/envoyproxy/go-control-plane/envoy/config/trace/v2"
	"github.com/envoyproxy/go-control-plane/pkg/util"
	"github.com/ghodss/yaml"
	"github.com/gogo/protobuf/jsonpb"
	"github.com/gogo/protobuf/proto"
	"github.com/gogo/protobuf/types"

	meshconfig "istio.io/api/mesh/v1alpha1"
	mixerv1 "istio.io/api/mixer/v1"
	mixerclient "istio.io/api/mixer/v1/config/client"
)

// Default constants
const (
	ZipkinCluster    = "zipkin"
	TelemetryCluster = "mixer_report_server"
)

var (
	circuitBreakers = &cluster.CircuitBreakers{
		Thresholds: []*cluster.CircuitBreakers_Thresholds{{
			Priority:           core.RoutingPriority_DEFAULT,
			MaxConnections:     &types.UInt32Value{Value: 100000},
			MaxPendingRequests: &types.UInt32Value{Value: 100000},
			MaxRequests:        &types.UInt32Value{Value: 100000},
			MaxRetries:         &types.UInt32Value{Value: 3},
		}},
	}
)

// Options are high-level options for the bootstrap.
type Options struct {
	Upstreams                      []Upstream // list of upstreams behind the proxy
	Service                        string     // service FQDN
	UID                            string     // workload ID
	TelemetryUpstream              *Upstream  // set to one of the upstreams to route telemetry locally
	TelemetryAddress, TelemetrySAN string     // set to telemetry DNS address and SAN, e.g. istio-telemetry:15004
	ZipkinAddress                  string     // set to non-empty DNS zipkin address, e.g. zipkin:9411
	ControlPlaneAuth               bool       // set to enable control plane authentication
	AdminPort                      int32      // AdminPort for the admin interface
}

// Upstream declaration for a local HTTP service instance.
type Upstream struct {
	ListenPort   uint32
	UpstreamPort uint32
	GRPC         bool
	Operation    string
	Auth         *bool // overrides ControlPlaneAuth if set
}

// BuildOptions generates options for a bootstrap from a pre-defined profile.
// These options contain all the hard-coded names selected at installation time.
func BuildOptions(proxyConfig meshconfig.ProxyConfig, staticProfile, name, namespace string) Options {
	opts := Options{
		TelemetryAddress: "istio-telemetry:15004",
		TelemetrySAN:     fmt.Sprintf("spiffe://cluster.local/ns/%s/sa/istio-mixer-service-account", namespace),
		ZipkinAddress:    proxyConfig.ZipkinAddress,
		ControlPlaneAuth: proxyConfig.ControlPlaneAuthPolicy == meshconfig.AuthenticationPolicy_MUTUAL_TLS,
		AdminPort:        proxyConfig.ProxyAdminPort,
		UID:              fmt.Sprintf("kubernetes://%s.%s", name, namespace),
	}
	switch staticProfile {
	case "telemetry":
		opts.TelemetryUpstream = &Upstream{
			ListenPort:   15004,
			UpstreamPort: 9091,
			GRPC:         true,
			Operation:    "Check",
		}
		opts.Upstreams = []Upstream{*opts.TelemetryUpstream}
		opts.Service = fmt.Sprintf("istio-telemetry.%s.svc.cluster.local", namespace)
	case "policy":
		opts.Upstreams = []Upstream{{
			ListenPort:   15004,
			UpstreamPort: 9091,
			GRPC:         true,
			Operation:    "Report",
		}}
		opts.Service = fmt.Sprintf("istio-policy.%s.svc.cluster.local", namespace)
	case "pilot":
		v1 := Upstream{
			ListenPort:   15003,
			UpstreamPort: 8080,
			Operation:    "v1",
		}
		v2 := Upstream{
			ListenPort:   15011,
			GRPC:         true,
			UpstreamPort: 15010,
			Operation:    "xDS",
		}
		secure := Upstream{
			ListenPort:   15005,
			UpstreamPort: 8080,
			Operation:    "v1",
			Auth:         func(b bool) *bool { return &b }(true),
		}
		insecure := Upstream{
			ListenPort:   15007,
			UpstreamPort: 8080,
			Operation:    "v1",
			Auth:         func(b bool) *bool { return &b }(false),
		}
		opts.Upstreams = []Upstream{v1, v2, secure, insecure}
		opts.Service = fmt.Sprintf("istio-pilot.%s.svc.cluster.local", namespace)
		// TODO: some tests fail due to zipkin
		opts.ZipkinAddress = ""
	}
	return opts
}

// BuildBootstrap creates a static config for the sidecar HTTP proxy.
func BuildBootstrap(opts Options) (*bootstrap.Bootstrap, error) {
	out := bootstrap.Bootstrap{
		Admin: bootstrap.Admin{
			AccessLogPath: "/dev/stdout",
			Address: core.Address{
				Address: &core.Address_SocketAddress{
					SocketAddress: &core.SocketAddress{
						Address:       "127.0.0.1",
						PortSpecifier: &core.SocketAddress_PortValue{PortValue: uint32(opts.AdminPort)},
					},
				},
			},
		},
		StaticResources: &bootstrap.Bootstrap_StaticResources{
			Clusters: BuildClusters(opts.Upstreams),
		},
	}

	if opts.ZipkinAddress != "" {
		zipkinAddress, err := toAddress(opts.ZipkinAddress)
		if err != nil {
			return nil, err
		}
		out.StaticResources.Clusters = append(out.StaticResources.Clusters,
			v2.Cluster{
				Name:           ZipkinCluster,
				ConnectTimeout: 1 * time.Second,
				Type:           v2.Cluster_STRICT_DNS,
				LbPolicy:       v2.Cluster_ROUND_ROBIN,
				Hosts:          []*core.Address{zipkinAddress},
			})
		out.Tracing = &trace.Tracing{
			Http: &trace.Tracing_Http{
				Name: util.Zipkin,
				Config: toStruct(&trace.ZipkinConfig{
					CollectorCluster:  ZipkinCluster,
					CollectorEndpoint: "/api/v1/spans",
				}),
			},
		}
	}

	// telemetry bootstrap should route telemetry directly to the local upstream
	telemetryCluster := TelemetryCluster
	if opts.TelemetryUpstream == nil {
		telemetryAddress, err := toAddress(opts.TelemetryAddress)
		if err != nil {
			return nil, err
		}
		cluster := v2.Cluster{
			Name:                 TelemetryCluster,
			ConnectTimeout:       1 * time.Second,
			Type:                 v2.Cluster_STRICT_DNS,
			LbPolicy:             v2.Cluster_ROUND_ROBIN,
			Hosts:                []*core.Address{telemetryAddress},
			CircuitBreakers:      circuitBreakers,
			Http2ProtocolOptions: &core.Http2ProtocolOptions{},
		}
		if opts.ControlPlaneAuth {
			cluster.TlsContext = &auth.UpstreamTlsContext{
				CommonTlsContext: commonTLSContext([]string{opts.TelemetrySAN}),
			}
		}
		out.StaticResources.Clusters = append(out.StaticResources.Clusters, cluster)
	} else {
		telemetryCluster = clusterName(*opts.TelemetryUpstream)
	}

	out.StaticResources.Listeners = BuildListeners(opts, telemetryCluster)

	if err := out.Validate(); err != nil {
		return nil, err
	}

	return &out, nil
}

func commonTLSContext(altNames []string) *auth.CommonTlsContext {
	return &auth.CommonTlsContext{
		TlsCertificates: []*auth.TlsCertificate{{
			CertificateChain: &core.DataSource{
				Specifier: &core.DataSource_Filename{Filename: "/etc/certs/cert-chain.pem"},
			},
			PrivateKey: &core.DataSource{
				Specifier: &core.DataSource_Filename{Filename: "/etc/certs/key.pem"},
			},
		}},
		ValidationContext: &auth.CertificateValidationContext{
			TrustedCa: &core.DataSource{
				Specifier: &core.DataSource_Filename{Filename: "/etc/certs/root-cert.pem"},
			},
			VerifySubjectAltName: altNames,
		},
	}
}

func toAddress(address string) (*core.Address, error) {
	host, portStr, err := net.SplitHostPort(address)
	if err != nil {
		return nil, err
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		return nil, err
	}
	return &core.Address{
		Address: &core.Address_SocketAddress{
			SocketAddress: &core.SocketAddress{
				Address:       host,
				PortSpecifier: &core.SocketAddress_PortValue{PortValue: uint32(port)},
			},
		},
	}, nil
}

func toStruct(msg proto.Message) *types.Struct {
	out, err := util.MessageToStruct(msg)
	if err != nil {
		panic(err)
	}
	return out
}

// BuildServiceConfig generates a mixer client config.
func BuildServiceConfig(service, uid, telemetryCluster string) *mixerclient.HttpClientConfig {
	return &mixerclient.HttpClientConfig{
		DefaultDestinationService: service,
		ServiceConfigs: map[string]*mixerclient.ServiceConfig{
			service: {
				DisableCheckCalls:  true,
				DisableReportCalls: false,
				MixerAttributes: &mixerv1.Attributes{
					Attributes: map[string]*mixerv1.Attributes_AttributeValue{
						"destination.service": {Value: &mixerv1.Attributes_AttributeValue_StringValue{
							StringValue: service,
						}},
						"destination.uid": {Value: &mixerv1.Attributes_AttributeValue_StringValue{
							StringValue: uid,
						}},
					},
				},
			},
		},
		Transport: &mixerclient.TransportConfig{
			CheckCluster:  "mixer_check_server",
			ReportCluster: telemetryCluster,
		},
	}
}

// BuildListeners generates HTTP listeners from upstreams
func BuildListeners(opts Options, telemetryCluster string) []v2.Listener {
	out := make([]v2.Listener, 0, len(opts.Upstreams))
	config := toStruct(BuildServiceConfig(opts.Service, opts.UID, telemetryCluster))
	for _, upstream := range opts.Upstreams {
		config := &hcm.HttpConnectionManager{
			CodecType:         hcm.AUTO,
			StatPrefix:        fmt.Sprintf("%d", upstream.ListenPort),
			GenerateRequestId: &types.BoolValue{Value: true}, // required for zipkin tracing
			AccessLog: []*accesslog.AccessLog{{
				Name: util.FileAccessLog,
				Config: toStruct(&accesslog.FileAccessLog{
					Path: "/dev/stdout",
				}),
			}},
			HttpFilters: []*hcm.HttpFilter{
				{
					Name:   "mixer",
					Config: config,
				},
				{
					Name: util.Router,
				},
			},
			RouteSpecifier: &hcm.HttpConnectionManager_RouteConfig{
				RouteConfig: &v2.RouteConfiguration{
					Name: fmt.Sprintf("%d", upstream.ListenPort),
					VirtualHosts: []route.VirtualHost{{
						Name:    opts.Service,
						Domains: []string{"*"},
						Routes: []route.Route{{
							Match:     route.RouteMatch{PathSpecifier: &route.RouteMatch_Prefix{Prefix: "/"}},
							Decorator: &route.Decorator{Operation: upstream.Operation},
							Action: &route.Route_Route{
								Route: &route.RouteAction{
									ClusterSpecifier: &route.RouteAction_Cluster{Cluster: clusterName(upstream)},
									Timeout:          (func(t time.Duration) *time.Duration { return &t })(0),
								},
							},
						}},
					}},
				},
			},
		}
		if opts.ZipkinAddress != "" {
			config.Tracing = &hcm.HttpConnectionManager_Tracing{
				OperationName: hcm.INGRESS,
			}
		}
		if upstream.GRPC {
			config.CodecType = hcm.HTTP2
		}
		lst := v2.Listener{
			Address: core.Address{
				Address: &core.Address_SocketAddress{
					SocketAddress: &core.SocketAddress{
						Address:       "0.0.0.0",
						PortSpecifier: &core.SocketAddress_PortValue{PortValue: upstream.ListenPort},
					},
				},
			},
			Name: fmt.Sprintf("%d", upstream.ListenPort),
			FilterChains: []listener.FilterChain{{
				Filters: []listener.Filter{{
					Name:   util.HTTPConnectionManager,
					Config: toStruct(config),
				}},
			}},
		}
		authEnable := opts.ControlPlaneAuth
		if upstream.Auth != nil {
			authEnable = *upstream.Auth
		}

		if authEnable {
			ctx := &auth.DownstreamTlsContext{
				CommonTlsContext:         commonTLSContext(nil),
				RequireClientCertificate: &types.BoolValue{Value: true},
			}
			if upstream.GRPC {
				ctx.CommonTlsContext.AlpnProtocols = []string{"h2"}
			} else {
				ctx.CommonTlsContext.AlpnProtocols = []string{"http/1.1"}
			}
			lst.FilterChains[0].TlsContext = ctx
		}
		out = append(out, lst)
	}
	return out
}

func clusterName(upstream Upstream) string {
	return fmt.Sprintf("in.%d", upstream.UpstreamPort)
}

// BuildClusters generates clusters from upstreams
func BuildClusters(upstreams []Upstream) []v2.Cluster {
	// skip duplicated upstream ports
	set := make(map[uint32]bool)
	out := make([]v2.Cluster, 0, len(upstreams))
	for _, upstream := range upstreams {
		if set[upstream.UpstreamPort] {
			continue
		}
		cluster := v2.Cluster{
			Name:           clusterName(upstream),
			ConnectTimeout: 1 * time.Second,
			Type:           v2.Cluster_STATIC,
			LbPolicy:       v2.Cluster_ROUND_ROBIN,
			Hosts: []*core.Address{{
				Address: &core.Address_SocketAddress{
					SocketAddress: &core.SocketAddress{
						Address:       "127.0.0.1",
						PortSpecifier: &core.SocketAddress_PortValue{PortValue: upstream.UpstreamPort},
					},
				},
			}},
			CircuitBreakers: circuitBreakers,
		}
		if upstream.GRPC {
			cluster.Http2ProtocolOptions = &core.Http2ProtocolOptions{}
		}
		out = append(out, cluster)
		set[upstream.UpstreamPort] = true
	}
	return out
}

// ToYAML converts bootstrap to YAML
func ToYAML(config proto.Message) ([]byte, error) {
	marshaler := jsonpb.Marshaler{OrigName: true}
	out, err := marshaler.MarshalToString(config)
	if err != nil {
		return nil, err
	}
	content, err := yaml.JSONToYAML([]byte(out))
	if err != nil {
		return nil, err
	}
	return content, nil
}
