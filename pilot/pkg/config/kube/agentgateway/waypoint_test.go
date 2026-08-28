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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	"istio.io/istio/pilot/pkg/config/kube/gatewaycommon"
	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/serviceregistry/ambient"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/config/schema/gvk"
	"istio.io/istio/pkg/config/schema/kind"
	"istio.io/istio/pkg/kube/krt"
	"istio.io/istio/pkg/kube/krt/krttest"
	"istio.io/istio/pkg/ptr"
	"istio.io/istio/pkg/slices"
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/util/assert"
	"istio.io/istio/pkg/workloadapi"
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

// test helpers for building inputs. All inputs live in the "ns1" namespace unless a
// namespaced variant is used.

func testGateway(name, class string) *gatewayv1.Gateway {
	return testGatewayIn("ns1", name, class)
}

func testGatewayIn(namespace, name, class string) *gatewayv1.Gateway {
	return &gatewayv1.Gateway{
		ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: name},
		Spec:       gatewayv1.GatewaySpec{GatewayClassName: gatewayv1.ObjectName(class)},
	}
}

// testServiceInfo builds the ambient projection of a "ns1"-namespace k8s Service. waypointRef
// mirrors what ambient's ServicesCollection writes when a use-waypoint label resolves; it is
// either "" or "namespace/name" of the target waypoint Gateway.
func testServiceInfo(name, waypointRef string) model.ServiceInfo {
	return testServiceInfoIn("ns1", name, kind.Service, waypointRef)
}

func testServiceInfoIn(namespace, name string, srcKind kind.Kind, waypointRef string) model.ServiceInfo {
	return model.ServiceInfo{
		Service: &workloadapi.Service{Namespace: namespace, Name: name},
		Source:  model.TypedObject{Kind: srcKind},
		Waypoint: model.WaypointBindingStatus{
			ResourceName: waypointRef,
		},
	}
}

func staticCol[T any](opts krt.OptionsBuilder, name string, items ...T) krt.Collection[T] {
	return krt.NewStaticCollection(nil, items, opts.WithName(name)...)
}

// resolverFromMap returns a ServiceWaypointResolver backed by a static NamespacedName-keyed map.
// The AGW binding builder consumes only the k8s Gateway names, so tests can bypass ambient's
// address/index machinery and inject the resolved answer directly.
func resolverFromMap(m map[types.NamespacedName][]types.NamespacedName) ambient.ServiceWaypointResolver {
	return func(_ krt.HandlerContext, svc model.ServiceInfo) []types.NamespacedName {
		return m[svc.NamespacedName()]
	}
}

