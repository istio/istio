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

package ambient

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	"istio.io/api/annotation"
	"istio.io/api/label"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/kube/krt"
	"istio.io/istio/pkg/ptr"
	"istio.io/istio/pkg/test/util/assert"
	"istio.io/istio/pkg/workloadapi"
)

func TestMakeAllowedRoutes(t *testing.T) {
	istioWaypointClass := &gatewayv1.GatewayClass{
		ObjectMeta: metav1.ObjectMeta{
			Name: "istio-waypoint",
		},
		Spec: gatewayv1.GatewayClassSpec{
			ControllerName: constants.ManagedGatewayMeshController,
		},
	}
	sandwichedWaypointClass := &gatewayv1.GatewayClass{
		ObjectMeta: metav1.ObjectMeta{
			Name: "sandwiched-waypoint",
		},
		Spec: gatewayv1.GatewayClassSpec{
			ControllerName: "sandwiched-controller",
		},
	}
	tests := []struct {
		name         string
		gateway      *gatewayv1.Gateway
		gatewayClass *gatewayv1.GatewayClass
		expected     WaypointSelector
	}{
		{
			name: "istio-waypoint defaults to same namespace",
			gateway: &gatewayv1.Gateway{
				Spec: gatewayv1.GatewaySpec{
					GatewayClassName: gatewayv1.ObjectName(istioWaypointClass.Name),
					Listeners:        []gatewayv1.Listener{},
				},
			},
			gatewayClass: istioWaypointClass,
			expected: WaypointSelector{
				FromNamespaces: gatewayv1.NamespacesFromSame,
			},
		},
		{
			name: "istio-waypoint matching listener with no allowed routes",
			gateway: &gatewayv1.Gateway{
				Spec: gatewayv1.GatewaySpec{
					Listeners: []gatewayv1.Listener{
						{
							Protocol: gatewayv1.ProtocolType("HBONE"),
							Port:     15008,
						},
					},
				},
			},
			gatewayClass: istioWaypointClass,
			expected: WaypointSelector{
				FromNamespaces: gatewayv1.NamespacesFromSame,
			},
		},
		{
			name: "istio-waypoint matching listener with allowed routes",
			gateway: &gatewayv1.Gateway{
				Spec: gatewayv1.GatewaySpec{
					Listeners: []gatewayv1.Listener{
						{
							Protocol: gatewayv1.ProtocolType("HBONE"),
							Port:     15008,
							AllowedRoutes: &gatewayv1.AllowedRoutes{
								Namespaces: &gatewayv1.RouteNamespaces{
									From: ptr.Of(gatewayv1.NamespacesFromAll),
								},
							},
						},
					},
				},
			},
			expected: WaypointSelector{
				FromNamespaces: gatewayv1.NamespacesFromAll,
				Selector:       labels.SelectorFromSet(labels.Set{}),
			},
		},
		{
			name: "istio-waypoint matching listener with selector",
			gateway: &gatewayv1.Gateway{
				Spec: gatewayv1.GatewaySpec{
					Listeners: []gatewayv1.Listener{
						{
							Protocol: gatewayv1.ProtocolType("HBONE"),
							Port:     15008,
							AllowedRoutes: &gatewayv1.AllowedRoutes{
								Namespaces: &gatewayv1.RouteNamespaces{
									From: ptr.Of(gatewayv1.NamespacesFromSelector),
									Selector: &metav1.LabelSelector{
										MatchLabels: map[string]string{"app": "test"},
									},
								},
							},
						},
					},
				},
			},
			expected: WaypointSelector{
				FromNamespaces: gatewayv1.NamespacesFromSelector,
				Selector:       labels.SelectorFromSet(labels.Set{"app": "test"}),
			},
		},
		{
			name: "sandwiched waypoint defaults to same namespace",
			gateway: &gatewayv1.Gateway{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						annotation.AmbientWaypointInboundBinding.Name: "PROXY/15088",
					},
				},
				Spec: gatewayv1.GatewaySpec{
					GatewayClassName: gatewayv1.ObjectName(sandwichedWaypointClass.Name),
					Listeners: []gatewayv1.Listener{
						{
							Protocol: gatewayv1.ProtocolType(constants.WaypointSandwichListenerProxyProtocol),
							Port:     15088,
						},
					},
				},
			},
			gatewayClass: sandwichedWaypointClass,
			expected: WaypointSelector{
				FromNamespaces: gatewayv1.NamespacesFromSame,
			},
		},
		{
			name: "sandwiched waypoint with listener with allowed routes",
			gateway: &gatewayv1.Gateway{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						annotation.AmbientWaypointInboundBinding.Name: "PROXY/15088",
					},
				},
				Spec: gatewayv1.GatewaySpec{
					GatewayClassName: gatewayv1.ObjectName(sandwichedWaypointClass.Name),
					Listeners: []gatewayv1.Listener{
						{
							Name:     "should be ignored",
							Protocol: gatewayv1.ProtocolType(constants.WaypointSandwichListenerProxyProtocol),
							Port:     15089,
							AllowedRoutes: &gatewayv1.AllowedRoutes{
								Namespaces: &gatewayv1.RouteNamespaces{
									From: ptr.Of(gatewayv1.NamespacesFromSame),
								},
							},
						},
						{
							Name:     "should be used",
							Protocol: gatewayv1.ProtocolType(constants.WaypointSandwichListenerProxyProtocol),
							Port:     15088,
							AllowedRoutes: &gatewayv1.AllowedRoutes{
								Namespaces: &gatewayv1.RouteNamespaces{
									From: ptr.Of(gatewayv1.NamespacesFromAll),
								},
							},
						},
					},
				},
			},
			gatewayClass: sandwichedWaypointClass,
			expected: WaypointSelector{
				FromNamespaces: gatewayv1.NamespacesFromAll,
				Selector:       labels.SelectorFromSet(labels.Set{}),
			},
		},
		{
			name: "sandwiched waypoint with listener with allowed routes",
			gateway: &gatewayv1.Gateway{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						annotation.AmbientWaypointInboundBinding.Name: "PROXY/15088",
					},
				},
				Spec: gatewayv1.GatewaySpec{
					GatewayClassName: gatewayv1.ObjectName(sandwichedWaypointClass.Name),
					Listeners: []gatewayv1.Listener{
						{
							Name:     "should be ignored",
							Protocol: gatewayv1.ProtocolType(constants.WaypointSandwichListenerProxyProtocol),
							Port:     15089,
						},
						{
							Name:     "should be used",
							Protocol: gatewayv1.ProtocolType(constants.WaypointSandwichListenerProxyProtocol),
							Port:     15088,
							AllowedRoutes: &gatewayv1.AllowedRoutes{
								Namespaces: &gatewayv1.RouteNamespaces{
									From: ptr.Of(gatewayv1.NamespacesFromAll),
								},
							},
						},
					},
				},
			},
			gatewayClass: sandwichedWaypointClass,
			expected: WaypointSelector{
				FromNamespaces: gatewayv1.NamespacesFromAll,
				Selector:       labels.SelectorFromSet(labels.Set{}),
			},
		},
		{
			name: "sandwiched waypoint with listener with allowed routes on port 0",
			gateway: &gatewayv1.Gateway{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						annotation.AmbientWaypointInboundBinding.Name: "PROXY",
					},
				},
				Spec: gatewayv1.GatewaySpec{
					GatewayClassName: gatewayv1.ObjectName(sandwichedWaypointClass.Name),
					Listeners: []gatewayv1.Listener{
						{
							Name:     "should be used",
							Protocol: gatewayv1.ProtocolType(constants.WaypointSandwichListenerProxyProtocol),
							Port:     15015,
							AllowedRoutes: &gatewayv1.AllowedRoutes{
								Namespaces: &gatewayv1.RouteNamespaces{
									From: ptr.Of(gatewayv1.NamespacesFromAll),
								},
							},
						},
					},
				},
			},
			gatewayClass: sandwichedWaypointClass,
			expected: WaypointSelector{
				FromNamespaces: gatewayv1.NamespacesFromAll,
				Selector:       labels.SelectorFromSet(labels.Set{}),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			binding := makeInboundBinding(tt.gateway, tt.gatewayClass)
			result := makeAllowedRoutes(tt.gateway, binding)
			assertWaypointSelector(t, result, tt.expected)
		})
	}
}

