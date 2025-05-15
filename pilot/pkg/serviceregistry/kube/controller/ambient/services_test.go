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
	"net/netip"
	"testing"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/intstr"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	"istio.io/api/label"
	networking "istio.io/api/networking/v1alpha3"
	networkingclient "istio.io/client-go/pkg/apis/networking/v1"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/kube/krt"
	"istio.io/istio/pkg/kube/krt/krttest"
	"istio.io/istio/pkg/ptr"
	"istio.io/istio/pkg/slices"
	"istio.io/istio/pkg/test/util/assert"
	"istio.io/istio/pkg/workloadapi"
)

func TestServiceEntryServices(t *testing.T) {
	cases := []struct {
		name   string
		inputs []any
		se     *networkingclient.ServiceEntry
		result []*workloadapi.Service
	}{
		{
			name:   "DNS service entry with address",
			inputs: []any{},
			se: &networkingclient.ServiceEntry{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "name",
					Namespace: "ns",
				},
				Spec: networking.ServiceEntry{
					Addresses: []string{"1.2.3.4"},
					Hosts:     []string{"a.example.com", "b.example.com"},
					Ports: []*networking.ServicePort{{
						Number: 80,
						Name:   "http",
					}},
					SubjectAltNames: []string{"san1"},
					Resolution:      networking.ServiceEntry_DNS,
				},
			},
			result: []*workloadapi.Service{
				{
					Name:      "name",
					Namespace: "ns",
					Hostname:  "a.example.com",
					Addresses: []*workloadapi.NetworkAddress{{
						Network: testNW,
						Address: netip.AddrFrom4([4]byte{1, 2, 3, 4}).AsSlice(),
					}},
					Ports: []*workloadapi.Port{{
						ServicePort: 80,
						TargetPort:  80,
					}},
					SubjectAltNames: []string{"san1"},
				},
				{
					Name:      "name",
					Namespace: "ns",
					Hostname:  "b.example.com",
					Addresses: []*workloadapi.NetworkAddress{{
						Network: testNW,
						Address: netip.AddrFrom4([4]byte{1, 2, 3, 4}).AsSlice(),
					}},
					Ports: []*workloadapi.Port{{
						ServicePort: 80,
						TargetPort:  80,
					}},
					SubjectAltNames: []string{"san1"},
				},
			},
		},
		{
			name:   "Uses auto-assigned addresses",
			inputs: []any{},
			se: &networkingclient.ServiceEntry{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "auto-assigned",
					Namespace: "ns",
				},
				Spec: networking.ServiceEntry{
					Hosts: []string{"assign-me.example.com"},
					Ports: []*networking.ServicePort{{
						Number: 80,
						Name:   "http",
					}},
					SubjectAltNames: []string{"san1"},
					Resolution:      networking.ServiceEntry_DNS,
				},
				Status: networking.ServiceEntryStatus{
					Addresses: []*networking.ServiceEntryAddress{
						{
							Host:  "assign-me.example.com",
							Value: "240.240.0.1",
						},
						{
							Host:  "assign-me.example.com",
							Value: "2001:2::1",
						},
					},
				},
			},
			result: []*workloadapi.Service{
				{
					Name:      "auto-assigned",
					Namespace: "ns",
					Hostname:  "assign-me.example.com",
					Addresses: []*workloadapi.NetworkAddress{
						{
							Network: testNW,
							Address: netip.AddrFrom4([4]byte{240, 240, 0, 1}).AsSlice(),
						},
						{
							Network: testNW,
							Address: netip.MustParseAddr("2001:2::1").AsSlice(),
						},
					},
					Ports: []*workloadapi.Port{{
						ServicePort: 80,
						TargetPort:  80,
					}},
					SubjectAltNames: []string{"san1"},
				},
			},
		},
		{
			name:   "Uses multiple auto-assigned addresses",
			inputs: []any{},
			se: &networkingclient.ServiceEntry{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "multi-host-auto-assigned",
					Namespace: "ns",
				},
				Spec: networking.ServiceEntry{
					Hosts: []string{
						"multi-host-assign-me.example.com",
						"second-host-assign-me.example.com",
					},
					Ports: []*networking.ServicePort{{
						Number: 80,
						Name:   "http",
					}},
					SubjectAltNames: []string{"san1"},
					Resolution:      networking.ServiceEntry_DNS,
				},
				Status: networking.ServiceEntryStatus{
					Addresses: []*networking.ServiceEntryAddress{
						{
							Host:  "multi-host-assign-me.example.com",
							Value: "240.240.0.1",
						},
						{
							Host:  "multi-host-assign-me.example.com",
							Value: "2001:2::1",
						},
						{
							Host:  "second-host-assign-me.example.com",
							Value: "240.240.0.2",
						},
						{
							Host:  "second-host-assign-me.example.com",
							Value: "2001:2::2",
						},
					},
				},
			},
			result: []*workloadapi.Service{
				{
					Name:      "multi-host-auto-assigned",
					Namespace: "ns",
					Hostname:  "multi-host-assign-me.example.com",
					Addresses: []*workloadapi.NetworkAddress{
						{
							Network: testNW,
							Address: netip.AddrFrom4([4]byte{240, 240, 0, 1}).AsSlice(),
						},
						{
							Network: testNW,
							Address: netip.MustParseAddr("2001:2::1").AsSlice(),
						},
					},
					Ports: []*workloadapi.Port{{
						ServicePort: 80,
						TargetPort:  80,
					}},
					SubjectAltNames: []string{"san1"},
				},
				{
					Name:      "multi-host-auto-assigned",
					Namespace: "ns",
					Hostname:  "second-host-assign-me.example.com",
					Addresses: []*workloadapi.NetworkAddress{
						{
							Network: testNW,
							Address: netip.AddrFrom4([4]byte{240, 240, 0, 2}).AsSlice(),
						},
						{
							Network: testNW,
							Address: netip.MustParseAddr("2001:2::2").AsSlice(),
						},
					},
					Ports: []*workloadapi.Port{{
						ServicePort: 80,
						TargetPort:  80,
					}},
					SubjectAltNames: []string{"san1"},
				},
			},
		},
		{
			name:   "Does not use auto-assigned addresses user provided address",
			inputs: []any{},
			se: &networkingclient.ServiceEntry{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "user-provided",
					Namespace: "ns",
				},
				Spec: networking.ServiceEntry{
					Addresses: []string{"1.2.3.4"},
					Hosts:     []string{"user-provided.example.com"},
					Ports: []*networking.ServicePort{{
						Number: 80,
						Name:   "http",
					}},
					SubjectAltNames: []string{"san1"},
					Resolution:      networking.ServiceEntry_DNS,
				},
				Status: networking.ServiceEntryStatus{
					Addresses: []*networking.ServiceEntryAddress{
						{
							Host:  "user-provided.example.com",
							Value: "240.240.0.1",
						},
						{
							Host:  "user-provided.example.com",
							Value: "2001:2::1",
						},
					},
				},
			},
			result: []*workloadapi.Service{
				{
					Name:      "user-provided",
					Namespace: "ns",
					Hostname:  "user-provided.example.com",
					Addresses: []*workloadapi.NetworkAddress{
						{
							Network: testNW,
							Address: netip.AddrFrom4([4]byte{1, 2, 3, 4}).AsSlice(),
						},
					},
					Ports: []*workloadapi.Port{{
						ServicePort: 80,
						TargetPort:  80,
					}},
					SubjectAltNames: []string{"san1"},
				},
			},
		},
		{
			name:   "Does not use auto-assigned addresses none resolution",
			inputs: []any{},
			se: &networkingclient.ServiceEntry{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "none-resolution",
					Namespace: "ns",
				},
				Spec: networking.ServiceEntry{
					Hosts: []string{"none-resolution.example.com"},
					Ports: []*networking.ServicePort{{
						Number: 80,
						Name:   "http",
					}},
					SubjectAltNames: []string{"san1"},
					Resolution:      networking.ServiceEntry_NONE,
				},
				Status: networking.ServiceEntryStatus{
					Addresses: []*networking.ServiceEntryAddress{
						{
							Host:  "none-resolution.example.com",
							Value: "240.240.0.1",
						},
						{
							Host:  "none-resolution.example.com",
							Value: "2001:2::1",
						},
					},
				},
			},
			result: []*workloadapi.Service{
				{
					Name:      "none-resolution",
					Namespace: "ns",
					Hostname:  "none-resolution.example.com",
					Addresses: []*workloadapi.NetworkAddress{},
					Ports: []*workloadapi.Port{{
						ServicePort: 80,
						TargetPort:  80,
					}},
					SubjectAltNames: []string{"san1"},
				},
			},
		},
		{
			name:   "Does not use auto-assigned addresses user opted out",
			inputs: []any{},
			se: &networkingclient.ServiceEntry{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "user-opt-out",
					Namespace: "ns",
					Labels: map[string]string{
						label.NetworkingEnableAutoallocateIp.Name: "false",
					},
				},
				Spec: networking.ServiceEntry{
					Hosts: []string{"user-opt-out.example.com"},
					Ports: []*networking.ServicePort{{
						Number: 80,
						Name:   "http",
					}},
					SubjectAltNames: []string{"san1"},
					Resolution:      networking.ServiceEntry_DNS,
				},
				Status: networking.ServiceEntryStatus{
					Addresses: []*networking.ServiceEntryAddress{
						{
							Host:  "user-opt-out.example.com",
							Value: "240.240.0.1",
						},
						{
							Host:  "user-opt-out.example.com",
							Value: "2001:2::1",
						},
					},
				},
			},
			result: []*workloadapi.Service{
				{
					Name:      "user-opt-out",
					Namespace: "ns",
					Hostname:  "user-opt-out.example.com",
					Addresses: []*workloadapi.NetworkAddress{},
					Ports: []*workloadapi.Port{{
						ServicePort: 80,
						TargetPort:  80,
					}},
					SubjectAltNames: []string{"san1"},
				},
			},
		},
		{
			name:   "Does not use auto-assigned addresses for wildcard host",
			inputs: []any{},
			se: &networkingclient.ServiceEntry{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "partial-wildcard",
					Namespace: "ns",
				},
				Spec: networking.ServiceEntry{
					Hosts: []string{"*.wildcard.example.com", "this-is-ok.example.com"},
					Ports: []*networking.ServicePort{{
						Number: 80,
						Name:   "http",
					}},
					SubjectAltNames: []string{"san1"},
					Resolution:      networking.ServiceEntry_DNS,
				},
				Status: networking.ServiceEntryStatus{
					Addresses: []*networking.ServiceEntryAddress{
						{
							Host:  "this-is-ok.example.com",
							Value: "240.240.0.1",
						},
						{
							Host:  "this-is-ok.example.com",
							Value: "2001:2::1",
						},
					},
				},
			},
			result: []*workloadapi.Service{
				{
					Name:      "partial-wildcard",
					Namespace: "ns",
					Hostname:  "*.wildcard.example.com",
					Addresses: []*workloadapi.NetworkAddress{},
					Ports: []*workloadapi.Port{{
						ServicePort: 80,
						TargetPort:  80,
					}},
					SubjectAltNames: []string{"san1"},
				},
				{
					Name:      "partial-wildcard",
					Namespace: "ns",
					Hostname:  "this-is-ok.example.com",
					Addresses: []*workloadapi.NetworkAddress{
						{
							Network: testNW,
							Address: netip.AddrFrom4([4]byte{240, 240, 0, 1}).AsSlice(),
						},
						{
							Network: testNW,
							Address: netip.MustParseAddr("2001:2::1").AsSlice(),
						},
					},
					Ports: []*workloadapi.Port{{
						ServicePort: 80,
						TargetPort:  80,
					}},
					SubjectAltNames: []string{"san1"},
				},
			},
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			mock := krttest.NewMock(t, tt.inputs)
			a := newAmbientUnitTest(t)
			builder := a.serviceEntryServiceBuilder(
				krttest.GetMockCollection[Waypoint](mock),
				krttest.GetMockCollection[*v1.Namespace](mock),
			)
			wrapper := builder(krt.TestingDummyContext{}, tt.se)
			res := slices.Map(wrapper, func(e model.ServiceInfo) *workloadapi.Service {
				return e.Service
			})
			assert.Equal(t, res, tt.result)
		})
	}
}