func TestBuildWaypointServiceBindings(t *testing.T) {
	svcKey := types.NamespacedName{Namespace: "ns1", Name: "svc1"}
	binding := func(wpNS, wpName string) WaypointServiceBinding {
		return WaypointServiceBinding{
			ServiceKey:      svcKey,
			WaypointGateway: types.NamespacedName{Namespace: wpNS, Name: wpName},
		}
	}
	tests := []struct {
		name     string
		services []model.ServiceInfo
		// waypoints is what ambient tells us fronts svcKey (primary + any canary).
		waypoints []types.NamespacedName
		gateways  []*gatewayv1.Gateway
		want      []WaypointServiceBinding
	}{
		{
			name:      "service with AGW waypoint produces binding",
			services:  []model.ServiceInfo{testServiceInfo("svc1", "ns1/wp")},
			waypoints: []types.NamespacedName{{Namespace: "ns1", Name: "wp"}},
			gateways:  []*gatewayv1.Gateway{testGateway("wp", constants.AgentgatewayWaypointClassName)},
			want:      []WaypointServiceBinding{binding("ns1", "wp")},
		},
		{
			name:      "service without waypoint produces no binding",
			services:  []model.ServiceInfo{testServiceInfo("svc1", "")},
			waypoints: nil,
			gateways:  []*gatewayv1.Gateway{testGateway("wp", constants.AgentgatewayWaypointClassName)},
			want:      nil,
		},
		{
			name:      "waypoint gateway missing produces no binding",
			services:  []model.ServiceInfo{testServiceInfo("svc1", "ns1/wp")},
			waypoints: []types.NamespacedName{{Namespace: "ns1", Name: "wp"}},
			gateways:  nil,
			want:      nil,
		},
		{
			name:      "waypoint gateway is not an AGW class produces no binding",
			services:  []model.ServiceInfo{testServiceInfo("svc1", "ns1/gw")},
			waypoints: []types.NamespacedName{{Namespace: "ns1", Name: "gw"}},
			gateways:  []*gatewayv1.Gateway{testGateway("gw", constants.AgentgatewayClassName)},
			want:      nil,
		},
		{
			name:      "Envoy waypoint class is ignored",
			services:  []model.ServiceInfo{testServiceInfo("svc1", "ns1/envoy-wp")},
			waypoints: []types.NamespacedName{{Namespace: "ns1", Name: "envoy-wp"}},
			gateways:  []*gatewayv1.Gateway{testGateway("envoy-wp", constants.WaypointGatewayClassName)},
			want:      nil,
		},
		{
			name:      "ServiceEntry source is dropped even when waypoint resolves",
			services:  []model.ServiceInfo{{Service: &workloadapi.Service{Namespace: "ns1", Name: "svc1"}, Source: model.TypedObject{Kind: kind.ServiceEntry}, Waypoint: model.WaypointBindingStatus{ResourceName: "ns1/wp"}}},
			waypoints: []types.NamespacedName{{Namespace: "ns1", Name: "wp"}},
			gateways:  []*gatewayv1.Gateway{testGateway("wp", constants.AgentgatewayWaypointClassName)},
			want:      nil,
		},
		{
			name:      "cross-namespace waypoint reference resolves",
			services:  []model.ServiceInfo{testServiceInfo("svc1", "ns2/wp")},
			waypoints: []types.NamespacedName{{Namespace: "ns2", Name: "wp"}},
			gateways:  []*gatewayv1.Gateway{testGatewayIn("ns2", "wp", constants.AgentgatewayWaypointClassName)},
			want:      []WaypointServiceBinding{binding("ns2", "wp")},
		},
		{
			name:     "AGW primary and AGW canary both bind",
			services: []model.ServiceInfo{testServiceInfo("svc1", "ns1/wp")},
			waypoints: []types.NamespacedName{
				{Namespace: "ns1", Name: "wp"},
				{Namespace: "ns1", Name: "wpc"},
			},
			gateways: []*gatewayv1.Gateway{
				testGateway("wp", constants.AgentgatewayWaypointClassName),
				testGateway("wpc", constants.AgentgatewayWaypointClassName),
			},
			want: []WaypointServiceBinding{binding("ns1", "wp"), binding("ns1", "wpc")},
		},
		{
			// Envoy primary fronts the service, connections shift to an AGW canary. Only the canary
			// is ours, and it still needs the service's config.
			name:     "Envoy primary with AGW canary binds only the canary",
			services: []model.ServiceInfo{testServiceInfo("svc1", "ns1/wp")},
			waypoints: []types.NamespacedName{
				{Namespace: "ns1", Name: "wp"},
				{Namespace: "ns1", Name: "wpc"},
			},
			gateways: []*gatewayv1.Gateway{
				testGateway("wp", constants.WaypointGatewayClassName),
				testGateway("wpc", constants.AgentgatewayWaypointClassName),
			},
			want: []WaypointServiceBinding{binding("ns1", "wpc")},
		},
		{
			name:     "AGW primary with Envoy canary binds only the primary",
			services: []model.ServiceInfo{testServiceInfo("svc1", "ns1/wp")},
			waypoints: []types.NamespacedName{
				{Namespace: "ns1", Name: "wp"},
				{Namespace: "ns1", Name: "wpc"},
			},
			gateways: []*gatewayv1.Gateway{
				testGateway("wp", constants.AgentgatewayWaypointClassName),
				testGateway("wpc", constants.WaypointGatewayClassName),
			},
			want: []WaypointServiceBinding{binding("ns1", "wp")},
		},
		{
			name:     "canary gateway missing produces only the primary binding",
			services: []model.ServiceInfo{testServiceInfo("svc1", "ns1/wp")},
			waypoints: []types.NamespacedName{
				{Namespace: "ns1", Name: "wp"},
				{Namespace: "ns1", Name: "wpc"},
			},
			gateways: []*gatewayv1.Gateway{testGateway("wp", constants.AgentgatewayWaypointClassName)},
			want:     []WaypointServiceBinding{binding("ns1", "wp")},
		},
		{
			name:     "canary in another namespace binds there",
			services: []model.ServiceInfo{testServiceInfo("svc1", "ns1/wp")},
			waypoints: []types.NamespacedName{
				{Namespace: "ns1", Name: "wp"},
				{Namespace: "ns2", Name: "wpc"},
			},
			gateways: []*gatewayv1.Gateway{
				testGateway("wp", constants.AgentgatewayWaypointClassName),
				testGatewayIn("ns2", "wpc", constants.AgentgatewayWaypointClassName),
			},
			want: []WaypointServiceBinding{binding("ns1", "wp"), binding("ns2", "wpc")},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts := krttest.Options(t)
			gatewayClasses := staticCol[gatewaycommon.GatewayClass](opts, "GatewayClasses")
			resolver := resolverFromMap(map[types.NamespacedName][]types.NamespacedName{svcKey: tt.waypoints})
			bindings := BuildWaypointServiceBindings(
				staticCol(opts, "Services", tt.services...),
				staticCol(opts, "Gateways", tt.gateways...),
				gatewayClasses,
				resolver,
				opts,
			)
			bindings.WaitUntilSynced(test.NewStop(t))
			got := slices.SortBy(bindings.List(), func(b WaypointServiceBinding) string { return b.ResourceName() })
			assert.Equal(t, got, tt.want)
		})
	}
}

