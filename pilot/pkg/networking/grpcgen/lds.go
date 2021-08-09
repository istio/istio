// Copyright Istio Authors
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

package grpcgen

import (
	"net"
	"strconv"
	"strings"

	core "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	listener "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	hcm "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/http_connection_manager/v3"
	tls "github.com/envoyproxy/go-control-plane/envoy/extensions/transport_sockets/tls/v3"
	discovery "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"
	"github.com/golang/protobuf/ptypes/wrappers"

	"istio.io/istio/pilot/pkg/model"
	authnplugin "istio.io/istio/pilot/pkg/networking/plugin/authn"
	"istio.io/istio/pilot/pkg/networking/util"
	"istio.io/istio/pilot/pkg/security/authn"
	"istio.io/istio/pilot/pkg/security/authn/factory"
	"istio.io/istio/pkg/config/labels"
	"istio.io/istio/pkg/istio-agent/grpcxds"
)

// BuildListeners handles a LDS request, returning listeners of ApiListener type.
// The request may include a list of resource names, using the full_hostname[:port] format to select only
// specific services.
func (g *GrpcConfigGenerator) BuildListeners(node *model.Proxy, push *model.PushContext, names []string) model.Resources {
	filter := newListenerNameFilter(names)

	log.Debugf("building lds for %s with filter:\n%v", node.ID, filter)

	resp := make(model.Resources, 0, len(filter))
	resp = append(resp, buildOutboundListeners(node, filter)...)
	resp = append(resp, buildInboundListeners(node, push, filter.inboundNames())...)

	return resp
}

func buildInboundListeners(node *model.Proxy, push *model.PushContext, names []string) model.Resources {
	if len(names) == 0 {
		return nil
	}
	var out model.Resources
	policyApplier := factory.NewPolicyApplier(push, node.Metadata.Namespace, labels.Collection{node.Metadata.Labels})
	serviceInstancesByPort := map[uint32]*model.ServiceInstance{}
	for _, si := range node.ServiceInstances {
		serviceInstancesByPort[si.Endpoint.EndpointPort] = si
	}

	for _, name := range names {
		listenAddress := strings.TrimPrefix(name, grpcxds.ServerListenerNamePrefix)
		listenHost, listenPortStr, err := net.SplitHostPort(listenAddress)
		if err != nil {
			log.Errorf("failed parsing address from gRPC listener name %s: %v", name, err)
			continue
		}
		listenPort, err := strconv.Atoi(listenPortStr)
		if err != nil {
			log.Errorf("failed parsing port from gRPC listener name %s: %v", name, err)
			continue
		}
		si, ok := serviceInstancesByPort[uint32(listenPort)]
		if !ok {
			log.Warnf("%s has no service instance for port %s", node.ID, listenPortStr)
			continue
		}

		ll := &listener.Listener{
			Name: name,
			Address: &core.Address{Address: &core.Address_SocketAddress{
				SocketAddress: &core.SocketAddress{
					Address: listenHost,
					PortSpecifier: &core.SocketAddress_PortValue{
						PortValue: uint32(listenPort),
					},
				},
			}},
			FilterChains: buildFilterChains(node, push, si, policyApplier),
			// the following must not be set or the client will NACK
			ListenerFilters: nil,
			UseOriginalDst:  nil,
		}
		out = append(out, &discovery.Resource{
			Name:     ll.Name,
			Resource: util.MessageToAny(ll),
		})
	}
	return out
}

func buildFilterChains(node *model.Proxy, push *model.PushContext, si *model.ServiceInstance, applier authn.PolicyApplier) []*listener.FilterChain {
	mode := applier.GetMutualTLSModeForPort(si.Endpoint.EndpointPort)

	var tlsContext *tls.DownstreamTlsContext
	if mode != model.MTLSDisable && mode != model.MTLSUnknown {
		tlsContext = &tls.DownstreamTlsContext{
			CommonTlsContext: buildCommonTLSContext(authnplugin.TrustDomainsForValidation(push.Mesh)),
			// TODO plain TLS support
			RequireClientCertificate: &wrappers.BoolValue{Value: true},
		}
	}

	if mode == model.MTLSUnknown {
		log.Warnf("could not find mTLS mode for %s on %s; defaulting to DISABLE", si.Service.Hostname, node.ID)
		mode = model.MTLSDisable
	}
	if mode == model.MTLSPermissive {
		// TODO gRPC's filter chain match is super limted - only effective transport_protocol match is "raw_buffer"
		// see https://github.com/grpc/proposal/blob/master/A36-xds-for-servers.md for detail
		log.Warnf("cannot support PERMISSIVE mode for %s on %s; defaulting to DISABLE", si.Service.Hostname, node.ID)
		mode = model.MTLSDisable
	}

	var out []*listener.FilterChain
	switch mode {
	case model.MTLSDisable:
		out = append(out, buildFilterChain("plaintext", nil))
	case model.MTLSStrict:
		out = append(out, buildFilterChain("mtls", tlsContext))
		// TODO permissive builts both plaintext and mtls; when tlsContext is present add a match for protocol
	}

	return out
}