func TestServiceServices(t *testing.T) {
	waypointAddr := &workloadapi.GatewayAddress{
		Destination: &workloadapi.GatewayAddress_Hostname{
			Hostname: &workloadapi.NamespacedHostname{
				Namespace: "ns",
				Hostname:  "hostname.example",
			},
		},
		// TODO: look up the HBONE port instead of hardcoding it
		HboneMtlsPort: 15008,
	}
	cases := []struct {
		name   string
		inputs []any
		svc    *v1.Service
		result *workloadapi.Service
	}{
		{
			name:   "simple",
			inputs: []any{},
			svc: &v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "name",
					Namespace: "ns",
				},
				Spec: v1.ServiceSpec{
					ClusterIP: "1.2.3.4",
					Ports: []v1.ServicePort{{
						Port: 80,
						Name: "http",
					}},
				},
			},
			result: &workloadapi.Service{
				Name:      "name",
				Namespace: "ns",
				Hostname:  "name.ns.svc.domain.suffix",
				Addresses: []*workloadapi.NetworkAddress{{
					Network: testNW,
					Address: netip.AddrFrom4([4]byte{1, 2, 3, 4}).AsSlice(),
				}},
				Ports: []*workloadapi.Port{{
					ServicePort: 80,
				}},
			},
		},
		{
			name:   "external name",
			inputs: []any{},
			svc: &v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "name",
					Namespace: "ns",
				},
				Spec: v1.ServiceSpec{
					Type:         v1.ServiceTypeExternalName,
					ExternalName: "example.com",
				},
			},
			result: nil,
		},
		{
			name:   "target ports",
			inputs: []any{},
			svc: &v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "name",
					Namespace: "ns",
				},
				Spec: v1.ServiceSpec{
					ClusterIP: "1.2.3.4",
					Ports: []v1.ServicePort{
						{
							Port:       80,
							TargetPort: intstr.FromInt32(81),
							Name:       "http",
						},
						{
							Port:       8080,
							TargetPort: intstr.FromString("something"),
							Name:       "http-alt",
						},
					},
				},
			},
			result: &workloadapi.Service{
				Name:      "name",
				Namespace: "ns",
				Hostname:  "name.ns.svc.domain.suffix",
				Addresses: []*workloadapi.NetworkAddress{{
					Network: testNW,
					Address: netip.AddrFrom4([4]byte{1, 2, 3, 4}).AsSlice(),
				}},
				Ports: []*workloadapi.Port{{
					ServicePort: 80,
					TargetPort:  81,
				}, {
					ServicePort: 8080,
					TargetPort:  0,
				}},
			},
		},
		{
			name:   "headless",
			inputs: []any{},
			svc: &v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "name",
					Namespace: "ns",
				},
				Spec: v1.ServiceSpec{
					Ports: []v1.ServicePort{{
						Port: 80,
						Name: "http",
					}},
				},
			},
			result: &workloadapi.Service{
				Name:      "name",
				Namespace: "ns",
				Hostname:  "name.ns.svc.domain.suffix",
				Ports: []*workloadapi.Port{{
					ServicePort: 80,
				}},
			},
		},
		{
			name:   "traffic distribution prefer close",
			inputs: []any{},
			svc: &v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "name",
					Namespace: "ns",
				},
				Spec: v1.ServiceSpec{
					TrafficDistribution: ptr.Of(v1.ServiceTrafficDistributionPreferClose),
					ClusterIP:           "1.2.3.4",
					Ports: []v1.ServicePort{{
						Port: 80,
						Name: "http",
					}},
				},
			},
			result: &workloadapi.Service{
				Name:      "name",
				Namespace: "ns",
				Hostname:  "name.ns.svc.domain.suffix",
				Addresses: []*workloadapi.NetworkAddress{{
					Network: testNW,
					Address: netip.AddrFrom4([4]byte{1, 2, 3, 4}).AsSlice(),
				}},
				LoadBalancing: &workloadapi.LoadBalancing{
					RoutingPreference: []workloadapi.LoadBalancing_Scope{
						workloadapi.LoadBalancing_NETWORK,
						workloadapi.LoadBalancing_REGION,
						workloadapi.LoadBalancing_ZONE,
					},
					Mode: workloadapi.LoadBalancing_FAILOVER,
				},
				Ports: []*workloadapi.Port{{
					ServicePort: 80,
				}},
			},
		},

		{
			name:   "traffic distribution prefer same zone",
			inputs: []any{},
			svc: &v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "name",
					Namespace: "ns",
				},
				Spec: v1.ServiceSpec{
					TrafficDistribution: ptr.Of(v1.ServiceTrafficDistributionPreferSameZone),
					ClusterIP:           "1.2.3.4",
					Ports: []v1.ServicePort{{
						Port: 80,
						Name: "http",
					}},
				},
			},
			result: &workloadapi.Service{
				Name:      "name",
				Namespace: "ns",
				Hostname:  "name.ns.svc.domain.suffix",
				Addresses: []*workloadapi.NetworkAddress{{
					Network: testNW,
					Address: netip.AddrFrom4([4]byte{1, 2, 3, 4}).AsSlice(),
				}},
				LoadBalancing: &workloadapi.LoadBalancing{
					RoutingPreference: []workloadapi.LoadBalancing_Scope{
						workloadapi.LoadBalancing_NETWORK,
						workloadapi.LoadBalancing_REGION,
						workloadapi.LoadBalancing_ZONE,
					},
					Mode: workloadapi.LoadBalancing_FAILOVER,
				},
				Ports: []*workloadapi.Port{{
					ServicePort: 80,
				}},
			},
		},

		{
			name:   "traffic distribution prefer same node",
			inputs: []any{},
			svc: &v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "name",
					Namespace: "ns",
				},
				Spec: v1.ServiceSpec{
					TrafficDistribution: ptr.Of(v1.ServiceTrafficDistributionPreferSameNode),
					ClusterIP:           "1.2.3.4",
					Ports: []v1.ServicePort{{
						Port: 80,
						Name: "http",
					}},
				},
			},
			result: &workloadapi.Service{
				Name:      "name",
				Namespace: "ns",
				Hostname:  "name.ns.svc.domain.suffix",
				Addresses: []*workloadapi.NetworkAddress{{
					Network: testNW,
					Address: netip.AddrFrom4([4]byte{1, 2, 3, 4}).AsSlice(),
				}},
				LoadBalancing: &workloadapi.LoadBalancing{
					RoutingPreference: []workloadapi.LoadBalancing_Scope{
						workloadapi.LoadBalancing_NETWORK,
						workloadapi.LoadBalancing_REGION,
						workloadapi.LoadBalancing_ZONE,
						workloadapi.LoadBalancing_SUBZONE,
						workloadapi.LoadBalancing_NODE,
					},
					Mode: workloadapi.LoadBalancing_FAILOVER,
				},
				Ports: []*workloadapi.Port{{
					ServicePort: 80,
				}},
			},
		},
		{
			name:   "publishNotReadyAddresses",
			inputs: []any{},
			svc: &v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "name",
					Namespace: "ns",
				},
				Spec: v1.ServiceSpec{
					PublishNotReadyAddresses: true,
					ClusterIP:                "1.2.3.4",
					Ports: []v1.ServicePort{{
						Port: 80,
						Name: "http",
					}},
				},
			},
			result: &workloadapi.Service{
				Name:      "name",
				Namespace: "ns",
				Hostname:  "name.ns.svc.domain.suffix",
				Addresses: []*workloadapi.NetworkAddress{{
					Network: testNW,
					Address: netip.AddrFrom4([4]byte{1, 2, 3, 4}).AsSlice(),
				}},
				LoadBalancing: &workloadapi.LoadBalancing{
					HealthPolicy: workloadapi.LoadBalancing_ALLOW_ALL,
				},
				Ports: []*workloadapi.Port{{
					ServicePort: 80,
				}},
			},
		},
		{
			name: "cross namespace waypoint",
			inputs: []any{
				Waypoint{
					Named: krt.Named{
						Name:      "waypoint",
						Namespace: "waypoint-ns",
					},
					TrafficType: constants.AllTraffic,
					Address:     waypointAddr,
					AllowedRoutes: WaypointSelector{
						FromNamespaces: gatewayv1.NamespacesFromSelector,
						Selector:       labels.ValidatedSetSelector(map[string]string{v1.LabelMetadataName: "ns"}),
					},
				},
				&v1.Namespace{
					ObjectMeta: metav1.ObjectMeta{
						Name: "ns",
						Labels: map[string]string{
							v1.LabelMetadataName: "ns",
						},
					},
				},
			},
			svc: &v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "name",
					Namespace: "ns",
					Labels: map[string]string{
						label.IoIstioUseWaypoint.Name:          "waypoint",
						label.IoIstioUseWaypointNamespace.Name: "waypoint-ns",
					},
				},
				Spec: v1.ServiceSpec{
					ClusterIP: "1.2.3.4",
					Ports: []v1.ServicePort{{
						Port: 80,
						Name: "http",
					}},
				},
			},
			result: &workloadapi.Service{
				Name:      "name",
				Namespace: "ns",
				Hostname:  "name.ns.svc.domain.suffix",
				Addresses: []*workloadapi.NetworkAddress{{
					Network: testNW,
					Address: netip.AddrFrom4([4]byte{1, 2, 3, 4}).AsSlice(),
				}},
				Waypoint: waypointAddr,
				Ports: []*workloadapi.Port{{
					ServicePort: 80,
				}},
			},
		},
		{
			name: "cross namespace waypoint denied",
			inputs: []any{
				Waypoint{
					Named: krt.Named{
						Name:      "waypoint",
						Namespace: "waypoint-ns",
					},
					TrafficType: constants.AllTraffic,
					Address:     waypointAddr,
					AllowedRoutes: WaypointSelector{
						FromNamespaces: gatewayv1.NamespacesFromSelector,
						Selector:       labels.ValidatedSetSelector(map[string]string{v1.LabelMetadataName: "not-ns"}),
					},
				},
				&v1.Namespace{
					ObjectMeta: metav1.ObjectMeta{
						Name: "ns",
						Labels: map[string]string{
							v1.LabelMetadataName: "ns",
						},
					},
				},
			},
			svc: &v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "name",
					Namespace: "ns",
					Labels: map[string]string{
						label.IoIstioUseWaypoint.Name:          "waypoint",
						label.IoIstioUseWaypointNamespace.Name: "waypoint-ns",
					},
				},
				Spec: v1.ServiceSpec{
					ClusterIP: "1.2.3.4",
					Ports: []v1.ServicePort{{
						Port: 80,
						Name: "http",
					}},
				},
			},
			result: &workloadapi.Service{
				Name:      "name",
				Namespace: "ns",
				Hostname:  "name.ns.svc.domain.suffix",
				Addresses: []*workloadapi.NetworkAddress{{
					Network: testNW,
					Address: netip.AddrFrom4([4]byte{1, 2, 3, 4}).AsSlice(),
				}},
				Waypoint: nil,
				Ports: []*workloadapi.Port{{
					ServicePort: 80,
				}},
			},
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			mock := krttest.NewMock(t, tt.inputs)
			a := newAmbientUnitTest(t)
			builder := a.serviceServiceBuilder(
				krttest.GetMockCollection[Waypoint](mock),
				krttest.GetMockCollection[*v1.Namespace](mock),
			)
			res := builder(krt.TestingDummyContext{}, tt.svc)
			if res == nil {
				assert.Equal(t, nil, tt.result)
			} else {
				assert.Equal(t, res.Service, tt.result)
			}
		})
	}
}

