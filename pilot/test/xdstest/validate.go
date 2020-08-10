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
	"testing"

	cluster "github.com/envoyproxy/go-control-plane/envoy/config/cluster/v3"
	endpoint "github.com/envoyproxy/go-control-plane/envoy/config/endpoint/v3"
	listener "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	route "github.com/envoyproxy/go-control-plane/envoy/config/route/v3"

	xdsfilters "istio.io/istio/pilot/pkg/xds/filters"
)

func ValidateListeners(t testing.TB, ls []*listener.Listener) {
	for _, l := range ls {
		ValidateListener(t, l)
	}
}

func ValidateListener(t testing.TB, l *listener.Listener) {
	if err := l.Validate(); err != nil {
		t.Errorf("listener %v is invalid: %v", l.Name, err)
	}
	validateInspector(t, l)
}

// Validate a tls inspect filter is added whenever it is needed
func validateInspector(t testing.TB, l *listener.Listener) {
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
			t.Errorf("transport protocol set, but missing tls inspector")
		}
		if len(m.ServerNames) > 0 {
			t.Errorf("server names set, but missing tls inspector")
		}
		// This is a bit suspect; I suspect this could be done with just http inspector without tls inspector,
		// but this mirrors Envoy validation logic
		if len(m.ApplicationProtocols) > 0 {
			t.Errorf("application protocol, but missing tls inspector")
		}
	}
}

func ValidateClusters(t testing.TB, ls []*cluster.Cluster) {
	for _, l := range ls {
		ValidateCluster(t, l)
	}
}

func ValidateCluster(t testing.TB, l *cluster.Cluster) {
	if err := l.Validate(); err != nil {
		t.Errorf("cluster %v is invalid: %v", l.Name, err)
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
	for _, l := range ls {
		ValidateRouteConfiguration(t, l)
	}
}

func ValidateRouteConfiguration(t testing.TB, l *route.RouteConfiguration) {
	if err := l.Validate(); err != nil {
		t.Errorf("route configuration %v is invalid: %v", l.Name, err)
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
