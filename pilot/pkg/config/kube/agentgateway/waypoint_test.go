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

package agentgateway

import (
	"testing"

	"github.com/agentgateway/agentgateway/api"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	"istio.io/api/label"
	"istio.io/istio/pilot/pkg/config/kube/gatewaycommon"
	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/config/schema/gvk"
	"istio.io/istio/pkg/kube/krt"
	"istio.io/istio/pkg/kube/krt/krttest"
	"istio.io/istio/pkg/ptr"
	"istio.io/istio/pkg/slices"
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/util/assert"
)

func init() {
	// The agentgateway and waypoint classes are only registered when these features are enabled.
	// Recompute the package-level class map so the waypoint class is resolvable in tests.
	features.EnableAgentgateway = true
	features.EnableAmbientWaypoints = true
	gatewaycommon.AgentgatewayClasses = gatewaycommon.GetAgentGatewayClasses()
}

var hboneProtocol = gatewayv1.ProtocolType(protocol.HBONE)

func TestUnexpectedWaypointListener(t *testing.T) {
	tests := []struct {
		name string
		l    gatewayv1.Listener
		want bool
	}{
		{
			name: "valid HBONE on 15008",
			l:    gatewayv1.Listener{Port: 15008, Protocol: hboneProtocol},
			want: false,
		},
		{
			name: "wrong port",
			l:    gatewayv1.Listener{Port: 8080, Protocol: hboneProtocol},
			want: true,
		},
		{
			name: "wrong protocol",
			l:    gatewayv1.Listener{Port: 15008, Protocol: gatewayv1.HTTPProtocolType},
			want: true,
		},
		{
			name: "wrong port and protocol",
			l:    gatewayv1.Listener{Port: 80, Protocol: gatewayv1.HTTPProtocolType},
			want: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, unexpectedWaypointListener(tt.l), tt.want)
		})
	}
}

func TestListenerProtocolToIstio(t *testing.T) {
	tests := []struct {
		name       string
		controller gatewayv1.GatewayController
		protocol   gatewayv1.ProtocolType
		alphaAPI   bool
		want       string
		wantErr    bool
	}{
		{
			name:       "HTTP",
			controller: constants.ManagedAgentgatewayController,
			protocol:   gatewayv1.HTTPProtocolType,
			want:       "HTTP",
		},
		{
			name:       "HTTPS",
			controller: constants.ManagedAgentgatewayController,
			protocol:   gatewayv1.HTTPSProtocolType,
			want:       "HTTPS",
		},
		{
			name:       "TLS",
			controller: constants.ManagedAgentgatewayController,
			protocol:   gatewayv1.TLSProtocolType,
			want:       "TLS",
		},
		{
			name:       "HBONE allowed for agentgateway waypoint controller",
			controller: constants.ManagedAgentgatewayWaypointController,
			protocol:   hboneProtocol,
			want:       string(hboneProtocol),
		},
		{
			name:       "HBONE allowed for agentgateway controller",
			controller: constants.ManagedAgentgatewayController,
			protocol:   hboneProtocol,
			want:       string(hboneProtocol),
		},
		{
			name:       "HBONE rejected for unrelated controller",
			controller: gatewayv1.GatewayController("example.com/other"),
			protocol:   hboneProtocol,
			wantErr:    true,
		},
		{
			name:       "TCP allowed when alpha API enabled",
			controller: constants.ManagedAgentgatewayController,
			protocol:   gatewayv1.TCPProtocolType,
			alphaAPI:   true,
			want:       "TCP",
		},
		{
			name:       "TCP rejected when alpha API disabled",
			controller: constants.ManagedAgentgatewayController,
			protocol:   gatewayv1.TCPProtocolType,
			alphaAPI:   false,
			wantErr:    true,
		},
		{
			name:       "lowercase protocol returns uppercase hint",
			controller: constants.ManagedAgentgatewayController,
			protocol:   gatewayv1.ProtocolType("http"),
			wantErr:    true,
		},
		{
			name:       "unsupported protocol",
			controller: constants.ManagedAgentgatewayController,
			protocol:   gatewayv1.UDPProtocolType,
			wantErr:    true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			test.SetForTest(t, &features.EnableAlphaGatewayAPI, tt.alphaAPI)
			got, err := listenerProtocolToIstio(tt.controller, tt.protocol)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			assert.NoError(t, err)
			assert.Equal(t, got, tt.want)
		})
	}
}