func buildFilterChain(nameSuffix string, tlsContext *tls.DownstreamTlsContext) *listener.FilterChain {
	out := &listener.FilterChain{
		Name:             "inbound-" + nameSuffix,
		FilterChainMatch: nil,
		Filters: []*listener.Filter{{
			Name: "inbound-hcm" + nameSuffix,
			ConfigType: &listener.Filter_TypedConfig{
				TypedConfig: util.MessageToAny(&hcm.HttpConnectionManager{
					// TODO gRPC doesn't support httpfilter yet; sending won't cause a NACK but they don't do anything
				}),
			},
		}},
	}
	if tlsContext != nil {
		out.TransportSocket = &core.TransportSocket{
			Name:       transportSocketName,
			ConfigType: &core.TransportSocket_TypedConfig{TypedConfig: util.MessageToAny(tlsContext)},
		}
	}
	return out
}

func buildOutboundListeners(node *model.Proxy, filter listenerNameFilter) model.Resources {
	out := make(model.Resources, 0, len(filter))
	for _, el := range node.SidecarScope.EgressListeners {
		for _, sv := range el.Services() {
			sHost := string(sv.Hostname)
			if !filter.includeHostOrExactName(sHost) {
				continue
			}
			for _, p := range sv.Ports {
				sPort := strconv.Itoa(p.Port)
				if !filter.includeHostPort(sHost, sPort) {
					continue
				}
				hp := net.JoinHostPort(sHost, sPort)
				ll := &listener.Listener{
					Name: hp,
					Address: &core.Address{
						Address: &core.Address_SocketAddress{
							SocketAddress: &core.SocketAddress{
								Address: sv.Address,
								PortSpecifier: &core.SocketAddress_PortValue{
									PortValue: uint32(p.Port),
								},
							},
						},
					},
					ApiListener: &listener.ApiListener{
						ApiListener: util.MessageToAny(&hcm.HttpConnectionManager{
							RouteSpecifier: &hcm.HttpConnectionManager_Rds{
								// TODO: for TCP listeners don't generate RDS, but some indication of cluster name.
								Rds: &hcm.Rds{
									ConfigSource: &core.ConfigSource{
										ConfigSourceSpecifier: &core.ConfigSource_Ads{
											Ads: &core.AggregatedConfigSource{},
										},
									},
									RouteConfigName: clusterKey(sHost, p.Port),
								},
							},
						}),
					},
				}

				out = append(out, &discovery.Resource{
					Name:     ll.Name,
					Resource: util.MessageToAny(ll),
				})
			}
		}
	}
	return out
}

// map[host] -> map[port] -> exists
// if the map[port] is empty, an exact listener name was provided (non-hostport)
type listenerNameFilter map[string]map[string]struct{}

func (f listenerNameFilter) includeHostOrExactName(s string) bool {
	if len(f) == 0 {
		// filter is empty, include everything
		return true
	}
	_, ok := f[s]
	return ok
}

func (f listenerNameFilter) includeHostPort(host string, port string) bool {
	if len(f) == 0 {
		// filter is empty, include everything
		return true
	}
	portMap, ok := f[host]
	if !ok {
		return false
	}
	if len(portMap) == 0 {
		return true
	}
	_, ok = portMap[port]
	return ok
}

func (f listenerNameFilter) inboundNames() []string {
	var out []string
	for key := range f {
		if strings.HasPrefix(key, grpcxds.ServerListenerNamePrefix) {
			out = append(out, key)
		}
	}
	return out
}

func newListenerNameFilter(names []string) listenerNameFilter {
	filter := make(listenerNameFilter, len(names))
	for _, name := range names {
		if strings.HasPrefix(name, grpcxds.ServerListenerNamePrefix) {
			filter[name] = map[string]struct{}{}
			continue
		}
		if host, port, err := net.SplitHostPort(name); err == nil {
			var first bool
			portMap, ok := filter[host]
			if !ok {
				portMap = map[string]struct{}{}
				filter[host] = portMap
				first = true
			}
			// the portMap is empty and we didn't just create it, we want to include all ports
			if len(portMap) == 0 && !first {
				continue
			}

			portMap[port] = struct{}{}
		} else {
			// if the listener name was "foo.com" and we already have "foo.com" -> {80 -> struct{}{}}, replace
			// with an empty map to indicate we will include all ports since only the hostname was provided
			// TODO, should we just default to some port in this case?
			filter[name] = map[string]struct{}{}
		}
	}
	return filter
}