// TestBuildWaypointServiceBindings_LiveUpdate pins the reactive contract: mutations to the
// upstream services and gateways collections propagate into the binding collection without a
// restart. This guards against a regression where the collection is inadvertently rebuilt from
// a snapshot.
func TestBuildWaypointServiceBindings_LiveUpdate(t *testing.T) {
	opts := krttest.Options(t)
	stop := test.NewStop(t)

	services := krt.NewStaticCollection[model.ServiceInfo](nil, nil, opts.WithName("Services")...)
	gateways := krt.NewStaticCollection[*gatewayv1.Gateway](nil,
		[]*gatewayv1.Gateway{testGateway("wp", constants.AgentgatewayWaypointClassName)},
		opts.WithName("Gateways")...)
	gatewayClasses := krt.NewStaticCollection[gatewaycommon.GatewayClass](nil, nil, opts.WithName("GatewayClasses")...)

	svcKey := types.NamespacedName{Namespace: "ns1", Name: "svc1"}
	resolver := resolverFromMap(map[types.NamespacedName][]types.NamespacedName{
		svcKey: {{Namespace: "ns1", Name: "wp"}},
	})

	bindings := BuildWaypointServiceBindings(services, gateways, gatewayClasses, resolver, opts)
	bindings.WaitUntilSynced(stop)
	assert.Equal(t, bindings.List(), nil)

	services.UpdateObject(testServiceInfo("svc1", "ns1/wp"))
	want := WaypointServiceBinding{
		ServiceKey:      svcKey,
		WaypointGateway: types.NamespacedName{Namespace: "ns1", Name: "wp"},
	}
	assert.EventuallyEqual(t, func() []WaypointServiceBinding { return bindings.List() }, []WaypointServiceBinding{want})

	gateways.DeleteObject("ns1/wp")
	assert.EventuallyEqual(t, func() []WaypointServiceBinding { return bindings.List() }, nil)
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
		// svc3 is split between a primary and a canary waypoint.
		WaypointServiceBinding{
			ServiceKey:      types.NamespacedName{Namespace: "ns1", Name: "svc3"},
			WaypointGateway: types.NamespacedName{Namespace: "ns1", Name: "wp1"},
		},
		WaypointServiceBinding{
			ServiceKey:      types.NamespacedName{Namespace: "ns1", Name: "svc3"},
			WaypointGateway: types.NamespacedName{Namespace: "ns1", Name: "wp3"},
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
			name:      "service ref resolves to both primary and canary waypoints",
			defaultNS: "ns1",
			refs:      []gatewayv1.ParentReference{serviceRef("", "svc3")},
			want:      []types.NamespacedName{{Namespace: "ns1", Name: "wp1"}, {Namespace: "ns1", Name: "wp3"}},
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
	waypointListener := func(name string) *GatewayListener {
		gw := types.NamespacedName{Namespace: "ns1", Name: name}
		return &GatewayListener{
			Name:          "ns1/" + name,
			ParentGateway: gw,
			ParentObject:  AgwParentKey{Kind: gvk.KubernetesGateway.Kubernetes(), Namespace: "ns1", Name: name},
			ParentInfo: AgwParentInfo{
				ParentGateway:          gw,
				ParentGatewayClassName: constants.AgentgatewayWaypointClassName,
				Protocol:               hboneProtocol,
				Port:                   15008,
			},
			Valid: true,
		}
	}
	wpKey := AgwParentKey{Kind: gvk.KubernetesGateway.Kubernetes(), Namespace: "ns1", Name: "wp"}
	gateways := staticCol(opts, "Gateways", waypointListener("wp"), waypointListener("wpc"))
	gateways.WaitUntilSynced(test.NewStop(t))
	binding := func(svc, gw string) WaypointServiceBinding {
		return WaypointServiceBinding{
			ServiceKey:      types.NamespacedName{Namespace: "ns1", Name: svc},
			WaypointGateway: types.NamespacedName{Namespace: "ns1", Name: gw},
		}
	}
	// svc1 is fronted by one waypoint, svc2 is split between a primary and a canary waypoint.
	bindings := staticCol(opts, "Bindings", binding("svc1", "wp"), binding("svc2", "wp"), binding("svc2", "wpc"))
	bindings.WaitUntilSynced(test.NewStop(t))
	parents := BuildRouteParents(gateways, bindings)

	gatewayNames := func(got []*AgwParentInfo) []string {
		return slices.Sort(slices.Map(got, func(p *AgwParentInfo) string { return p.ParentGateway.String() }))
	}

	t.Run("service parent resolves to waypoint listeners", func(t *testing.T) {
		svcKey := AgwParentKey{Kind: gvk.Service.Kubernetes(), Namespace: "ns1", Name: "svc1"}
		got := parents.fetch(krt.TestingDummyContext{}, svcKey)
		assert.Equal(t, gatewayNames(got), []string{"ns1/wp"})
	})

	t.Run("service parent resolves to both primary and canary waypoints", func(t *testing.T) {
		svcKey := AgwParentKey{Kind: gvk.Service.Kubernetes(), Namespace: "ns1", Name: "svc2"}
		got := parents.fetch(krt.TestingDummyContext{}, svcKey)
		assert.Equal(t, gatewayNames(got), []string{"ns1/wp", "ns1/wpc"})
	})

	t.Run("service parent without binding resolves to nothing", func(t *testing.T) {
		svcKey := AgwParentKey{Kind: gvk.Service.Kubernetes(), Namespace: "ns1", Name: "other"}
		got := parents.fetch(krt.TestingDummyContext{}, svcKey)
		assert.Equal(t, len(got), 0)
	})

	t.Run("gateway parent resolves via gateway index", func(t *testing.T) {
		got := parents.fetch(krt.TestingDummyContext{}, wpKey)
		assert.Equal(t, gatewayNames(got), []string{"ns1/wp"})
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