func TestGetTunnelProtocol(t *testing.T) {
	c := &Controller{}
	tests := []struct {
		name      string
		protocol  gatewayv1.ProtocolType
		className string
		want      api.Bind_TunnelProtocol
	}{
		{
			name:      "HBONE waypoint uses HBONE_WAYPOINT",
			protocol:  hboneProtocol,
			className: constants.AgentgatewayWaypointClassName,
			want:      api.Bind_HBONE_WAYPOINT,
		},
		{
			name:      "HBONE non-waypoint uses HBONE_GATEWAY",
			protocol:  hboneProtocol,
			className: constants.AgentgatewayClassName,
			want:      api.Bind_HBONE_GATEWAY,
		},
		{
			name:      "non-HBONE uses DIRECT",
			protocol:  gatewayv1.HTTPProtocolType,
			className: constants.AgentgatewayClassName,
			want:      api.Bind_DIRECT,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			obj := &GatewayListener{
				ParentInfo: AgwParentInfo{
					Protocol:               tt.protocol,
					ParentGatewayClassName: tt.className,
				},
			}
			assert.Equal(t, c.getTunnelProtocol(obj), tt.want)
		})
	}
}

func TestGetBindProtocol(t *testing.T) {
	c := &Controller{}
	tests := []struct {
		name     string
		protocol gatewayv1.ProtocolType
		want     api.Bind_Protocol
	}{
		{name: "HTTP", protocol: gatewayv1.HTTPProtocolType, want: api.Bind_HTTP},
		{name: "HTTPS", protocol: gatewayv1.HTTPSProtocolType, want: api.Bind_TLS},
		{name: "TLS", protocol: gatewayv1.TLSProtocolType, want: api.Bind_TLS},
		{name: "TCP", protocol: gatewayv1.TCPProtocolType, want: api.Bind_TCP},
		{name: "HBONE placeholder is HTTP", protocol: hboneProtocol, want: api.Bind_HTTP},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			obj := &GatewayListener{ParentInfo: AgwParentInfo{Protocol: tt.protocol}}
			assert.Equal(t, c.getBindProtocol(obj), tt.want)
		})
	}
}

func TestAgwParentInfoIsWaypoint(t *testing.T) {
	tests := []struct {
		name      string
		className string
		want      bool
	}{
		{name: "waypoint class", className: constants.AgentgatewayWaypointClassName, want: true},
		{name: "regular agentgateway class", className: constants.AgentgatewayClassName, want: false},
		{name: "empty class", className: "", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			info := AgwParentInfo{ParentGatewayClassName: tt.className}
			assert.Equal(t, info.IsWaypoint(), tt.want)
		})
	}
}

// test helpers for building inputs. All inputs live in the "ns1" namespace.

func testService(labels map[string]string) *corev1.Service {
	return &corev1.Service{ObjectMeta: metav1.ObjectMeta{Namespace: "ns1", Name: "svc1", Labels: labels}}
}

func testNamespace(labels map[string]string) *corev1.Namespace {
	return &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "ns1", Labels: labels}}
}

func testGateway(name, class string) *gatewayv1.Gateway {
	return &gatewayv1.Gateway{
		ObjectMeta: metav1.ObjectMeta{Namespace: "ns1", Name: name},
		Spec:       gatewayv1.GatewaySpec{GatewayClassName: gatewayv1.ObjectName(class)},
	}
}

func staticCol[T any](opts krt.OptionsBuilder, name string, items ...T) krt.Collection[T] {
	return krt.NewStaticCollection(nil, items, opts.WithName(name)...)
}

