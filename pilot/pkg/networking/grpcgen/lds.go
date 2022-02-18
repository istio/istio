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
	"fmt"
	"net"
	"strconv"
	"strings"

	core "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	listener "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	route "github.com/envoyproxy/go-control-plane/envoy/config/route/v3"
	hcm "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/http_connection_manager/v3"
	tls "github.com/envoyproxy/go-control-plane/envoy/extensions/transport_sockets/tls/v3"
	discovery "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"
	wrappers "google.golang.org/protobuf/types/known/wrapperspb"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/util"
	"istio.io/istio/pilot/pkg/security/authn"
	"istio.io/istio/pilot/pkg/security/authn/factory"
	"istio.io/istio/pilot/pkg/util/sets"
	xdsfilters "istio.io/istio/pilot/pkg/xds/filters"
	"istio.io/istio/pkg/config/labels"
	"istio.io/istio/pkg/istio-agent/grpcxds"
)

var supportedFilters = []*hcm.HttpFilter{
	xdsfilters.Fault,
	xdsfilters.Router,
}

// BuildListeners handles a LDS request, returning listeners of ApiListener type.
// The request may include a list of resource names, using the full_hostname[:port] format to select only
// specific services.
func (g *GrpcConfigGenerator) BuildListeners(node *model.Proxy, push *model.PushContext, names []string) model.Resources {
	filter := newListenerNameFilter(names, node)

	log.Debugf("building lds for %s with filter:\n%v", node.ID, filter)

	resp := make(model.Resources, 0, len(filter))
	resp = append(resp, buildOutboundListeners(node, push, filter)...)
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
			FilterChains: buildInboundFilterChains(node, push, si, policyApplier),
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

// nolint: unparam
func buildInboundFilterChains(node *model.Proxy, push *model.PushContext, si *model.ServiceInstance, applier authn.PolicyApplier) []*listener.FilterChain {
	mode := applier.GetMutualTLSModeForPort(si.Endpoint.EndpointPort)

	var tlsContext *tls.DownstreamTlsContext
	if mode != model.MTLSDisable && mode != model.MTLSUnknown {
		tlsContext = &tls.DownstreamTlsContext{
			CommonTlsContext: buildCommonTLSContext(nil),
			// TODO match_subject_alt_names field in validation context is not supported on the server
			// CommonTlsContext: buildCommonTLSContext(authnplugin.TrustDomainsForValidation(push.Mesh)),
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
		out = append(out, buildInboundFilterChain("plaintext", nil))
	case model.MTLSStrict:
		out = append(out, buildInboundFilterChain("mtls", tlsContext))
		// TODO permissive builts both plaintext and mtls; when tlsContext is present add a match for protocol
	}

	return out
}

func buildInboundFilterChain(nameSuffix string, tlsContext *tls.DownstreamTlsContext) *listener.FilterChain {
	out := &listener.FilterChain{
		Name:             "inbound-" + nameSuffix,
		FilterChainMatch: nil,
		Filters: []*listener.Filter{{
			Name: "inbound-hcm" + nameSuffix,
			ConfigType: &listener.Filter_TypedConfig{
				TypedConfig: util.MessageToAny(&hcm.HttpConnectionManager{
					RouteSpecifier: &hcm.HttpConnectionManager_RouteConfig{
						// https://github.com/grpc/grpc-go/issues/4924
						RouteConfig: &route.RouteConfiguration{
							Name: "inbound",
							VirtualHosts: []*route.VirtualHost{{
								Domains: []string{"*"},
								Routes: []*route.Route{{
									Match: &route.RouteMatch{
										PathSpecifier: &route.RouteMatch_Prefix{Prefix: "/"},
									},
									Action: &route.Route_NonForwardingAction{},
								}},
							}},
						},
					},
					HttpFilters: []*hcm.HttpFilter{xdsfilters.Router},
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

func buildOutboundListeners(node *model.Proxy, push *model.PushContext, filter listenerNames) model.Resources {
	out := make(model.Resources, 0, len(filter))
	for _, sv := range node.SidecarScope.Services() {
		serviceHost := string(sv.Hostname)
		match, ok := filter.includes(serviceHost)
		if !ok {
			continue
		}
		// we must duplicate the listener for every requested host - grpc may have watches for both foo and foo.ns
		for _, matchedHost := range match.RequestedNames.SortedList() {
			for _, p := range sv.Ports {
				sPort := strconv.Itoa(p.Port)
				if !match.includesPort(sPort) {
					continue
				}
				ll := &listener.Listener{
					Name: net.JoinHostPort(matchedHost, sPort),
					Address: &core.Address{
						Address: &core.Address_SocketAddress{
							SocketAddress: &core.SocketAddress{
								Address: sv.GetAddressForProxy(node),
								PortSpecifier: &core.SocketAddress_PortValue{
									PortValue: uint32(p.Port),
								},
							},
						},
					},
					ApiListener: &listener.ApiListener{
						ApiListener: util.MessageToAny(&hcm.HttpConnectionManager{
							HttpFilters: supportedFilters,
							RouteSpecifier: &hcm.HttpConnectionManager_Rds{
								// TODO: for TCP listeners don't generate RDS, but some indication of cluster name.
								Rds: &hcm.Rds{
									ConfigSource: &core.ConfigSource{
										ConfigSourceSpecifier: &core.ConfigSource_Ads{
											Ads: &core.AggregatedConfigSource{},
										},
									},
									RouteConfigName: clusterKey(serviceHost, p.Port),
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
type listenerNames map[string]listenerName

type listenerName struct {
	RequestedNames sets.Set
	Ports          sets.Set
}

func (ln *listenerName) includesPort(port string) bool {
	if len(ln.Ports) == 0 {
		return true
	}
	_, ok := ln.Ports[port]
	return ok
}

func (f listenerNames) includes(s string) (listenerName, bool) {
	if len(f) == 0 {
		// filter is empty, include everything
		return listenerName{RequestedNames: sets.NewSet(s)}, true
	}
	n, ok := f[s]
	return n, ok
}

func (f listenerNames) inboundNames() []string {
	var out []string
	for key := range f {
		if strings.HasPrefix(key, grpcxds.ServerListenerNamePrefix) {
			out = append(out, key)
		}
	}
	return out
}

func newListenerNameFilter(names []string, node *model.Proxy) listenerNames {
	filter := make(listenerNames, len(names))
	for _, name := range names {
		// inbound, create a simple entry and move on
		if strings.HasPrefix(name, grpcxds.ServerListenerNamePrefix) {
			filter[name] = listenerName{RequestedNames: sets.NewSet(name)}
			continue
		}

		host, port, err := net.SplitHostPort(name)
		hasPort := err == nil

		// attempt to expand shortname to FQDN
		requestedName := name
		if hasPort {
			requestedName = host
		}
		allNames := []string{requestedName}
		if fqdn := tryFindFQDN(requestedName, node); fqdn != "" {
			allNames = append(allNames, fqdn)
		}

		for _, name := range allNames {
			ln, ok := filter[name]
			if !ok {
				ln = listenerName{RequestedNames: sets.NewSet()}
			}
			ln.RequestedNames.Insert(requestedName)

			// only build the portmap if we aren't filtering this name yet, or if the existing filter is non-empty
			if hasPort && (!ok || len(ln.Ports) != 0) {
				if ln.Ports == nil {
					ln.Ports = map[string]struct{}{}
				}
				ln.Ports.Insert(port)
			} else if !hasPort {
				// if we didn't have a port, we should clear the portmap
				ln.Ports = nil
			}
			filter[name] = ln
		}
	}
	return filter
}

func tryFindFQDN(name string, node *model.Proxy) string {
	// no "." - assuming this is a shortname "foo" -> "foo.ns.svc.cluster.local"
	if !strings.Contains(name, ".") {
		return fmt.Sprintf("%s.%s", name, node.DNSDomain)
	}
	for _, suffix := range []string{
		node.Metadata.Namespace,
		node.Metadata.Namespace + ".svc",
	} {
		shortname := strings.TrimSuffix(name, "."+suffix)
		if shortname != name && strings.HasPrefix(node.DNSDomain, suffix) {
			return fmt.Sprintf("%s.%s", shortname, node.DNSDomain)
		}
	}
	return ""
}
