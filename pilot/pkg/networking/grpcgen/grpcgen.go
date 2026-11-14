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
	"strings"

	tls "github.com/envoyproxy/go-control-plane/envoy/extensions/transport_sockets/tls/v3"
	matcher "github.com/envoyproxy/go-control-plane/envoy/type/matcher/v3"

	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/util"
	"istio.io/istio/pilot/pkg/xds"
	v3 "istio.io/istio/pilot/pkg/xds/v3"
	"istio.io/istio/pkg/config/host"
	"istio.io/istio/pkg/config/schema/kind"
	"istio.io/istio/pkg/istio-agent/grpcxds"
	istiolog "istio.io/istio/pkg/log"
	"istio.io/istio/pkg/util/sets"
)

// Support generation of 'ApiListener' LDS responses, used for native support of gRPC.
// The same response can also be used by other apps using XDS directly.

// GRPC proposal:
// https://github.com/grpc/proposal/blob/master/A27-xds-global-load-balancing.md
//
// Note that this implementation is tested against gRPC, but it is generic - any other framework can
// use this XDS mode to get load balancing info from Istio, including MC/VM/etc.

// The corresponding RDS response is also generated - currently gRPC has special differences
// and can't understand normal Istio RDS - in particular expects "" instead of "/" as
// default prefix, and is expects just the route for one host.
// handleAck will detect if the message is an ACK or NACK, and update/log/count
// using the generic structures. "Classical" CDS/LDS/RDS/EDS use separate logic -
// this is used for the API-based LDS and generic messages.

var log = istiolog.RegisterScope("grpcgen", "xDS Generator for Proxyless gRPC")

type GrpcConfigGenerator struct{}

func clusterKey(hostname string, port int) string {
	return subsetClusterKey("", hostname, port)
}

func subsetClusterKey(subset, hostname string, port int) string {
	return model.BuildSubsetKey(model.TrafficDirectionOutbound, subset, host.Name(hostname), port)
}

func (g *GrpcConfigGenerator) Generate(proxy *model.Proxy, w *model.WatchedResource, req *model.PushRequest) (model.Resources, model.XdsLogDetails, error) {
	// Scope the update to the watched hostnames before the Envoy predicates decide.
	switch w.TypeUrl {
	case v3.ListenerType:
		if !xds.LdsNeedsPush(proxy, scopePushRequest(proxy, w, req)) {
			return nil, model.DefaultXdsLogDetails, nil
		}
		return g.BuildListeners(proxy, req.Push, w.ResourceNames.UnsortedList()), model.DefaultXdsLogDetails, nil
	case v3.ClusterType:
		scoped := scopePushRequest(proxy, w, req)
		_, needsPush := xds.CdsNeedsPush(scoped, proxy)
		// Cluster SANs come from the service accounts, which arrive as a ServiceEntry update with an
		// endpoint reason. The Envoy predicate skips those during headless endpoint updates.
		if !needsPush && !model.HasConfigsOfKind(scoped.ConfigsUpdated, kind.ServiceEntry) {
			return nil, model.DefaultXdsLogDetails, nil
		}
		return g.BuildClusters(proxy, req.Push, w.ResourceNames.UnsortedList()), model.DefaultXdsLogDetails, nil
	case v3.RouteType:
		if !xds.RdsNeedsPush(scopePushRequest(proxy, w, req), proxy) {
			return nil, model.DefaultXdsLogDetails, nil
		}
		return g.BuildHTTPRoutes(proxy, req.Push, w.ResourceNames.UnsortedList()), model.DefaultXdsLogDetails, nil
	}

	return nil, model.DefaultXdsLogDetails, nil
}

// scopePushRequest drops service and destination rule updates that cannot change the resources in
// w. Proxyless gRPC clients subscribe by hostname and the generators build only what they name.
// Forced pushes, wildcard watches and names without a hostname are not scoped.
func scopePushRequest(proxy *model.Proxy, w *model.WatchedResource, req *model.PushRequest) *model.PushRequest {
	if req == nil || req.Forced || len(req.ConfigsUpdated) == 0 {
		return req
	}
	hosts := watchedHostnames(proxy, w)
	if hosts == nil {
		return req
	}
	scoped := sets.New[model.ConfigKey]()
	for cfg := range req.ConfigsUpdated {
		switch cfg.Kind {
		case kind.ServiceEntry, kind.Endpoints:
			// Services in the proxy's own namespace shape its inbound listeners.
			if cfg.Namespace != proxy.Metadata.Namespace && !hosts.Contains(host.Name(cfg.Name)) && !aliasWatched(proxy, cfg, hosts) {
				continue
			}
		case kind.DestinationRule:
			// Routes take hash policies from the destination rules of their destinations.
			if w.TypeUrl != v3.RouteType && !ruleMatchesHosts(proxy, cfg, hosts) {
				continue
			}
		}
		scoped.Insert(cfg)
	}
	if len(scoped) == len(req.ConfigsUpdated) {
		return req
	}
	out := *req
	out.ConfigsUpdated = scoped
	return &out
}

