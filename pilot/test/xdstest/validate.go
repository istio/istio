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

package xdstest

import (
	"strings"
	"testing"

	cluster "github.com/envoyproxy/go-control-plane/envoy/config/cluster/v3"
	endpoint "github.com/envoyproxy/go-control-plane/envoy/config/endpoint/v3"
	listener "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	route "github.com/envoyproxy/go-control-plane/envoy/config/route/v3"
	"github.com/google/go-cmp/cmp"
	"google.golang.org/protobuf/testing/protocmp"

	xdsfilters "istio.io/istio/pilot/pkg/xds/filters"
	"istio.io/istio/pkg/util/sets"
)

func ValidateListeners(t testing.TB, ls []*listener.Listener) {
	t.Helper()
	found := sets.New()
	for _, l := range ls {
		if found.Contains(l.Name) {
			t.Errorf("duplicate listener name %v", l.Name)
		}
		found.Insert(l.Name)
		ValidateListener(t, l)
	}
}

func ValidateListener(t testing.TB, l *listener.Listener) {
	t.Helper()
	if err := l.Validate(); err != nil {
		t.Errorf("listener %v is invalid: %v", l.Name, err)
	}
	validateInspector(t, l)
	validateListenerTLS(t, l)
	validateFilterChainMatch(t, l)
	validateInboundListener(t, l)
	validateListenerFilters(t, l)
}

func validateListenerFilters(t testing.TB, l *listener.Listener) {
	found := sets.New()
	for _, lf := range l.GetListenerFilters() {
		if found.Contains(lf.GetName()) {
			// Technically legal in Envoy but should always be a bug when done in Istio based on our usage
			t.Errorf("listener contains duplicate listener filter: %v", lf.GetName())
		}
		found.Insert(lf.GetName())
	}
}

func validateInboundListener(t testing.TB, l *listener.Listener) {
	if l.GetAddress().GetSocketAddress().GetPortValue() != 15006 {
		// Not an inbound port
		return
	}
	for i, fc := range l.GetFilterChains() {
		if fc.FilterChainMatch == nil {
			t.Errorf("nil filter chain %d", i)
			continue
		}
		if fc.FilterChainMatch.TransportProtocol == "" && fc.FilterChainMatch.GetDestinationPort().GetValue() != 15006 {
			// Not setting transport protocol may lead to unexpected matching behavior due to https://github.com/istio/istio/issues/26079
			// This is not *always* a bug, just a guideline - the 15006 blocker filter chain doesn't follow this rule and is exluced.
			t.Errorf("filter chain %d had no transport protocol set", i)
		}
	}
}

func validateFilterChainMatch(t testing.TB, l *listener.Listener) {
	t.Helper()

	// Check for duplicate filter chains, to avoid "multiple filter chains with the same matching rules are defined" error
	for i1, l1 := range l.FilterChains {
		for i2, l2 := range l.FilterChains {
			if i1 == i2 {
				continue
			}
			// We still create virtual inbound listeners before merging into single inbound
			// This hack skips these ones, as they will be processed later
			if hcm := ExtractHTTPConnectionManager(t, l1); strings.HasPrefix(hcm.GetStatPrefix(), "inbound_") && l.Name != "virtualInbound" {
				continue
			}
			if cmp.Equal(l1.FilterChainMatch, l2.FilterChainMatch, protocmp.Transform()) {
				fcms := []string{}
				for _, fc := range l.FilterChains {
					fcms = append(fcms, Dump(t, fc.GetFilterChainMatch()))
				}
				t.Errorf("overlapping filter chains %d and %d:\n%v\n Full listener: %v", i1, i2, strings.Join(fcms, ",\n"), Dump(t, l))
			}
		}
	}

	// Due to the trie based logic of FCM, an unset field is only a wildcard if no
	// other FCM sets it. Therefore, we should ensure we explicitly set the FCM on
	// all match clauses if its set on any other match clause See
	// https://github.com/envoyproxy/envoy/issues/12572 for details
	destPorts := sets.NewIntSet()
	for _, fc := range l.FilterChains {
		if fc.GetFilterChainMatch().GetDestinationPort() != nil {
			destPorts.Insert(int(fc.GetFilterChainMatch().GetDestinationPort().GetValue()))
		}
	}
	for p := range destPorts {
		hasTLSInspector := false
		for _, fc := range l.FilterChains {
			if p == int(fc.GetFilterChainMatch().GetDestinationPort().GetValue()) && fc.GetFilterChainMatch().GetTransportProtocol() != "" {
				hasTLSInspector = true
			}
		}
		if hasTLSInspector {
			for _, fc := range l.FilterChains {
				if p == int(fc.GetFilterChainMatch().GetDestinationPort().GetValue()) && fc.GetFilterChainMatch().GetTransportProtocol() == "" {
					// Note: matches [{transport=tls},{}] and [{transport=tls},{transport=buffer}]
					// are equivalent, so technically this error is overly sensitive. However, for
					// more complicated use cases its generally best to be explicit rather than
					// assuming that {} will be treated as wildcard when in reality it may not be.
					// Instead, we should explicitly double the filter chain (one for raw buffer, one
					// for TLS)
					t.Errorf("filter chain should have transport protocol set for port %v: %v", p, Dump(t, fc))
				}
			}
		}
	}
}

