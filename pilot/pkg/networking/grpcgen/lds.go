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
	rbacpb "github.com/envoyproxy/go-control-plane/envoy/config/rbac/v3"
	route "github.com/envoyproxy/go-control-plane/envoy/config/route/v3"
	rbachttppb "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/rbac/v3"
	hcm "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/http_connection_manager/v3"
	tls "github.com/envoyproxy/go-control-plane/envoy/extensions/transport_sockets/tls/v3"
	discovery "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"
	wrappers "google.golang.org/protobuf/types/known/wrapperspb"

	"istio.io/api/label"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/security/authn"
	"istio.io/istio/pilot/pkg/security/authn/factory"
	authzmodel "istio.io/istio/pilot/pkg/security/authz/model"
	"istio.io/istio/pilot/pkg/util/protoconv"
	xdsfilters "istio.io/istio/pilot/pkg/xds/filters"
	"istio.io/istio/pkg/istio-agent/grpcxds"
	"istio.io/istio/pkg/util/sets"
)

var supportedFilters = []*hcm.HttpFilter{
	xdsfilters.Fault,
	xdsfilters.Router,
}

const (
	RBACHTTPFilterName     = "envoy.filters.http.rbac"
	RBACHTTPFilterNameDeny = "envoy.filters.http.rbac.DENY"
)

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
	policyApplier := factory.NewPolicyApplier(push, node.Metadata.Namespace, node.Labels)
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
			Resource: protoconv.MessageToAny(ll),
		})
	}
	return out
}

// nolint: unparam
func buildInboundFilterChains(node *model.Proxy, push *model.PushContext, si *model.ServiceInstance, applier authn.PolicyApplier) []*listener.FilterChain {
	mode := applier.GetMutualTLSModeForPort(si.Endpoint.EndpointPort)

	// auto-mtls label is set - clients will attempt to connect using mtls, and
	// gRPC doesn't support permissive.
	if node.Labels[label.SecurityTlsMode.Name] == "istio" && mode == model.MTLSPermissive {
		mode = model.MTLSStrict
	}

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
		// No need to warn on each push - the behavior is still consistent with auto-mtls, which is the
		// replacement for permissive.
		mode = model.MTLSDisable
	}

	var out []*listener.FilterChain
	switch mode {
	case model.MTLSDisable:
		out = append(out, buildInboundFilterChain(node, push, "plaintext", nil))
	case model.MTLSStrict:
		out = append(out, buildInboundFilterChain(node, push, "mtls", tlsContext))
		// TODO permissive builts both plaintext and mtls; when tlsContext is present add a match for protocol
	}

	return out
}

func buildInboundFilterChain(node *model.Proxy, push *model.PushContext, nameSuffix string, tlsContext *tls.DownstreamTlsContext) *listener.FilterChain {
	fc := []*hcm.HttpFilter{}
	// See security/authz/builder and grpc internal/xds/rbac
	// grpc supports ALLOW and DENY actions (fail if it is not one of them), so we can't use the normal generator
	policies := push.AuthzPolicies.ListAuthorizationPolicies(node.ConfigNamespace, node.Labels)
	if len(policies.Deny)+len(policies.Allow) > 0 {
		rules := buildRBAC(node, push, nameSuffix, tlsContext, rbacpb.RBAC_DENY, policies.Deny)
		if rules != nil && len(rules.Policies) > 0 {
			rbac := &rbachttppb.RBAC{
				Rules: rules,
			}
			fc = append(fc,
				&hcm.HttpFilter{
					Name:       RBACHTTPFilterNameDeny,
					ConfigType: &hcm.HttpFilter_TypedConfig{TypedConfig: protoconv.MessageToAny(rbac)},
				})
		}
		arules := buildRBAC(node, push, nameSuffix, tlsContext, rbacpb.RBAC_ALLOW, policies.Allow)
		if arules != nil && len(arules.Policies) > 0 {
			rbac := &rbachttppb.RBAC{
				Rules: arules,
			}
			fc = append(fc,
				&hcm.HttpFilter{
					Name:       RBACHTTPFilterName,
					ConfigType: &hcm.HttpFilter_TypedConfig{TypedConfig: protoconv.MessageToAny(rbac)},
				})
		}
	}

	// Must be last
	fc = append(fc, xdsfilters.Router)

	out := &listener.FilterChain{
		Name:             "inbound-" + nameSuffix,
		FilterChainMatch: nil,
		Filters: []*listener.Filter{{
			Name: "inbound-hcm" + nameSuffix,
			ConfigType: &listener.Filter_TypedConfig{
				TypedConfig: protoconv.MessageToAny(&hcm.HttpConnectionManager{
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
					HttpFilters: fc,
				}),
			},
		}},
	}
	if tlsContext != nil {
		out.TransportSocket = &core.TransportSocket{
			Name:       transportSocketName,
			ConfigType: &core.TransportSocket_TypedConfig{TypedConfig: protoconv.MessageToAny(tlsContext)},
		}
	}
	return out
}

// buildRBAC builds the RBAC config expected by gRPC.
//
// See: xds/interal/httpfilter/rbac
//
// TODO: gRPC also supports 'per route override' - not yet clear how to use it, Istio uses path expressions instead and we don't generate
// vhosts or routes for the inbound listener.
//
// For gateways it would make a lot of sense to use this concept, same for moving path prefix at top level ( more scalable, easier for users)
// This should probably be done for the v2 API.
//
// nolint: unparam
func buildRBAC(node *model.Proxy, push *model.PushContext, suffix string, context *tls.DownstreamTlsContext,
	a rbacpb.RBAC_Action, policies []model.AuthorizationPolicy,
) *rbacpb.RBAC {
	rules := &rbacpb.RBAC{
		Action:   a,
		Policies: map[string]*rbacpb.Policy{},
	}
	for _, policy := range policies {
		for i, rule := range policy.Spec.Rules {
			name := fmt.Sprintf("%s-%s-%d", policy.Namespace, policy.Name, i)
			m, err := authzmodel.New(rule)
			if err != nil {
				log.Warn("Invalid rule ", rule, err)
				continue
			}
			generated, _ := m.Generate(false, a)
			rules.Policies[name] = generated
		}
	}

	return rules
}

// nolint: unparam
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
						ApiListener: protoconv.MessageToAny(&hcm.HttpConnectionManager{
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
					Resource: protoconv.MessageToAny(ll),
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
		return listenerName{RequestedNames: sets.New(s)}, true
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
			filter[name] = listenerName{RequestedNames: sets.New(name)}
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
				ln = listenerName{RequestedNames: sets.New()}
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
