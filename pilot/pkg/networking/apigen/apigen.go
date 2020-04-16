// Copyright 2017 Istio Authors
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

package apigen

import (
	"net"
	"strconv"
	"strings"

	xdsapi "github.com/envoyproxy/go-control-plane/envoy/api/v2"
	envoycore "github.com/envoyproxy/go-control-plane/envoy/api/v2/core"
	envoy_api_v2_route "github.com/envoyproxy/go-control-plane/envoy/api/v2/route"
	v2 "github.com/envoyproxy/go-control-plane/envoy/config/filter/network/http_connection_manager/v2"
	envoy_config_listener_v2 "github.com/envoyproxy/go-control-plane/envoy/config/listener/v2"
	"github.com/gogo/protobuf/types"
	"github.com/golang/protobuf/ptypes/any"
	mcp "istio.io/api/mcp/v1alpha1"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/util"
	"istio.io/istio/pkg/config/schema/resource"
	"istio.io/pkg/log"

	"istio.io/istio/pkg/config/host"
)

// To avoid a recoursive depenency to v2.
const (
	typePrefix = "type.googleapis.com/envoy.api.v2."

	// Constants used for XDS

	// ClusterType is used for cluster discovery. Typically first request received
	ClusterType = typePrefix + "Cluster"
	// EndpointType is used for EDS and ADS endpoint discovery. Typically second request.
	EndpointType = typePrefix + "ClusterLoadAssignment"
	// ListenerType is sent after clusters and endpoints.
	ListenerType = typePrefix + "Listener"
	// RouteType is sent after listeners.
	RouteType = typePrefix + "RouteConfiguration"
)

// Support generation of 'ApiListener' LDS responses, used for native support of gRPC.
// The same response can also be used by other apps using XDS directly.

// GRPC proposal:
// https://github.com/grpc/proposal/blob/master/A27-xds-global-load-balancing.md
//
// Note that this implementation is tested against gRPC, but it is generic - any other framework can
// use this XDS mode to get load balancing info from Istio, including MC/VM/etc.

// DNS can populate the name to cluster VIP mapping using this response.

// The corresponding RDS response is also generated - currently gRPC has special differences
// and can't understand normal Istio RDS - in particular expects "" instead of "/" as
// default prefix, and is expects just the route for one host.
// handleAck will detect if the message is an ACK or NACK, and update/log/count
// using the generic structures. "Classical" CDS/LDS/RDS/EDS use separate logic -
// this is used for the API-based LDS and generic messages.
type GrpcConfigGenerator struct {
}

func (g *GrpcConfigGenerator) Generate(node *model.Proxy, push *model.PushContext, w *model.WatchedResource) []*any.Any {
	switch w.TypeUrl {
	case ListenerType:
		return g.BuildListeners(node, push, w.ResourceNames)
	case ClusterType:
		return g.BuildClusters(node, push, w.ResourceNames)
	case RouteType:
		return g.BuildHTTPRoutes(node, push, w.ResourceNames)
	default:
		return g.handleConfigResource(node, push, w)
	}

	return nil
}

// handleLDSApiType handles a LDS request, returning listeners of ApiListener type.
// The request may include a list of resource names, using the full_hostname[:port] format to select only
// specific services.
func (g *GrpcConfigGenerator) BuildListeners(node *model.Proxy, push *model.PushContext, names []string) []*any.Any {
	resp := []*any.Any{}

	filter := map[string]bool{}
	for _, name := range names {
		if strings.Contains(name, ":") {
			n, _, err := net.SplitHostPort(name)
			if err == nil {
				name = n
			}
		}
		filter[name] = true
	}

	for _, el := range node.SidecarScope.EgressListeners {
		for _, sv := range el.Services() {
			shost := string(sv.Hostname)
			if len(filter) > 0 {
				// DiscReq has a filter - only return services that match
				if !filter[shost] {
					continue
				}
			}
			for _, p := range sv.Ports {
				hp := net.JoinHostPort(shost, strconv.Itoa(p.Port))
				ll := &xdsapi.Listener{
					Name: hp,
				}

				ll.Address = &envoycore.Address{
					Address: &envoycore.Address_SocketAddress{
						SocketAddress: &envoycore.SocketAddress{
							Address: sv.Address,
							PortSpecifier: &envoycore.SocketAddress_PortValue{
								PortValue: uint32(p.Port),
							},
						},
					},
				}
				// TODO: for TCP listeners don't generate RDS, but some indication of cluster name.
				ll.ApiListener = &envoy_config_listener_v2.ApiListener{
					ApiListener: util.MessageToAny(&v2.HttpConnectionManager{
						RouteSpecifier: &v2.HttpConnectionManager_Rds{
							Rds: &v2.Rds{
								RouteConfigName: hp,
							},
						},
					}),
				}
				resp = append(resp, util.MessageToAny(ll))
			}
		}
	}

	return resp
}