func TestServiceConditions(t *testing.T) {
	waypointAddr := &workloadapi.GatewayAddress{
		Destination: &workloadapi.GatewayAddress_Hostname{
			Hostname: &workloadapi.NamespacedHostname{
				Namespace: "ns",
				Hostname:  "hostname.example",
			},
		},
		// TODO: look up the HBONE port instead of hardcoding it
		HboneMtlsPort: 15008,
	}

	waypoint := Waypoint{
		Named: krt.Named{
			Name:      "waypoint",
			Namespace: "waypoint-ns",
		},
		TrafficType: constants.AllTraffic,
		Address:     waypointAddr,
		AllowedRoutes: WaypointSelector{
			FromNamespaces: gatewayv1.NamespacesFromSelector,
			Selector:       labels.ValidatedSetSelector(map[string]string{v1.LabelMetadataName: "ns"}),
		},
	}

	ns := &v1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "ns",
			Labels: map[string]string{
				v1.LabelMetadataName: "ns",
			},
		},
	}

	makeServiceWithLabels := func(labels map[string]string) *v1.Service {
		return &v1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "name",
				Namespace: "ns",
				Labels:    labels,
			},
			Spec: v1.ServiceSpec{
				ClusterIP: "1.2.3.4",
				Ports: []v1.ServicePort{{
					Port: 80,
					Name: "http",
				}},
			},
		}
	}

	cases := []struct {
		name       string
		inputs     []any
		svc        *v1.Service
		result     *workloadapi.Service
		conditions model.ConditionSet
	}{
		{
			name: "service bound to waypoint",
			inputs: []any{
				waypoint,
				ns,
			},
			svc: makeServiceWithLabels(map[string]string{
				label.IoIstioUseWaypoint.Name:          "waypoint",
				label.IoIstioUseWaypointNamespace.Name: "waypoint-ns",
			}),
			conditions: map[model.ConditionType]*model.Condition{
				model.WaypointBound: {
					Status:  true,
					Reason:  string(model.WaypointAccepted),
					Message: "Successfully attached to waypoint waypoint-ns/waypoint",
				},
			},
		},
		{
			name: "service bound to waypoint and using waypoint for ingress",
			inputs: []any{
				waypoint,
				ns,
			},
			svc: makeServiceWithLabels(map[string]string{
				label.IoIstioUseWaypoint.Name:          "waypoint",
				label.IoIstioUseWaypointNamespace.Name: "waypoint-ns",
				"istio.io/ingress-use-waypoint":        "true",
			}),
			conditions: map[model.ConditionType]*model.Condition{
				model.WaypointBound: {
					Status:  true,
					Reason:  string(model.WaypointAccepted),
					Message: "Successfully attached to waypoint waypoint-ns/waypoint. Ingress traffic will traverse the waypoint",
				},
			},
		},
		{
			name: "service bound to waypoint and ingress label is malformed",
			inputs: []any{
				waypoint,
				ns,
			},
			svc: makeServiceWithLabels(map[string]string{
				label.IoIstioUseWaypoint.Name:          "waypoint",
				label.IoIstioUseWaypointNamespace.Name: "waypoint-ns",
				"istio.io/ingress-use-waypoint":        "eurt",
			}),
			conditions: map[model.ConditionType]*model.Condition{
				model.WaypointBound: {
					Status: true,
					Reason: string(model.WaypointAccepted),
					Message: "Successfully attached to waypoint waypoint-ns/waypoint. " +
						"Ingress traffic is not using the waypoint, set the istio.io/ingress-use-waypoint label to true if desired.",
				},
			},
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			mock := krttest.NewMock(t, tt.inputs)
			a := newAmbientUnitTest(t)
			builder := a.serviceServiceBuilder(
				krttest.GetMockCollection[Waypoint](mock),
				krttest.GetMockCollection[*v1.Namespace](mock),
			)
			res := builder(krt.TestingDummyContext{}, tt.svc)
			assert.Equal(t, res.GetConditions(), tt.conditions)
		})
	}
}