func TestBuildWaypointServiceBindings(t *testing.T) {
	useWaypoint := func(wp string) map[string]string {
		return map[string]string{label.IoIstioUseWaypoint.Name: wp}
	}
	tests := []struct {
		name       string
		services   []*corev1.Service
		namespaces []*corev1.Namespace
		gateways   []*gatewayv1.Gateway
		want       []WaypointServiceBinding
	}{
		{
			name:       "service labeled with existing waypoint gateway",
			services:   []*corev1.Service{testService(useWaypoint("wp"))},
			namespaces: []*corev1.Namespace{testNamespace(nil)},
			gateways:   []*gatewayv1.Gateway{testGateway("wp", constants.AgentgatewayWaypointClassName)},
			want: []WaypointServiceBinding{{
				ServiceKey:      types.NamespacedName{Namespace: "ns1", Name: "svc1"},
				WaypointGateway: types.NamespacedName{Namespace: "ns1", Name: "wp"},
			}},
		},
		{
			name:       "service without label produces no binding",
			services:   []*corev1.Service{testService(nil)},
			namespaces: []*corev1.Namespace{testNamespace(nil)},
			gateways:   []*gatewayv1.Gateway{testGateway("wp", constants.AgentgatewayWaypointClassName)},
			want:       nil,
		},
		{
			name:       "labeled service but waypoint gateway missing",
			services:   []*corev1.Service{testService(useWaypoint("wp"))},
			namespaces: []*corev1.Namespace{testNamespace(nil)},
			gateways:   nil,
			want:       nil,
		},
		{
			name:       "labeled service but gateway is not a waypoint class",
			services:   []*corev1.Service{testService(useWaypoint("gw"))},
			namespaces: []*corev1.Namespace{testNamespace(nil)},
			gateways:   []*gatewayv1.Gateway{testGateway("gw", constants.AgentgatewayClassName)},
			want:       nil,
		},
		{
			name:       "namespace label inherited by service",
			services:   []*corev1.Service{testService(nil)},
			namespaces: []*corev1.Namespace{testNamespace(useWaypoint("wp"))},
			gateways:   []*gatewayv1.Gateway{testGateway("wp", constants.AgentgatewayWaypointClassName)},
			want: []WaypointServiceBinding{{
				ServiceKey:      types.NamespacedName{Namespace: "ns1", Name: "svc1"},
				WaypointGateway: types.NamespacedName{Namespace: "ns1", Name: "wp"},
			}},
		},
		{
			name:       "use-waypoint none opts out",
			services:   []*corev1.Service{testService(useWaypoint("none"))},
			namespaces: []*corev1.Namespace{testNamespace(useWaypoint("wp"))},
			gateways:   []*gatewayv1.Gateway{testGateway("wp", constants.AgentgatewayWaypointClassName)},
			want:       nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts := krttest.Options(t)
			gatewayClasses := staticCol[gatewaycommon.GatewayClass](opts, "GatewayClasses")
			bindings := BuildWaypointServiceBindings(
				staticCol(opts, "Services", tt.services...),
				staticCol(opts, "Namespaces", tt.namespaces...),
				staticCol(opts, "Gateways", tt.gateways...),
				gatewayClasses,
				opts,
			)
			bindings.WaitUntilSynced(test.NewStop(t))
			got := slices.SortBy(bindings.List(), func(b WaypointServiceBinding) string { return b.ResourceName() })
			assert.Equal(t, got, tt.want)
		})
	}
}