// Handle a gRPC CDS request, used with the 'ApiListener' style of requests.
// The main difference is that the request includes Resources.
func (g *GrpcConfigGenerator) BuildClusters(node *model.Proxy, push *model.PushContext, names []string) []*any.Any {
	resp := []*any.Any{}
	// gRPC doesn't currently support any of the APIs - returning just the expected EDS result.
	// Since the code is relatively strict - we'll add info as needed.
	for _, n := range names {
		hn, portn, err := net.SplitHostPort(n)
		if err != nil {
			log.Warna("Failed to parse ", n, " ", err)
			continue
		}
		rc := &xdsapi.Cluster{
			Name:                 n,
			ClusterDiscoveryType: &xdsapi.Cluster_Type{Type: xdsapi.Cluster_EDS},
			EdsClusterConfig: &xdsapi.Cluster_EdsClusterConfig{
				ServiceName: "outbound|" + portn + "||" + hn,
				EdsConfig: &envoycore.ConfigSource{
					ConfigSourceSpecifier: &envoycore.ConfigSource_Ads{
						Ads: &envoycore.AggregatedConfigSource{},
					},
				},
			},
		}
		resp = append(resp, util.MessageToAny(rc))
	}
	return resp
}

// handleSplitRDS supports per-VIP routes, as used by GRPC.
// This mode is indicated by using names containing full host:port instead of just port.
// Returns true of the request is of this type.
func (g *GrpcConfigGenerator) BuildHTTPRoutes(node *model.Proxy, push *model.PushContext, routeNames []string) []*any.Any {
	resp := []*any.Any{}

	// Currently this mode is only used by GRPC, to extract Cluster for the default
	// route.
	// Current GRPC is also expecting the default route to be prefix=="", while we generate "/"
	// in normal response.
	// TODO: add support for full route, make sure GRPC is fixed to support both
	for _, n := range routeNames {
		hn, portn, err := net.SplitHostPort(n)
		if err != nil {
			log.Warna("Failed to parse ", n, " ", err)
			continue
		}
		port, err := strconv.Atoi(portn)
		if err != nil {
			log.Warna("Failed to parse port ", n, " ", err)
			continue
		}
		el := node.SidecarScope.GetEgressListenerForRDS(port, "")
		// TODO: use VirtualServices instead !
		// Currently gRPC doesn't support matching the path.
		svc := el.Services()
		for _, s := range svc {
			if s.Hostname.Matches(host.Name(hn)) {
				// Only generate the required route for grpc. Will need to generate more
				// as GRPC adds more features.
				rc := &xdsapi.RouteConfiguration{
					Name: n,
					VirtualHosts: []*envoy_api_v2_route.VirtualHost{
						&envoy_api_v2_route.VirtualHost{
							Name:    hn,
							Domains: []string{hn, n},

							Routes: []*envoy_api_v2_route.Route{
								&envoy_api_v2_route.Route{
									Match: &envoy_api_v2_route.RouteMatch{
										PathSpecifier: &envoy_api_v2_route.RouteMatch_Prefix{Prefix: ""},
									},
									Action: &envoy_api_v2_route.Route_Route{
										Route: &envoy_api_v2_route.RouteAction{
											ClusterSpecifier: &envoy_api_v2_route.RouteAction_Cluster{
												Cluster: n,
											},
										},
									},
								},
							},
						},
					},
				}
				resp = append(resp, util.MessageToAny(rc))
			}
		}
	}
	return resp
}

// Handle watching Istio config types. This provides similar functionality with MCP.
// Alternative to using 8080:/debug/configz
// Names are based on the current stable naming in istiod.
func (g *GrpcConfigGenerator) handleConfigResource(node *model.Proxy, push *model.PushContext, w *model.WatchedResource) []*any.Any {
	resp := []*any.Any{}

	// Example: networking.istio.io/v1alpha3/VirtualService
	// Note: this is the style used by MCP and its config. Pilot is using 'Group/Version/Kind' as the
	// key, which is similar. However the actual type in the Any should be a real proto.
	// For example: correct type is 'type.googlepis.com/istio.networking.v1alpha3.EnvoyFilter
	// We use: networking.istio.io/v1alpha3/EnvoyFilter
	gvk := strings.SplitN(w.TypeUrl, "/", 3)
	if len(gvk) == 3 {
		cfg, err := push.IstioConfigStore.List(resource.GroupVersionKind{
			Group:   gvk[0],
			Version: gvk[1],
			Kind:    gvk[2],
		}, "")
		if err != nil {
			log.Warnf("ADS: Unknown watched resources %s %v", w.TypeUrl, err)
			return resp
		}
		for _, c := range cfg {
			// Right now model.Config is not a proto - until we change it, mcp.Resource.
			// This also helps migrating MCP users.

			b, err := configToResource(&c)
			if err != nil {
				log.Warna("Resource error ", err, " ", c.Namespace, "/", c.Name)
				continue
			}
			bany, err := types.MarshalAny(b)
			if err == nil {
				resp = append(resp, &any.Any{
					TypeUrl: bany.TypeUrl,
					Value:  bany.Value,
				})
			} else {
				log.Warna("Any ", err)
			}
		}
	}

	return resp
}

// Convert from model.Config, which has no associated proto, to MCP Resource proto.
// TODO: define a proto matching Config - to avoid useless superficial conversions.
func configToResource(c *model.Config) (*mcp.Resource, error) {
	r := &mcp.Resource{}

	// MCP, K8S and Istio configs use gogo configs
	// On the wire it's the same as golang proto.
	a, err := types.MarshalAny(c.Spec)
	if err != nil {
		return nil, err
	}
	r.Body = a
	ts, err := types.TimestampProto(c.CreationTimestamp)
	if err != nil {
		return nil, err
	}
	r.Metadata = &mcp.Metadata{
		Name:                 c.Namespace + "/" + c.Name,
		CreateTime:           ts,
		Version:              c.ResourceVersion,
		Labels: c.Labels,
		Annotations: c.Annotations,
	}

	return r, nil
}


