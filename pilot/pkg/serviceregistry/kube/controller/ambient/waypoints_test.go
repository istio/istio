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
	gatewayv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"

	"istio.io/api/annotation"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/ptr"
	"istio.io/istio/pkg/test/util/assert"
)

func TestMakeAllowedRoutes(t *testing.T) {
	istioWaypointClass := &gatewayv1beta1.GatewayClass{
		ObjectMeta: metav1.ObjectMeta{
			Name: "istio-waypoint",
		},
		Spec: gatewayv1beta1.GatewayClassSpec{
			ControllerName: constants.ManagedGatewayMeshController,
		},
	}
	sandwichedWaypointClass := &gatewayv1beta1.GatewayClass{
		ObjectMeta: metav1.ObjectMeta{
			Name: "sandwiched-waypoint",
		},
		Spec: gatewayv1beta1.GatewayClassSpec{
			ControllerName: "sandwiched-controller",
		},
	}
	tests := []struct {
		name         string
		gateway      *gatewayv1beta1.Gateway
		gatewayClass *gatewayv1beta1.GatewayClass
		expected     WaypointSelector
	}{
		{
			name: "istio-waypoint defaults to same namespace",
			gateway: &gatewayv1beta1.Gateway{
				Spec: gatewayv1beta1.GatewaySpec{
					GatewayClassName: gatewayv1.ObjectName(istioWaypointClass.Name),
					Listeners:        []gatewayv1beta1.Listener{},
				},
			},
			gatewayClass: istioWaypointClass,
			expected: WaypointSelector{
				FromNamespaces: gatewayv1.NamespacesFromSame,
			},
		},
		{
			name: "istio-waypoint matching listener with no allowed routes",
			gateway: &gatewayv1beta1.Gateway{
				Spec: gatewayv1beta1.GatewaySpec{
					Listeners: []gatewayv1beta1.Listener{
						{
							Protocol: gatewayv1beta1.ProtocolType("HBONE"),
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
			gateway: &gatewayv1beta1.Gateway{
				Spec: gatewayv1beta1.GatewaySpec{
					Listeners: []gatewayv1beta1.Listener{
						{
							Protocol: gatewayv1beta1.ProtocolType("HBONE"),
							Port:     15008,
							AllowedRoutes: &gatewayv1beta1.AllowedRoutes{
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
			gateway: &gatewayv1beta1.Gateway{
				Spec: gatewayv1beta1.GatewaySpec{
					Listeners: []gatewayv1beta1.Listener{
						{
							Protocol: gatewayv1beta1.ProtocolType("HBONE"),
							Port:     15008,
							AllowedRoutes: &gatewayv1beta1.AllowedRoutes{
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
			gateway: &gatewayv1beta1.Gateway{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						annotation.AmbientWaypointInboundBinding.Name: "PROXY/15088",
					},
				},
				Spec: gatewayv1beta1.GatewaySpec{
					GatewayClassName: gatewayv1.ObjectName(sandwichedWaypointClass.Name),
					Listeners: []gatewayv1beta1.Listener{
						{
							Protocol: gatewayv1beta1.ProtocolType("PROXY"),
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
			gateway: &gatewayv1beta1.Gateway{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						annotation.AmbientWaypointInboundBinding.Name: "PROXY/15088",
					},
				},
				Spec: gatewayv1beta1.GatewaySpec{
					GatewayClassName: gatewayv1.ObjectName(sandwichedWaypointClass.Name),
					Listeners: []gatewayv1beta1.Listener{
						{
							Name:     "should be ignored",
							Protocol: gatewayv1beta1.ProtocolType("istio.io/PROXY"),
							Port:     15089,
							AllowedRoutes: &gatewayv1beta1.AllowedRoutes{
								Namespaces: &gatewayv1.RouteNamespaces{
									From: ptr.Of(gatewayv1.NamespacesFromSame),
								},
							},
						},
						{
							Name:     "ignored HBONE listener",
							Protocol: gatewayv1beta1.ProtocolType("HBONE"),
							Port:     15008,
							AllowedRoutes: &gatewayv1beta1.AllowedRoutes{
								Namespaces: &gatewayv1.RouteNamespaces{
									From: ptr.Of(gatewayv1.NamespacesFromSame),
								},
							},
						},
						{
							Name:     "should be used",
							Protocol: gatewayv1beta1.ProtocolType("istio.io/PROXY"),
							Port:     15088,
							AllowedRoutes: &gatewayv1beta1.AllowedRoutes{
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
			gateway: &gatewayv1beta1.Gateway{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						annotation.AmbientWaypointInboundBinding.Name: "PROXY/15088",
					},
				},
				Spec: gatewayv1beta1.GatewaySpec{
					GatewayClassName: gatewayv1.ObjectName(sandwichedWaypointClass.Name),
					Listeners: []gatewayv1beta1.Listener{
						{
							Name:     "should be ignored",
							Protocol: gatewayv1beta1.ProtocolType("PROXY"),
							Port:     15089,
						},
						{
							Name:     "should be used",
							Protocol: gatewayv1beta1.ProtocolType("PROXY"),
							Port:     15088,
							AllowedRoutes: &gatewayv1beta1.AllowedRoutes{
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
			gateway: &gatewayv1beta1.Gateway{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						annotation.AmbientWaypointInboundBinding.Name: "PROXY",
					},
				},
				Spec: gatewayv1beta1.GatewaySpec{
					GatewayClassName: gatewayv1.ObjectName(sandwichedWaypointClass.Name),
					Listeners: []gatewayv1beta1.Listener{
						{
							Name:     "should be ignored",
							Protocol: gatewayv1beta1.ProtocolType("HBONE"),
							Port:     15008,
						},
						{
							Name:     "should be used",
							Protocol: gatewayv1beta1.ProtocolType("PROXY"),
							Port:     15015,
							AllowedRoutes: &gatewayv1beta1.AllowedRoutes{
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