func TestExtractWaypointGatewayRefs(t *testing.T) {
	opts := krttest.Options(t)
	bindings := staticCol(opts, "Bindings",
		WaypointServiceBinding{
			ServiceKey:      types.NamespacedName{Namespace: "ns1", Name: "svc1"},
			WaypointGateway: types.NamespacedName{Namespace: "ns1", Name: "wp1"},
		},
		WaypointServiceBinding{
			ServiceKey:      types.NamespacedName{Namespace: "ns2", Name: "svc2"},
			WaypointGateway: types.NamespacedName{Namespace: "ns2", Name: "wp2"},
		},
	)
	bindings.WaitUntilSynced(test.NewStop(t))
	idx := krt.NewIndex(bindings, "by-service", func(b WaypointServiceBinding) []types.NamespacedName {
		return []types.NamespacedName{b.ServiceKey}
	})

	serviceRef := func(ns, name string) gatewayv1.ParentReference {
		ref := gatewayv1.ParentReference{Kind: ptr.Of(gatewayv1.Kind("Service")), Name: gatewayv1.ObjectName(name)}
		if ns != "" {
			ref.Namespace = ptr.Of(gatewayv1.Namespace(ns))
		}
		return ref
	}

	tests := []struct {
		name      string
		defaultNS string
		refs      []gatewayv1.ParentReference
		want      []types.NamespacedName
	}{
		{
			name:      "service ref in default namespace resolves to waypoint",
			defaultNS: "ns1",
			refs:      []gatewayv1.ParentReference{serviceRef("", "svc1")},
			want:      []types.NamespacedName{{Namespace: "ns1", Name: "wp1"}},
		},
		{
			name:      "cross-namespace service ref resolves to waypoint",
			defaultNS: "ns1",
			refs:      []gatewayv1.ParentReference{serviceRef("ns2", "svc2")},
			want:      []types.NamespacedName{{Namespace: "ns2", Name: "wp2"}},
		},
		{
			name:      "gateway kind ref is ignored",
			defaultNS: "ns1",
			refs:      []gatewayv1.ParentReference{{Kind: ptr.Of(gatewayv1.Kind("Gateway")), Name: "gw"}},
			want:      nil,
		},
		{
			name:      "service ref with non-core group is ignored",
			defaultNS: "ns1",
			refs: []gatewayv1.ParentReference{{
				Group: ptr.Of(gatewayv1.Group("example.com")),
				Kind:  ptr.Of(gatewayv1.Kind("Service")),
				Name:  "svc1",
			}},
			want: nil,
		},
		{
			name:      "service ref without binding resolves to nothing",
			defaultNS: "ns1",
			refs:      []gatewayv1.ParentReference{serviceRef("", "unbound")},
			want:      nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractWaypointGatewayRefs(krt.TestingDummyContext{}, tt.defaultNS, tt.refs, idx)
			gotList := slices.SortBy(got.UnsortedList(), func(n types.NamespacedName) string { return n.String() })
			assert.Equal(t, gotList, tt.want)
		})
	}
}

func TestRouteParentsFetchServiceParent(t *testing.T) {
	opts := krttest.Options(t)
	wpParentInfo := AgwParentInfo{
		ParentGateway:          types.NamespacedName{Namespace: "ns1", Name: "wp"},
		ParentGatewayClassName: constants.AgentgatewayWaypointClassName,
		Protocol:               hboneProtocol,
		Port:                   15008,
	}
	wpKey := AgwParentKey{Kind: gvk.KubernetesGateway.Kubernetes(), Namespace: "ns1", Name: "wp"}
	gateways := staticCol(opts, "Gateways", &GatewayListener{
		Name:          "ns1/wp",
		ParentGateway: types.NamespacedName{Namespace: "ns1", Name: "wp"},
		ParentObject:  wpKey,
		ParentInfo:    wpParentInfo,
		Valid:         true,
	})
	gateways.WaitUntilSynced(test.NewStop(t))
	bindings := staticCol(opts, "Bindings", WaypointServiceBinding{
		ServiceKey:      types.NamespacedName{Namespace: "ns1", Name: "svc1"},
		WaypointGateway: types.NamespacedName{Namespace: "ns1", Name: "wp"},
	})
	bindings.WaitUntilSynced(test.NewStop(t))
	parents := BuildRouteParents(gateways, bindings)

	t.Run("service parent resolves to waypoint listeners", func(t *testing.T) {
		svcKey := AgwParentKey{Kind: gvk.Service.Kubernetes(), Namespace: "ns1", Name: "svc1"}
		got := parents.fetch(krt.TestingDummyContext{}, svcKey)
		assert.Equal(t, len(got), 1)
		assert.Equal(t, got[0].ParentGateway, types.NamespacedName{Namespace: "ns1", Name: "wp"})
	})

	t.Run("service parent without binding resolves to nothing", func(t *testing.T) {
		svcKey := AgwParentKey{Kind: gvk.Service.Kubernetes(), Namespace: "ns1", Name: "other"}
		got := parents.fetch(krt.TestingDummyContext{}, svcKey)
		assert.Equal(t, len(got), 0)
	})

	t.Run("gateway parent resolves via gateway index", func(t *testing.T) {
		got := parents.fetch(krt.TestingDummyContext{}, wpKey)
		assert.Equal(t, len(got), 1)
		assert.Equal(t, got[0].ParentGateway, types.NamespacedName{Namespace: "ns1", Name: "wp"})
	})
}