func validateListenerTLS(t testing.TB, l *listener.Listener) {
	t.Helper()
	for _, fc := range l.FilterChains {
		m := fc.FilterChainMatch
		if m == nil {
			continue
		}
		// if we are matching TLS traffic and doing HTTP traffic, we must terminate the TLS
		if m.TransportProtocol == xdsfilters.TLSTransportProtocol && fc.TransportSocket == nil && ExtractHTTPConnectionManager(t, fc) != nil {
			t.Errorf("listener %v is invalid: tls traffic may not be terminated: %v", l.Name, Dump(t, fc))
		}
	}
}

// Validate a tls inspect filter is added whenever it is needed
// matches logic in https://github.com/envoyproxy/envoy/blob/22683a0a24ffbb0cdeb4111eec5ec90246bec9cb/source/server/listener_impl.cc#L41
func validateInspector(t testing.TB, l *listener.Listener) {
	t.Helper()
	for _, lf := range l.ListenerFilters {
		if lf.Name == xdsfilters.TLSInspector.Name {
			return
		}
	}
	for _, fc := range l.FilterChains {
		m := fc.FilterChainMatch
		if fc.FilterChainMatch == nil {
			continue
		}
		if m.TransportProtocol == xdsfilters.TLSTransportProtocol {
			t.Errorf("transport protocol set, but missing tls inspector: %v", Dump(t, l))
		}
		if m.TransportProtocol == "" && len(m.ServerNames) > 0 {
			t.Errorf("server names set, but missing tls inspector: %v", Dump(t, l))
		}
		// This is a bit suspect; I suspect this could be done with just http inspector without tls inspector,
		// but this mirrors Envoy validation logic
		if m.TransportProtocol == "" && len(m.ApplicationProtocols) > 0 {
			t.Errorf("application protocol set, but missing tls inspector: %v", Dump(t, l))
		}
	}
}

func ValidateClusters(t testing.TB, ls []*cluster.Cluster) {
	found := sets.New()
	for _, l := range ls {
		if found.Contains(l.Name) {
			t.Errorf("duplicate cluster name %v", l.Name)
		}
		found.Insert(l.Name)
		ValidateCluster(t, l)
	}
}

func ValidateCluster(t testing.TB, c *cluster.Cluster) {
	if err := c.Validate(); err != nil {
		t.Errorf("cluster %v is invalid: %v", c.Name, err)
	}
	validateClusterTLS(t, c)
}

func validateClusterTLS(t testing.TB, c *cluster.Cluster) {
	if c.TransportSocket != nil && c.TransportSocketMatches != nil {
		t.Errorf("both transport_socket and transport_socket_matches set for %v", c)
	}
}

func ValidateRoutes(t testing.TB, ls []*route.Route) {
	for _, l := range ls {
		ValidateRoute(t, l)
	}
}

func ValidateRoute(t testing.TB, r *route.Route) {
	if err := r.Validate(); err != nil {
		t.Errorf("route %v is invalid: %v", r.Name, err)
	}
}

func ValidateRouteConfigurations(t testing.TB, ls []*route.RouteConfiguration) {
	found := sets.New()
	for _, l := range ls {
		if found.Contains(l.Name) {
			t.Errorf("duplicate route config name %v", l.Name)
		}
		found.Insert(l.Name)
		ValidateRouteConfiguration(t, l)
	}
}

func ValidateRouteConfiguration(t testing.TB, l *route.RouteConfiguration) {
	t.Helper()
	if err := l.Validate(); err != nil {
		t.Errorf("route configuration %v is invalid: %v", l.Name, err)
	}
	validateRouteConfigurationDomains(t, l)
}

func validateRouteConfigurationDomains(t testing.TB, l *route.RouteConfiguration) {
	t.Helper()

	vhosts := sets.New()
	domains := sets.New()
	for _, vhost := range l.VirtualHosts {
		if vhosts.Contains(vhost.Name) {
			t.Errorf("duplicate virtual host found %s", vhost.Name)
		}
		vhosts.Insert(vhost.Name)
		for _, domain := range vhost.Domains {
			if domains.Contains(domain) {
				t.Errorf("duplicate virtual host domain found %s", domain)
			}
			domains.Insert(domain)
		}
	}
}

func ValidateClusterLoadAssignments(t testing.TB, ls []*endpoint.ClusterLoadAssignment) {
	for _, l := range ls {
		ValidateClusterLoadAssignment(t, l)
	}
}

func ValidateClusterLoadAssignment(t testing.TB, l *endpoint.ClusterLoadAssignment) {
	if err := l.Validate(); err != nil {
		t.Errorf("cluster load assignment %v is invalid: %v", l.ClusterName, err)
	}
}