func assertWaypointSelector(t *testing.T, result, expected WaypointSelector) {
	t.Helper()
	assert.Equal(t, result.FromNamespaces, expected.FromNamespaces)
	if expected.Selector == nil {
		assert.Equal(t, result.Selector, nil)
	} else {
		assert.Equal(t, result.Selector.String(), expected.Selector.String())
	}
}

func TestGetUseWaypointCanary(t *testing.T) {
	cases := []struct {
		name      string
		labels    map[string]string
		nsLabels  map[string]string
		defaultNS string
		want      *krt.Named
	}{
		{"no label", nil, nil, "ns", nil},
		{"empty value", map[string]string{label.IoIstioUseWaypointCanary.Name: ""}, nil, "ns", nil},
		{"none", map[string]string{label.IoIstioUseWaypointCanary.Name: "none"}, nil, "ns", nil},
		{
			"same namespace",
			map[string]string{label.IoIstioUseWaypointCanary.Name: "wp"},
			nil,
			"ns",
			&krt.Named{Name: "wp", Namespace: "ns"},
		},
		{
			"cross namespace",
			map[string]string{
				label.IoIstioUseWaypointCanary.Name:          "wp",
				label.IoIstioUseWaypointCanaryNamespace.Name: "other",
			},
			nil,
			"ns",
			&krt.Named{Name: "wp", Namespace: "other"},
		},
		{
			"inherited from namespace",
			nil,
			map[string]string{label.IoIstioUseWaypointCanary.Name: "ns-wp"},
			"ns",
			&krt.Named{Name: "ns-wp", Namespace: "ns"},
		},
		{
			"inherited cross namespace from namespace",
			nil,
			map[string]string{
				label.IoIstioUseWaypointCanary.Name:          "ns-wp",
				label.IoIstioUseWaypointCanaryNamespace.Name: "other",
			},
			"ns",
			&krt.Named{Name: "ns-wp", Namespace: "other"},
		},
		{
			"object overrides namespace",
			map[string]string{label.IoIstioUseWaypointCanary.Name: "wp"},
			map[string]string{label.IoIstioUseWaypointCanary.Name: "ns-wp"},
			"ns",
			&krt.Named{Name: "wp", Namespace: "ns"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var nsMeta *metav1.ObjectMeta
			if tc.nsLabels != nil {
				nsMeta = &metav1.ObjectMeta{Labels: tc.nsLabels}
			}
			got := getUseWaypointCanary(metav1.ObjectMeta{Labels: tc.labels}, nsMeta, tc.defaultNS)
			assert.Equal(t, got, tc.want)
		})
	}
}