func TestBuildAncestorBackends(t *testing.T) {
	gatewayRef := gatewayv1.ParentReference{Kind: ptr.Of(gatewayv1.Kind("Gateway")), Name: "gw"}
	serviceRef := gatewayv1.ParentReference{Kind: ptr.Of(gatewayv1.Kind("Service")), Name: "svc1"}
	backend := func(name string, kind *gatewayv1.Kind) gatewayv1.BackendRef {
		return gatewayv1.BackendRef{BackendObjectReference: gatewayv1.BackendObjectReference{Name: gatewayv1.ObjectName(name), Kind: kind}}
	}

	httpRoute := &gatewayv1.HTTPRoute{
		ObjectMeta: metav1.ObjectMeta{Namespace: "ns1", Name: "httproute"},
		Spec: gatewayv1.HTTPRouteSpec{
			// The route targets a Service that is assigned (via use-waypoint) to a waypoint gateway.
			// It must resolve to that waypoint gateway, not a directly-referenced Gateway.
			CommonRouteSpec: gatewayv1.CommonRouteSpec{ParentRefs: []gatewayv1.ParentReference{serviceRef}},
			Rules: []gatewayv1.HTTPRouteRule{{
				BackendRefs: []gatewayv1.HTTPBackendRef{
					{BackendRef: backend("be", nil)},
					{BackendRef: backend("ignored", ptr.Of(gatewayv1.Kind("ServiceImport")))},
				},
			}},
		},
	}
	grpcRoute := &gatewayv1.GRPCRoute{
		ObjectMeta: metav1.ObjectMeta{Namespace: "ns1", Name: "grpcroute"},
		Spec: gatewayv1.GRPCRouteSpec{
			CommonRouteSpec: gatewayv1.CommonRouteSpec{ParentRefs: []gatewayv1.ParentReference{gatewayRef}},
			Rules: []gatewayv1.GRPCRouteRule{{
				BackendRefs: []gatewayv1.GRPCBackendRef{{BackendRef: backend("be", nil)}},
			}},
		},
	}

	opts := krttest.Options(t)
	bindings := staticCol(opts, "Bindings", WaypointServiceBinding{
		ServiceKey:      types.NamespacedName{Namespace: "ns1", Name: "svc1"},
		WaypointGateway: types.NamespacedName{Namespace: "ns1", Name: "wp"},
	})
	bindings.WaitUntilSynced(test.NewStop(t))

	ancestors := BuildAncestorBackends(
		staticCol(opts, "HTTPRoutes", httpRoute),
		staticCol(opts, "GRPCRoutes", grpcRoute),
		bindings,
		opts,
	)
	ancestors.WaitUntilSynced(test.NewStop(t))

	got := slices.Sort(slices.Map(ancestors.List(), func(a *AncestorBackend) string { return a.ResourceName() }))

	gw := types.NamespacedName{Namespace: "ns1", Name: "gw"}
	wp := types.NamespacedName{Namespace: "ns1", Name: "wp"}
	be := types.NamespacedName{Namespace: "ns1", Name: "be"}
	httpSrc := TypedResource{Kind: gvk.HTTPRoute, Name: types.NamespacedName{Namespace: "ns1", Name: "httproute"}}
	grpcSrc := TypedResource{Kind: gvk.GRPCRoute, Name: types.NamespacedName{Namespace: "ns1", Name: "grpcroute"}}

	want := slices.Sort([]string{
		// HTTPRoute: Service parentRef resolved to the waypoint gateway, paired with the be backend.
		AncestorBackend{Gateway: wp, Backend: be, Source: httpSrc}.ResourceName(),
		// GRPCRoute: Gateway parentRef paired with the be backend.
		AncestorBackend{Gateway: gw, Backend: be, Source: grpcSrc}.ResourceName(),
	})
	assert.Equal(t, got, want)
}