// watchedHostnames returns the hostnames w subscribes to, or nil when it cannot be reduced to hostnames.
func watchedHostnames(proxy *model.Proxy, w *model.WatchedResource) sets.Set[host.Name] {
	if w == nil || len(w.ResourceNames) == 0 {
		return nil
	}
	hosts := sets.NewWithLength[host.Name](len(w.ResourceNames))
	switch w.TypeUrl {
	case v3.ListenerType:
		// Server listeners carry an address, not a hostname; the own-namespace rule covers them.
		for name := range newListenerNameFilter(w.ResourceNames.UnsortedList(), proxy) {
			if !strings.HasPrefix(name, grpcxds.ServerListenerNamePrefix) {
				hosts.Insert(host.Name(name))
			}
		}
	case v3.ClusterType, v3.RouteType:
		for name := range w.ResourceNames {
			_, _, hostname, _ := model.ParseSubsetKey(name)
			if hostname == "" {
				return nil
			}
			hosts.Insert(hostname)
		}
	default:
		return nil
	}
	return hosts
}

// aliasWatched reports whether the updated service has an alias in hosts. A deleted service is only
// in the previous sidecar scope.
func aliasWatched(proxy *model.Proxy, cfg model.ConfigKey, hosts sets.Set[host.Name]) bool {
	for _, scope := range []*model.SidecarScope{proxy.SidecarScope, proxy.PrevSidecarScope} {
		if svc := scope.GetService(host.Name(cfg.Name)); svc != nil {
			for _, alias := range svc.Attributes.Aliases {
				if hosts.Contains(alias.Hostname) {
					return true
				}
			}
		}
	}
	return false
}

// ruleMatchesHosts matches the rule's host in the current and previous scope, so a changed or deleted
// rule matches by its old host. An unknown rule counts as relevant.
func ruleMatchesHosts(proxy *model.Proxy, cfg model.ConfigKey, hosts sets.Set[host.Name]) bool {
	found := false
	for _, scope := range []*model.SidecarScope{proxy.SidecarScope, proxy.PrevSidecarScope} {
		rule := scope.DestinationRuleByName(cfg.Name, cfg.Namespace)
		if rule == nil {
			continue
		}
		found = true
		ruleHost := host.Name(rule.Spec.(*networking.DestinationRule).Host)
		for hostname := range hosts {
			if ruleHost.Matches(hostname) {
				return true
			}
		}
	}
	return !found
}

// buildCommonTLSContext creates a TLS context that assumes 'default' name, and credentials/tls/certprovider/pemfile
// (see grpc/xds/internal/client/xds.go securityConfigFromCluster).
//
// This function sends both current and deprecated xDS certificate provider fields for backward compatibility:
// - Current fields (field 14, ca_certificate_provider_instance): supported by grpc-go >= 1.66, grpc-cpp >= 1.66
// - Deprecated fields (field 11, field 4): for older gRPC clients
//
// The deprecated fields will be removed in a future release. See https://github.com/istio/istio/issues/TBD
func buildCommonTLSContext(sans []string) *tls.CommonTlsContext {
	var sanMatch []*matcher.StringMatcher
	if len(sans) > 0 {
		sanMatch = util.StringToExactMatch(sans)
	}

	// Create certificate provider instances for deprecated fields (field 11 and field 4)
	// These use the CommonTlsContext_CertificateProviderInstance type
	deprecatedCertProviderInstance := &tls.CommonTlsContext_CertificateProviderInstance{
		InstanceName:    "default",
		CertificateName: "default",
	}

	deprecatedCaProviderInstance := &tls.CommonTlsContext_CertificateProviderInstance{
		InstanceName:    "default",
		CertificateName: "ROOTCA",
	}

	// Create certificate provider instances for current fields (field 14 and ca_certificate_provider_instance)
	// These use the CertificateProviderPluginInstance type
	currentCertProviderInstance := &tls.CertificateProviderPluginInstance{
		InstanceName:    "default",
		CertificateName: "default",
	}

	currentCaProviderInstance := &tls.CertificateProviderPluginInstance{
		InstanceName:    "default",
		CertificateName: "ROOTCA",
	}

	return &tls.CommonTlsContext{
		// Current field (field 14) - introduced in Envoy 1.28
		// Supported by grpc-go >= 1.66, grpc-cpp >= 1.66, modern grpc-java
		// Uses CertificateProviderPluginInstance type
		TlsCertificateProviderInstance: currentCertProviderInstance,

		// DEPRECATED field (field 11) - kept for backward compatibility with older gRPC clients
		// TODO: Remove this field after 2 releases (when all users have upgraded gRPC clients)
		// See https://github.com/grpc/grpc-java/commit/65d0bb8a4d9ee111f1eeb02c43321bbb99143151
		// Uses CommonTlsContext_CertificateProviderInstance type
		TlsCertificateCertificateProviderInstance: deprecatedCertProviderInstance,

		ValidationContextType: &tls.CommonTlsContext_CombinedValidationContext{
			CombinedValidationContext: &tls.CommonTlsContext_CombinedCertificateValidationContext{
				// DEPRECATED field (field 4) - kept for backward compatibility with older gRPC clients
				// TODO: Remove this field after 2 releases (when all users have upgraded gRPC clients)
				// Uses CommonTlsContext_CertificateProviderInstance type
				ValidationContextCertificateProviderInstance: deprecatedCaProviderInstance,

				DefaultValidationContext: &tls.CertificateValidationContext{
					MatchSubjectAltNames: sanMatch,

					// Current field - CA certificate provider in default_validation_context
					// This is the recommended way to specify CA certificates per the xDS specification
					// Uses CertificateProviderPluginInstance type
					CaCertificateProviderInstance: currentCaProviderInstance,
				},
			},
		},
	}
}