func TestGetCanaryWeight(t *testing.T) {
	anno := annotation.IoIstioUseWaypointCanaryWeight.Name
	cases := []struct {
		name       string
		anns       map[string]string
		nsAnns     map[string]string
		wantWeight uint32
		wantValid  bool
	}{
		{"absent defaults to 0", nil, nil, 0, true},
		{"zero", map[string]string{anno: "0"}, nil, 0, true},
		{"mid", map[string]string{anno: "42"}, nil, 42, true},
		{"hundred", map[string]string{anno: "100"}, nil, 100, true},
		{"non-integer", map[string]string{anno: "abc"}, nil, 0, false},
		{"negative", map[string]string{anno: "-1"}, nil, 0, false},
		{"over hundred", map[string]string{anno: "101"}, nil, 0, false},
		{"inherited from namespace", nil, map[string]string{anno: "30"}, 30, true},
		{"object overrides namespace", map[string]string{anno: "42"}, map[string]string{anno: "30"}, 42, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var nsMeta *metav1.ObjectMeta
			if tc.nsAnns != nil {
				nsMeta = &metav1.ObjectMeta{Annotations: tc.nsAnns}
			}
			w, valid := getCanaryWeight(metav1.ObjectMeta{Annotations: tc.anns}, nsMeta)
			assert.Equal(t, valid, tc.wantValid)
			assert.Equal(t, w, tc.wantWeight)
		})
	}
}

func TestServiceOwningWaypoints(t *testing.T) {
	primary := &workloadapi.GatewayAddress{
		Destination:   &workloadapi.GatewayAddress_Hostname{Hostname: &workloadapi.NamespacedHostname{Namespace: "ns", Hostname: "primary"}},
		HboneMtlsPort: 15008,
	}
	canary := &workloadapi.GatewayAddress{
		Destination:   &workloadapi.GatewayAddress_Hostname{Hostname: &workloadapi.NamespacedHostname{Namespace: "ns", Hostname: "canary"}},
		HboneMtlsPort: 15008,
	}
	cases := []struct {
		name string
		svc  *workloadapi.Service
		want []*workloadapi.GatewayAddress
	}{
		{"nil service", nil, nil},
		{"no waypoint", &workloadapi.Service{}, nil},
		{"primary only", &workloadapi.Service{Waypoint: primary}, []*workloadapi.GatewayAddress{primary}},
		{
			// The weighted set always contains the primary, so it is the complete set.
			"primary plus weighted canary",
			&workloadapi.Service{
				Waypoint: primary,
				WeightedWaypoints: []*workloadapi.WeightedWaypoint{
					{Destination: primary, Weight: 95},
					{Destination: canary, Weight: 5},
				},
			},
			[]*workloadapi.GatewayAddress{primary, canary},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := serviceOwningWaypoints(model.ServiceInfo{Service: tc.svc})
			assert.Equal(t, got, tc.want)
		})
	}
}
