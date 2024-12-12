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
	discovery "k8s.io/api/discovery/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	"istio.io/api/label"
	networking "istio.io/api/networking/v1alpha3"
	networkingclient "istio.io/client-go/pkg/apis/networking/v1"
	securityclient "istio.io/client-go/pkg/apis/security/v1"
	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/config/labels"
	"istio.io/istio/pkg/config/mesh"
	"istio.io/istio/pkg/config/schema/kind"
	"istio.io/istio/pkg/kube/krt"
	"istio.io/istio/pkg/kube/krt/krttest"
	"istio.io/istio/pkg/network"
	"istio.io/istio/pkg/ptr"
	"istio.io/istio/pkg/slices"
	"istio.io/istio/pkg/test/util/assert"
	"istio.io/istio/pkg/workloadapi"
	"istio.io/istio/pkg/workloadapi/security"
)

func TestPodWorkloads(t *testing.T) {
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
		pod    *v1.Pod
		result *workloadapi.Workload
	}{
		{
			name:   "simple pod not running and not have podIP",
			inputs: []any{},
			pod: &v1.Pod{
				TypeMeta: metav1.TypeMeta{},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "name",
					Namespace: "ns",
				},
				Spec: v1.PodSpec{},
				Status: v1.PodStatus{
					Phase: v1.PodPending,
				},
			},
			result: nil,
		},
		{
			name:   "simple pod not running but have podIP",
			inputs: []any{},
			pod: &v1.Pod{
				TypeMeta: metav1.TypeMeta{},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "name",
					Namespace: "ns",
				},
				Spec: v1.PodSpec{},
				Status: v1.PodStatus{
					Phase: v1.PodPending,
					PodIP: "1.2.3.4",
				},
			},
			result: &workloadapi.Workload{
				Uid:               "cluster0//Pod/ns/name",
				Name:              "name",
				Namespace:         "ns",
				Addresses:         [][]byte{netip.AddrFrom4([4]byte{1, 2, 3, 4}).AsSlice()},
				Network:           testNW,
				CanonicalName:     "name",
				CanonicalRevision: "latest",
				WorkloadType:      workloadapi.WorkloadType_POD,
				WorkloadName:      "name",
				Status:            workloadapi.WorkloadStatus_UNHEALTHY,
				ClusterId:         testC,
			},
		},
		{
			name:   "simple pod not ready",
			inputs: []any{},
			pod: &v1.Pod{
				TypeMeta: metav1.TypeMeta{},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "name",
					Namespace: "ns",
				},
				Spec: v1.PodSpec{},
				Status: v1.PodStatus{
					Phase: v1.PodRunning,
					PodIP: "1.2.3.4",
				},
			},
			result: &workloadapi.Workload{
				Uid:               "cluster0//Pod/ns/name",
				Name:              "name",
				Namespace:         "ns",
				Addresses:         [][]byte{netip.AddrFrom4([4]byte{1, 2, 3, 4}).AsSlice()},
				Network:           testNW,
				CanonicalName:     "name",
				CanonicalRevision: "latest",
				WorkloadType:      workloadapi.WorkloadType_POD,
				WorkloadName:      "name",
				Status:            workloadapi.WorkloadStatus_UNHEALTHY,
				ClusterId:         testC,
			},
		},
		{
			name: "pod with service",
			inputs: []any{
				model.ServiceInfo{
					Service: &workloadapi.Service{
						Name:      "svc",
						Namespace: "ns",
						Hostname:  "hostname",
						Ports: []*workloadapi.Port{{
							ServicePort: 80,
							TargetPort:  8080,
						}},
					},
					LabelSelector: model.NewSelector(map[string]string{"app": "foo"}),
				},
			},
			pod: &v1.Pod{
				TypeMeta: metav1.TypeMeta{},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "name",
					Namespace: "ns",
					Labels: map[string]string{
						"app": "foo",
					},
				},
				Spec: v1.PodSpec{},
				Status: v1.PodStatus{
					Phase:      v1.PodRunning,
					Conditions: podReady,
					PodIP:      "1.2.3.4",
				},
			},
			result: &workloadapi.Workload{
				Uid:               "cluster0//Pod/ns/name",
				Name:              "name",
				Namespace:         "ns",
				Addresses:         [][]byte{netip.AddrFrom4([4]byte{1, 2, 3, 4}).AsSlice()},
				Network:           testNW,
				CanonicalName:     "foo",
				CanonicalRevision: "latest",
				WorkloadType:      workloadapi.WorkloadType_POD,
				WorkloadName:      "name",
				Status:            workloadapi.WorkloadStatus_HEALTHY,
				ClusterId:         testC,
				Services: map[string]*workloadapi.PortList{
					"ns/hostname": {
						Ports: []*workloadapi.Port{{
							ServicePort: 80,
							TargetPort:  8080,
						}},
					},
				},
			},
		},
		{
			name: "pod with service named ports",
			inputs: []any{
				model.ServiceInfo{
					Service: &workloadapi.Service{
						Name:      "svc",
						Namespace: "ns",
						Hostname:  "hostname",
						Ports: []*workloadapi.Port{
							{
								ServicePort: 80,
								TargetPort:  8080,
							},
							{
								ServicePort: 81,
								TargetPort:  0,
							},
							{
								ServicePort: 82,
								TargetPort:  0,
							},
						},
					},
					PortNames: map[int32]model.ServicePortName{
						// Not a named port
						80: {PortName: "80"},
						// Named port found in pod
						81: {PortName: "81", TargetPortName: "81-target"},
						// Named port not found in pod
						82: {PortName: "82", TargetPortName: "82-target"},
					},
					LabelSelector: model.NewSelector(map[string]string{"app": "foo"}),
				},
			},
			pod: &v1.Pod{
				TypeMeta: metav1.TypeMeta{},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "name",
					Namespace: "ns",
					Labels: map[string]string{
						"app": "foo",
					},
				},
				Spec: v1.PodSpec{
					Containers: []v1.Container{{Ports: []v1.ContainerPort{
						{
							Name:          "81-target",
							ContainerPort: 9090,
							Protocol:      v1.ProtocolTCP,
						},
					}}},
				},
				Status: v1.PodStatus{
					Phase:      v1.PodRunning,
					Conditions: podReady,
					PodIP:      "1.2.3.4",
				},
			},
			result: &workloadapi.Workload{
				Uid:               "cluster0//Pod/ns/name",
				Name:              "name",
				Namespace:         "ns",
				Addresses:         [][]byte{netip.AddrFrom4([4]byte{1, 2, 3, 4}).AsSlice()},
				Network:           testNW,
				CanonicalName:     "foo",
				CanonicalRevision: "latest",
				WorkloadType:      workloadapi.WorkloadType_POD,
				WorkloadName:      "name",
				Status:            workloadapi.WorkloadStatus_HEALTHY,
				ClusterId:         testC,
				Services: map[string]*workloadapi.PortList{
					"ns/hostname": {
						Ports: []*workloadapi.Port{{
							ServicePort: 80,
							TargetPort:  8080,
						}, {
							ServicePort: 81,
							TargetPort:  9090,
						}},
					},
				},
			},
		},
		{
			name: "simple pod with locality",
			inputs: []any{
				&v1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Name: "node",
						Labels: map[string]string{
							v1.LabelTopologyRegion: "region",
							v1.LabelTopologyZone:   "zone",
						},
					},
				},
			},
			pod: &v1.Pod{
				TypeMeta: metav1.TypeMeta{},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "name",
					Namespace: "ns",
				},
				Spec: v1.PodSpec{NodeName: "node"},
				Status: v1.PodStatus{
					Phase: v1.PodPending,
					PodIP: "1.2.3.4",
				},
			},
			result: &workloadapi.Workload{
				Uid:               "cluster0//Pod/ns/name",
				Name:              "name",
				Namespace:         "ns",
				Node:              "node",
				Addresses:         [][]byte{netip.AddrFrom4([4]byte{1, 2, 3, 4}).AsSlice()},
				Network:           testNW,
				CanonicalName:     "name",
				CanonicalRevision: "latest",
				WorkloadType:      workloadapi.WorkloadType_POD,
				WorkloadName:      "name",
				Status:            workloadapi.WorkloadStatus_UNHEALTHY,
				ClusterId:         testC,
				Locality: &workloadapi.Locality{
					Region: "region",
					Zone:   "zone",
				},
			},
		},
		{
			name: "pod with authz",
			inputs: []any{
				model.WorkloadAuthorization{
					LabelSelector: model.NewSelector(map[string]string{"app": "foo"}),
					Authorization: &security.Authorization{Name: "wrong-ns", Namespace: "not-ns"},
				},
				model.WorkloadAuthorization{
					LabelSelector: model.NewSelector(map[string]string{"app": "foo"}),
					Authorization: &security.Authorization{Name: "local-ns", Namespace: "ns"},
				},
				model.WorkloadAuthorization{
					LabelSelector: model.NewSelector(map[string]string{"app": "not-foo"}),
					Authorization: &security.Authorization{Name: "local-ns-wrong-labels", Namespace: "ns"},
				},
				model.WorkloadAuthorization{
					LabelSelector: model.NewSelector(map[string]string{"app": "foo"}),
					Authorization: &security.Authorization{Name: "root-ns", Namespace: "istio-system"},
				},
			},
			pod: &v1.Pod{
				TypeMeta: metav1.TypeMeta{},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "name",
					Namespace: "ns",
					Labels: map[string]string{
						"app": "foo",
					},
				},
				Spec: v1.PodSpec{},
				Status: v1.PodStatus{
					Phase:      v1.PodRunning,
					Conditions: podReady,
					PodIP:      "1.2.3.4",
				},
			},
			result: &workloadapi.Workload{
				Uid:               "cluster0//Pod/ns/name",
				Name:              "name",
				Namespace:         "ns",
				Addresses:         [][]byte{netip.AddrFrom4([4]byte{1, 2, 3, 4}).AsSlice()},
				Network:           testNW,
				CanonicalName:     "foo",
				CanonicalRevision: "latest",
				WorkloadType:      workloadapi.WorkloadType_POD,
				WorkloadName:      "name",
				Status:            workloadapi.WorkloadStatus_HEALTHY,
				ClusterId:         testC,
				AuthorizationPolicies: []string{
					"istio-system/root-ns",
					"ns/local-ns",
				},
			},
		},
		{
			name: "pod with waypoint",
			inputs: []any{
				Waypoint{
					Named: krt.Named{
						Name:      "waypoint",
						Namespace: "ns",
					},
					TrafficType: constants.AllTraffic,
					Address:     waypointAddr,
				},
			},
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "name",
					Namespace: "ns",
					Labels: map[string]string{
						"app":                         "foo",
						label.IoIstioUseWaypoint.Name: "waypoint",
					},
				},
				Spec: v1.PodSpec{},
				Status: v1.PodStatus{
					Phase:      v1.PodRunning,
					Conditions: podReady,
					PodIP:      "1.2.3.4",
				},
			},
			result: &workloadapi.Workload{
				Uid:               "cluster0//Pod/ns/name",
				Name:              "name",
				Namespace:         "ns",
				Addresses:         [][]byte{netip.AddrFrom4([4]byte{1, 2, 3, 4}).AsSlice()},
				Network:           testNW,
				CanonicalName:     "foo",
				CanonicalRevision: "latest",
				WorkloadType:      workloadapi.WorkloadType_POD,
				WorkloadName:      "name",
				Status:            workloadapi.WorkloadStatus_HEALTHY,
				ClusterId:         testC,
				Waypoint:          waypointAddr,
			},
		},
		{
			name: "pod that is a waypoint",
			inputs: []any{
				Waypoint{
					Named: krt.Named{
						Name:      "waypoint",
						Namespace: "ns",
					},
					TrafficType: constants.AllTraffic,
					Address:     waypointAddr,
				},
			},
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					// This pod *is* the waypoint
					Name:      "waypoint",
					Namespace: "ns",
					Labels: map[string]string{
						label.IoK8sNetworkingGatewayGatewayName.Name: "waypoint",
					},
				},
				Spec: v1.PodSpec{},
				Status: v1.PodStatus{
					Phase:      v1.PodRunning,
					Conditions: podReady,
					PodIP:      "1.2.3.4",
				},
			},
			result: &workloadapi.Workload{
				Uid:               "cluster0//Pod/ns/waypoint",
				Name:              "waypoint",
				Namespace:         "ns",
				Addresses:         [][]byte{netip.AddrFrom4([4]byte{1, 2, 3, 4}).AsSlice()},
				Network:           testNW,
				CanonicalName:     "waypoint",
				CanonicalRevision: "latest",
				WorkloadType:      workloadapi.WorkloadType_POD,
				WorkloadName:      "waypoint",
				Status:            workloadapi.WorkloadStatus_HEALTHY,
				ClusterId:         testC,
			},
		},
		{
			name: "pod as part of selectorless service",
			inputs: []any{
				model.ServiceInfo{
					Service: &workloadapi.Service{
						Name:      "svc",
						Namespace: "default",
						Hostname:  "svc.default.svc.domain.suffix",
						Ports: []*workloadapi.Port{
							{
								ServicePort: 80,
								TargetPort:  80,
							},
						},
					},
					PortNames: map[int32]model.ServicePortName{
						80: {PortName: "80"},
					},
					// no selector!
					LabelSelector: model.LabelSelector{},
					Source:        model.TypedObject{Kind: kind.Service},
				},
				// EndpointSlice manually associates the pod with a service
				&discovery.EndpointSlice{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "svc-123",
						Namespace: "default",
						Labels: map[string]string{
							discovery.LabelServiceName: "svc",
						},
					},
					AddressType: discovery.AddressTypeIPv4,
					Endpoints: []discovery.Endpoint{
						{
							Addresses: []string{"1.2.3.4"},
							Conditions: discovery.EndpointConditions{
								Ready: ptr.Of(true),
							},
							TargetRef: &v1.ObjectReference{
								Kind:      "Pod",
								Name:      "pod-123",
								Namespace: "default",
							},
						},
					},
					Ports: []discovery.EndpointPort{
						{
							Name:     ptr.Of("http"),
							Protocol: ptr.Of(v1.ProtocolTCP),
							Port:     ptr.Of(int32(80)),
						},
					},
				},
			},
			pod: &v1.Pod{
				TypeMeta: metav1.TypeMeta{},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "pod-123",
					Namespace: "default",
					Labels: map[string]string{
						"app": "foo",
					},
				},
				Spec: v1.PodSpec{},
				Status: v1.PodStatus{
					Phase:      v1.PodRunning,
					Conditions: podReady,
					PodIP:      "1.2.3.4",
				},
			},
			result: &workloadapi.Workload{
				Uid:               "cluster0//Pod/default/pod-123",
				Name:              "pod-123",
				Namespace:         "default",
				Addresses:         [][]byte{netip.AddrFrom4([4]byte{1, 2, 3, 4}).AsSlice()},
				Network:           testNW,
				CanonicalName:     "foo",
				CanonicalRevision: "latest",
				WorkloadType:      workloadapi.WorkloadType_POD,
				WorkloadName:      "pod-123",
				Status:            workloadapi.WorkloadStatus_HEALTHY,
				ClusterId:         testC,
				Services: map[string]*workloadapi.PortList{
					"default/svc.default.svc.domain.suffix": {
						Ports: []*workloadapi.Port{{
							ServicePort: 80,
							TargetPort:  80,
						}},
					},
				},
			},
		},
		{
			name: "pod as part of selectorless service without targetRef",
			inputs: []any{
				model.ServiceInfo{
					Service: &workloadapi.Service{
						Name:      "svc",
						Namespace: "default",
						Hostname:  "svc.default.svc.domain.suffix",
						Ports: []*workloadapi.Port{
							{
								ServicePort: 80,
								TargetPort:  80,
							},
						},
					},
					PortNames: map[int32]model.ServicePortName{
						80: {PortName: "80"},
					},
					// no selector!
					LabelSelector: model.LabelSelector{},
					Source:        model.TypedObject{Kind: kind.Service},
				},
				// EndpointSlice manually created with the IP of the pod, but does NOT have a targetRef
				&discovery.EndpointSlice{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "svc-123",
						Namespace: "default",
						Labels: map[string]string{
							discovery.LabelServiceName: "svc",
						},
					},
					AddressType: discovery.AddressTypeIPv4,
					Endpoints: []discovery.Endpoint{
						{
							Addresses: []string{"1.2.3.4"},
							Conditions: discovery.EndpointConditions{
								Ready: ptr.Of(true),
							},
						},
					},
					Ports: []discovery.EndpointPort{
						{
							Name:     ptr.Of("http"),
							Protocol: ptr.Of(v1.ProtocolTCP),
							Port:     ptr.Of(int32(80)),
						},
					},
				},
			},
			pod: &v1.Pod{
				TypeMeta: metav1.TypeMeta{},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "pod-123",
					Namespace: "default",
					Labels: map[string]string{
						"app": "foo",
					},
				},
				Spec: v1.PodSpec{},
				Status: v1.PodStatus{
					Phase:      v1.PodRunning,
					Conditions: podReady,
					PodIP:      "1.2.3.4",
				},
			},
			result: &workloadapi.Workload{
				Uid:               "cluster0//Pod/default/pod-123",
				Name:              "pod-123",
				Namespace:         "default",
				Addresses:         [][]byte{netip.AddrFrom4([4]byte{1, 2, 3, 4}).AsSlice()},
				Network:           testNW,
				CanonicalName:     "foo",
				CanonicalRevision: "latest",
				WorkloadType:      workloadapi.WorkloadType_POD,
				WorkloadName:      "pod-123",
				Status:            workloadapi.WorkloadStatus_HEALTHY,
				ClusterId:         testC,
				// We do NOT associate this with the service.
				// However, there will be a corresponding Workload created from the raw EndpointSlice. See TestEndpointSliceWorkloads
				// for corresponding test.
				Services: nil,
			},
		},
		{
			name: "host network pod",
			inputs: []any{
				model.ServiceInfo{
					Service: &workloadapi.Service{
						Name:      "svc",
						Namespace: "default",
						Hostname:  "svc.default.svc.domain.suffix",
						Ports:     []*workloadapi.Port{{ServicePort: 80, TargetPort: 80}},
					},
					PortNames: map[int32]model.ServicePortName{
						80: {PortName: "80"},
					},
					LabelSelector: model.NewSelector(map[string]string{"app": "foo"}),
					Source:        model.TypedObject{Kind: kind.Service},
				},
				// Another endpointslice exists with the same IP... This should have no impact
				kubernetesAPIServerEndpoint("1.1.1.1"),
				kubernetesAPIServerService("1.2.3.4"),
				// Another Service with an endpointslice for the same IP and for *a* pod, but not this pod. Again, no impact
				model.ServiceInfo{
					Service: &workloadapi.Service{
						Name:      "some-other-svc",
						Namespace: "default",
						Hostname:  "some-other-svc.default.svc.domain.suffix",
						Ports:     []*workloadapi.Port{{ServicePort: 80, TargetPort: 80}},
					},
					PortNames: map[int32]model.ServicePortName{
						80: {PortName: "80"},
					},
					Source: model.TypedObject{Kind: kind.Service},
				},
				&discovery.EndpointSlice{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "some-other-svc",
						Namespace: "default",
						Labels: map[string]string{
							discovery.LabelServiceName: "some-other-svc",
						},
					},
					AddressType: discovery.AddressTypeIPv4,
					Endpoints: []discovery.Endpoint{
						{
							Addresses: []string{"1.1.1.1"},
							Conditions: discovery.EndpointConditions{
								Ready: ptr.Of(true),
							},
							TargetRef: &v1.ObjectReference{
								Kind:      "Pod",
								Name:      "not-the-same-pod",
								Namespace: "default",
							},
						},
					},
					Ports: []discovery.EndpointPort{
						{
							Name:     ptr.Of("80"),
							Protocol: ptr.Of(v1.ProtocolTCP),
							Port:     ptr.Of(int32(80)),
						},
					},
				},
			},
			pod: &v1.Pod{
				TypeMeta: metav1.TypeMeta{},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "pod-123",
					Namespace: "default",
					Labels: map[string]string{
						"app": "foo",
					},
				},
				Spec: v1.PodSpec{HostNetwork: true},
				Status: v1.PodStatus{
					Phase:      v1.PodRunning,
					Conditions: podReady,
					// Important: aligns with the kubernetesAPIServerEndpoint above
					PodIP: "1.1.1.1",
				},
			},
			result: &workloadapi.Workload{
				Uid:               "cluster0//Pod/default/pod-123",
				Name:              "pod-123",
				Namespace:         "default",
				Addresses:         [][]byte{netip.AddrFrom4([4]byte{1, 1, 1, 1}).AsSlice()},
				Network:           testNW,
				CanonicalName:     "foo",
				CanonicalRevision: "latest",
				WorkloadType:      workloadapi.WorkloadType_POD,
				WorkloadName:      "pod-123",
				Status:            workloadapi.WorkloadStatus_HEALTHY,
				NetworkMode:       workloadapi.NetworkMode_HOST_NETWORK,
				ClusterId:         testC,
				Services: map[string]*workloadapi.PortList{
					"default/svc.default.svc.domain.suffix": {
						Ports: []*workloadapi.Port{{
							ServicePort: 80,
							TargetPort:  80,
						}},
					},
				},
			},
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			mock := krttest.NewMock(t, tt.inputs)
			a := newAmbientUnitTest()
			WorkloadServices := krttest.GetMockCollection[model.ServiceInfo](mock)
			WorkloadServicesNamespaceIndex := krt.NewNamespaceIndex(WorkloadServices)
			EndpointSlices := krttest.GetMockCollection[*discovery.EndpointSlice](mock)
			EndpointSlicesAddressIndex := endpointSliceAddressIndex(EndpointSlices)
			builder := a.podWorkloadBuilder(
				GetMeshConfig(mock),
				krttest.GetMockCollection[model.WorkloadAuthorization](mock),
				krttest.GetMockCollection[*securityclient.PeerAuthentication](mock),
				krttest.GetMockCollection[Waypoint](mock),
				WorkloadServices,
				WorkloadServicesNamespaceIndex,
				EndpointSlices,
				EndpointSlicesAddressIndex,
				krttest.GetMockCollection[*v1.Namespace](mock),
				krttest.GetMockCollection[*v1.Node](mock),
			)
			wrapper := builder(krt.TestingDummyContext{}, tt.pod)
			var res *workloadapi.Workload
			if wrapper != nil {
				res = wrapper.Workload
			}
			assert.Equal(t, res, tt.result)
		})
	}
}

func TestWorkloadEntryWorkloads(t *testing.T) {
	cases := []struct {
		name   string
		inputs []any
		we     *networkingclient.WorkloadEntry
		result *workloadapi.Workload
	}{
		{
			name: "we with service",
			inputs: []any{
				model.ServiceInfo{
					Service: &workloadapi.Service{
						Name:      "svc",
						Namespace: "ns",
						Hostname:  "hostname",
						Ports: []*workloadapi.Port{{
							ServicePort: 80,
							TargetPort:  8080,
						}},
					},
					LabelSelector: model.NewSelector(map[string]string{"app": "foo"}),
				},
			},
			we: &networkingclient.WorkloadEntry{
				TypeMeta: metav1.TypeMeta{},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "name",
					Namespace: "ns",
					Labels: map[string]string{
						"app": "foo",
					},
				},
				Spec: networking.WorkloadEntry{
					Address: "1.2.3.4",
				},
			},
			result: &workloadapi.Workload{
				Uid:               "cluster0/networking.istio.io/WorkloadEntry/ns/name",
				Name:              "name",
				Namespace:         "ns",
				Addresses:         [][]byte{netip.AddrFrom4([4]byte{1, 2, 3, 4}).AsSlice()},
				Network:           testNW,
				CanonicalName:     "foo",
				CanonicalRevision: "latest",
				WorkloadType:      workloadapi.WorkloadType_POD,
				WorkloadName:      "name",
				Status:            workloadapi.WorkloadStatus_HEALTHY,
				ClusterId:         testC,
				Services: map[string]*workloadapi.PortList{
					"ns/hostname": {
						Ports: []*workloadapi.Port{{
							ServicePort: 80,
							TargetPort:  8080,
						}},
					},
				},
			},
		},
		{
			name: "we without labels",
			inputs: []any{
				model.ServiceInfo{
					Service: &workloadapi.Service{
						Name:      "svc",
						Namespace: "ns",
						Hostname:  "hostname",
						Ports: []*workloadapi.Port{{
							ServicePort: 80,
							TargetPort:  8080,
						}},
					},
					LabelSelector: model.NewSelector(map[string]string{"app": "foo"}),
				},
			},
			we: &networkingclient.WorkloadEntry{
				TypeMeta: metav1.TypeMeta{},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "name",
					Namespace: "ns",
				},
				Spec: networking.WorkloadEntry{
					Address: "1.2.3.4",
				},
			},
			result: &workloadapi.Workload{
				Uid:               "cluster0/networking.istio.io/WorkloadEntry/ns/name",
				Name:              "name",
				Namespace:         "ns",
				Addresses:         [][]byte{netip.AddrFrom4([4]byte{1, 2, 3, 4}).AsSlice()},
				Network:           testNW,
				CanonicalName:     "name",
				CanonicalRevision: "latest",
				WorkloadType:      workloadapi.WorkloadType_POD,
				WorkloadName:      "name",
				Status:            workloadapi.WorkloadStatus_HEALTHY,
				ClusterId:         testC,
			},
		},
		{
			name: "we spec labels",
			inputs: []any{
				model.ServiceInfo{
					Service: &workloadapi.Service{
						Name:      "svc",
						Namespace: "ns",
						Hostname:  "hostname",
						Ports: []*workloadapi.Port{{
							ServicePort: 80,
							TargetPort:  8080,
						}},
					},
					LabelSelector: model.NewSelector(map[string]string{"app": "foo"}),
				},
			},
			we: &networkingclient.WorkloadEntry{
				TypeMeta: metav1.TypeMeta{},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "name",
					Namespace: "ns",
				},
				Spec: networking.WorkloadEntry{
					Address: "1.2.3.4",
					// Labels in spec instead of metadata
					Labels: map[string]string{
						"app": "foo",
					},
				},
			},
			result: &workloadapi.Workload{
				Uid:               "cluster0/networking.istio.io/WorkloadEntry/ns/name",
				Name:              "name",
				Namespace:         "ns",
				Addresses:         [][]byte{netip.AddrFrom4([4]byte{1, 2, 3, 4}).AsSlice()},
				Network:           testNW,
				CanonicalName:     "foo",
				CanonicalRevision: "latest",
				WorkloadType:      workloadapi.WorkloadType_POD,
				WorkloadName:      "name",
				Status:            workloadapi.WorkloadStatus_HEALTHY,
				ClusterId:         testC,
				Services: map[string]*workloadapi.PortList{
					"ns/hostname": {
						Ports: []*workloadapi.Port{{
							ServicePort: 80,
							TargetPort:  8080,
						}},
					},
				},
			},
		},
		{
			name: "pod with service named ports",
			inputs: []any{
				model.ServiceInfo{
					Service: &workloadapi.Service{
						Name:      "svc",
						Namespace: "ns",
						Hostname:  "hostname",
						Ports: []*workloadapi.Port{
							{
								ServicePort: 80,
								TargetPort:  8080,
							},
							{
								ServicePort: 81,
								TargetPort:  0,
							},
							{
								ServicePort: 82,
								TargetPort:  0,
							},
							{
								ServicePort: 83,
								TargetPort:  0,
							},
						},
					},
					PortNames: map[int32]model.ServicePortName{
						// Not a named port
						80: {PortName: "80"},
						// Named port found in WE
						81: {PortName: "81", TargetPortName: "81-target"},
						// Named port target found in WE
						82: {PortName: "82", TargetPortName: "82-target"},
						// Named port not found in WE
						83: {PortName: "83", TargetPortName: "83-target"},
					},
					LabelSelector: model.NewSelector(map[string]string{"app": "foo"}),
					Source:        model.TypedObject{Kind: kind.Service},
				},
			},
			we: &networkingclient.WorkloadEntry{
				TypeMeta: metav1.TypeMeta{},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "name",
					Namespace: "ns",
					Labels: map[string]string{
						"app": "foo",
					},
				},
				Spec: networking.WorkloadEntry{
					Ports: map[string]uint32{
						"81":        8180,
						"82-target": 8280,
					},
					Address: "1.2.3.4",
				},
			},
			result: &workloadapi.Workload{
				Uid:               "cluster0/networking.istio.io/WorkloadEntry/ns/name",
				Name:              "name",
				Namespace:         "ns",
				Addresses:         [][]byte{netip.AddrFrom4([4]byte{1, 2, 3, 4}).AsSlice()},
				Network:           testNW,
				CanonicalName:     "foo",
				CanonicalRevision: "latest",
				WorkloadType:      workloadapi.WorkloadType_POD,
				WorkloadName:      "name",
				Status:            workloadapi.WorkloadStatus_HEALTHY,
				ClusterId:         testC,
				Services: map[string]*workloadapi.PortList{
					"ns/hostname": {
						Ports: []*workloadapi.Port{
							{
								ServicePort: 80,
								TargetPort:  8080,
							},
							{
								ServicePort: 82,
								TargetPort:  8280,
							},
						},
					},
				},
			},
		},
		{
			name: "pod with serviceentry named ports",
			inputs: []any{
				model.ServiceInfo{
					Service: &workloadapi.Service{
						Name:      "svc",
						Namespace: "ns",
						Hostname:  "hostname",
						Ports: []*workloadapi.Port{
							{
								ServicePort: 80,
								TargetPort:  8080,
							},
							{
								ServicePort: 81,
								TargetPort:  0,
							},
							{
								ServicePort: 82,
								TargetPort:  0,
							},
						},
					},
					PortNames: map[int32]model.ServicePortName{
						// TargetPort explicitly set
						80: {PortName: "80"},
						// Port name found
						81: {PortName: "81"},
						// Port name not found
						82: {PortName: "82"},
					},
					LabelSelector: model.NewSelector(map[string]string{"app": "foo"}),
					Source:        model.TypedObject{Kind: kind.ServiceEntry},
				},
			},
			we: &networkingclient.WorkloadEntry{
				TypeMeta: metav1.TypeMeta{},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "name",
					Namespace: "ns",
					Labels: map[string]string{
						"app": "foo",
					},
				},
				Spec: networking.WorkloadEntry{
					Ports: map[string]uint32{
						"81": 8180,
					},
					Address: "1.2.3.4",
				},
			},
			result: &workloadapi.Workload{
				Uid:               "cluster0/networking.istio.io/WorkloadEntry/ns/name",
				Name:              "name",
				Namespace:         "ns",
				Addresses:         [][]byte{netip.AddrFrom4([4]byte{1, 2, 3, 4}).AsSlice()},
				Network:           testNW,
				CanonicalName:     "foo",
				CanonicalRevision: "latest",
				WorkloadType:      workloadapi.WorkloadType_POD,
				WorkloadName:      "name",
				Status:            workloadapi.WorkloadStatus_HEALTHY,
				ClusterId:         testC,
				Services: map[string]*workloadapi.PortList{
					"ns/hostname": {
						Ports: []*workloadapi.Port{
							{
								ServicePort: 80,
								TargetPort:  8080,
							},
							{
								ServicePort: 81,
								TargetPort:  8180,
							},
							{
								ServicePort: 82,
								TargetPort:  82,
							},
						},
					},
				},
			},
		},
		{
			name:   "cross network we",
			inputs: []any{},
			we: &networkingclient.WorkloadEntry{
				TypeMeta: metav1.TypeMeta{},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "name",
					Namespace: "ns",
				},
				Spec: networking.WorkloadEntry{
					Ports: map[string]uint32{
						"80": 80,
					},
					Network: "remote-network",
				},
			},
			result: &workloadapi.Workload{
				Uid:               "cluster0/networking.istio.io/WorkloadEntry/ns/name",
				Name:              "name",
				Namespace:         "ns",
				Network:           "remote-network",
				CanonicalName:     "name",
				CanonicalRevision: "latest",
				WorkloadType:      workloadapi.WorkloadType_POD,
				WorkloadName:      "name",
				Status:            workloadapi.WorkloadStatus_HEALTHY,
				ClusterId:         testC,
				NetworkGateway: &workloadapi.GatewayAddress{
					Destination: &workloadapi.GatewayAddress_Address{Address: &workloadapi.NetworkAddress{
						Network: "remote-network",
						Address: netip.MustParseAddr("9.9.9.9").AsSlice(),
					}},
					HboneMtlsPort: 15008,
				},
			},
		},
		{
			name:   "cross network we hostname gateway",
			inputs: []any{},
			we: &networkingclient.WorkloadEntry{
				TypeMeta: metav1.TypeMeta{},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "name",
					Namespace: "ns",
				},
				Spec: networking.WorkloadEntry{
					Ports: map[string]uint32{
						"80": 80,
					},
					Network: "remote-network-hostname",
				},
			},
			result: &workloadapi.Workload{
				Uid:               "cluster0/networking.istio.io/WorkloadEntry/ns/name",
				Name:              "name",
				Namespace:         "ns",
				Network:           "remote-network-hostname",
				CanonicalName:     "name",
				CanonicalRevision: "latest",
				WorkloadType:      workloadapi.WorkloadType_POD,
				WorkloadName:      "name",
				Status:            workloadapi.WorkloadStatus_HEALTHY,
				ClusterId:         testC,
				NetworkGateway: &workloadapi.GatewayAddress{
					Destination: &workloadapi.GatewayAddress_Hostname{Hostname: &workloadapi.NamespacedHostname{
						Hostname:  "networkgateway.example.com",
						Namespace: "ns-gtw",
					}},
					HboneMtlsPort: 15008,
				},
			},
		},
		{
			name: "waypoint binding",
			inputs: []any{
				Waypoint{
					Named: krt.Named{Name: "waypoint", Namespace: "ns"},
					Address: &workloadapi.GatewayAddress{
						Destination: &workloadapi.GatewayAddress_Hostname{
							Hostname: &workloadapi.NamespacedHostname{
								Namespace: "ns",
								Hostname:  "waypoint.example.com",
							},
						},
					},
					DefaultBinding: &InboundBinding{Port: 15088, Protocol: workloadapi.ApplicationTunnel_PROXY},
					TrafficType:    constants.AllTraffic,
				},
			},
			we: &networkingclient.WorkloadEntry{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "name",
					Namespace: "ns",
					Labels: map[string]string{
						label.IoK8sNetworkingGatewayGatewayName.Name: "waypoint",
					},
				},
				Spec: networking.WorkloadEntry{
					Ports: map[string]uint32{
						"80": 80,
					},
				},
			},
			result: &workloadapi.Workload{
				Uid:               "cluster0/networking.istio.io/WorkloadEntry/ns/name",
				Name:              "name",
				Namespace:         "ns",
				CanonicalName:     "name",
				CanonicalRevision: "latest",
				Network:           "testnetwork",
				WorkloadType:      workloadapi.WorkloadType_POD,
				WorkloadName:      "name",
				Status:            workloadapi.WorkloadStatus_HEALTHY,
				ClusterId:         testC,
				ApplicationTunnel: &workloadapi.ApplicationTunnel{
					Protocol: workloadapi.ApplicationTunnel_PROXY,
					Port:     15088,
				},
			},
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			mock := krttest.NewMock(t, tt.inputs)
			a := newAmbientUnitTest()
			WorkloadServices := krttest.GetMockCollection[model.ServiceInfo](mock)
			WorkloadServicesNamespaceIndex := krt.NewNamespaceIndex(WorkloadServices)
			builder := a.workloadEntryWorkloadBuilder(
				GetMeshConfig(mock),
				krttest.GetMockCollection[model.WorkloadAuthorization](mock),
				krttest.GetMockCollection[*securityclient.PeerAuthentication](mock),
				krttest.GetMockCollection[Waypoint](mock),
				WorkloadServices,
				WorkloadServicesNamespaceIndex,
				krttest.GetMockCollection[*v1.Namespace](mock),
			)
			wrapper := builder(krt.TestingDummyContext{}, tt.we)
			var res *workloadapi.Workload
			if wrapper != nil {
				res = wrapper.Workload
			}
			assert.Equal(t, res, tt.result)
		})
	}
}

func TestServiceEntryWorkloads(t *testing.T) {
	cases := []struct {
		name   string
		inputs []any
		se     *networkingclient.ServiceEntry
		result []*workloadapi.Workload
	}{
		{
			name:   "dns without endpoints",
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
					Resolution: networking.ServiceEntry_DNS,
				},
			},
			result: []*workloadapi.Workload{
				{
					Uid:               "cluster0/networking.istio.io/ServiceEntry/ns/name/a.example.com",
					Name:              "name",
					Namespace:         "ns",
					Hostname:          "a.example.com",
					Network:           testNW,
					CanonicalName:     "name",
					CanonicalRevision: "latest",
					WorkloadType:      workloadapi.WorkloadType_POD,
					WorkloadName:      "name",
					Status:            workloadapi.WorkloadStatus_HEALTHY,
					ClusterId:         testC,
					Services: map[string]*workloadapi.PortList{
						"ns/a.example.com": {
							Ports: []*workloadapi.Port{{
								ServicePort: 80,
								TargetPort:  80,
							}},
						},
					},
				},
				{
					Uid:               "cluster0/networking.istio.io/ServiceEntry/ns/name/b.example.com",
					Name:              "name",
					Namespace:         "ns",
					Hostname:          "b.example.com",
					Network:           testNW,
					CanonicalName:     "name",
					CanonicalRevision: "latest",
					WorkloadType:      workloadapi.WorkloadType_POD,
					WorkloadName:      "name",
					Status:            workloadapi.WorkloadStatus_HEALTHY,
					ClusterId:         testC,
					Services: map[string]*workloadapi.PortList{
						"ns/b.example.com": {
							Ports: []*workloadapi.Port{{
								ServicePort: 80,
								TargetPort:  80,
							}},
						},
					},
				},
			},
		},
		{
			name: "static",
			inputs: []any{
				Waypoint{
					Named: krt.Named{Name: "waypoint", Namespace: "ns"},
					Address: &workloadapi.GatewayAddress{
						Destination: &workloadapi.GatewayAddress_Hostname{
							Hostname: &workloadapi.NamespacedHostname{
								Namespace: "ns",
								Hostname:  "waypoint.example.com",
							},
						},
					},
					TrafficType: constants.AllTraffic,
				},
			},
			se: &networkingclient.ServiceEntry{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "name",
					Namespace: "ns",
				},
				Spec: networking.ServiceEntry{
					Addresses: []string{"1.2.3.4"},
					Hosts:     []string{"a.example.com"},
					Ports: []*networking.ServicePort{{
						Number: 80,
						Name:   "http",
					}},
					Resolution: networking.ServiceEntry_STATIC,
					Endpoints: []*networking.WorkloadEntry{
						// One is bound to waypoint, other is not
						{Address: "2.3.4.5"},
						{Address: "3.4.5.6", Labels: map[string]string{label.IoIstioUseWaypoint.Name: "waypoint"}},
					},
				},
			},
			result: []*workloadapi.Workload{
				{
					Uid:               "cluster0/networking.istio.io/ServiceEntry/ns/name/2.3.4.5",
					Name:              "name",
					Namespace:         "ns",
					Addresses:         [][]byte{netip.MustParseAddr("2.3.4.5").AsSlice()},
					Network:           testNW,
					CanonicalName:     "name",
					CanonicalRevision: "latest",
					WorkloadType:      workloadapi.WorkloadType_POD,
					WorkloadName:      "name",
					Status:            workloadapi.WorkloadStatus_HEALTHY,
					ClusterId:         testC,
					Services: map[string]*workloadapi.PortList{
						"ns/a.example.com": {
							Ports: []*workloadapi.Port{{
								ServicePort: 80,
								TargetPort:  80,
							}},
						},
					},
				},
				{
					Uid:               "cluster0/networking.istio.io/ServiceEntry/ns/name/3.4.5.6",
					Name:              "name",
					Namespace:         "ns",
					Addresses:         [][]byte{netip.MustParseAddr("3.4.5.6").AsSlice()},
					Network:           testNW,
					CanonicalName:     "name",
					CanonicalRevision: "latest",
					WorkloadType:      workloadapi.WorkloadType_POD,
					WorkloadName:      "name",
					Status:            workloadapi.WorkloadStatus_HEALTHY,
					ClusterId:         testC,
					Waypoint: &workloadapi.GatewayAddress{
						Destination: &workloadapi.GatewayAddress_Hostname{
							Hostname: &workloadapi.NamespacedHostname{
								Namespace: "ns",
								Hostname:  "waypoint.example.com",
							},
						},
					},
					Services: map[string]*workloadapi.PortList{
						"ns/a.example.com": {
							Ports: []*workloadapi.Port{{
								ServicePort: 80,
								TargetPort:  80,
							}},
						},
					},
				},
			},
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			mock := krttest.NewMock(t, tt.inputs)
			a := newAmbientUnitTest()
			builder := a.serviceEntryWorkloadBuilder(
				GetMeshConfig(mock),
				krttest.GetMockCollection[model.WorkloadAuthorization](mock),
				krttest.GetMockCollection[*securityclient.PeerAuthentication](mock),
				krttest.GetMockCollection[Waypoint](mock),
				krttest.GetMockCollection[*v1.Namespace](mock),
			)
			res := builder(krt.TestingDummyContext{}, tt.se)
			wl := slices.Map(res, func(e model.WorkloadInfo) *workloadapi.Workload {
				return e.Workload
			})
			assert.Equal(t, wl, tt.result)
		})
	}
}

func TestEndpointSliceWorkloads(t *testing.T) {
	cases := []struct {
		name   string
		inputs []any
		slice  *discovery.EndpointSlice
		result []*workloadapi.Workload
	}{
		{
			name: "api server",
			inputs: []any{
				kubernetesAPIServerService("1.2.3.4"),
			},
			slice: kubernetesAPIServerEndpoint("172.18.0.5"),
			result: []*workloadapi.Workload{{
				Uid:         "cluster0/discovery.k8s.io/EndpointSlice/default/kubernetes/172.18.0.5",
				Name:        "kubernetes",
				Namespace:   "default",
				Addresses:   [][]byte{netip.MustParseAddr("172.18.0.5").AsSlice()},
				Network:     testNW,
				Status:      workloadapi.WorkloadStatus_HEALTHY,
				NetworkMode: workloadapi.NetworkMode_HOST_NETWORK,
				ClusterId:   testC,
				Services: map[string]*workloadapi.PortList{
					"default/kubernetes.default.svc.domain.suffix": {
						Ports: []*workloadapi.Port{{
							ServicePort: 443,
							TargetPort:  6443,
						}},
					},
				},
			}},
		},
		{
			name: "pod as part of selectorless service without targetRef",
			inputs: []any{
				model.ServiceInfo{
					Service: &workloadapi.Service{
						Name:      "svc",
						Namespace: "default",
						Hostname:  "svc.default.svc.domain.suffix",
						Ports: []*workloadapi.Port{
							{
								ServicePort: 80,
								TargetPort:  80,
							},
						},
					},
					PortNames: map[int32]model.ServicePortName{
						80: {PortName: "http"},
					},
					// no selector!
					LabelSelector: model.LabelSelector{},
					Source:        model.TypedObject{Kind: kind.Service},
				},
			},
			// EndpointSlice manually created with the IP of the pod, but does NOT have a targetRef
			slice: &discovery.EndpointSlice{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "svc-123",
					Namespace: "default",
					Labels: map[string]string{
						discovery.LabelServiceName: "svc",
					},
				},
				AddressType: discovery.AddressTypeIPv4,
				Endpoints: []discovery.Endpoint{
					{
						Addresses: []string{"1.2.3.4"},
						Conditions: discovery.EndpointConditions{
							Ready: ptr.Of(true),
						},
					},
				},
				Ports: []discovery.EndpointPort{
					{
						Name:     ptr.Of("http"),
						Protocol: ptr.Of(v1.ProtocolTCP),
						Port:     ptr.Of(int32(80)),
					},
				},
			},
			result: []*workloadapi.Workload{{
				Uid:         "cluster0/discovery.k8s.io/EndpointSlice/default/svc-123/1.2.3.4",
				Name:        "svc-123",
				Namespace:   "default",
				Addresses:   [][]byte{netip.AddrFrom4([4]byte{1, 2, 3, 4}).AsSlice()},
				Network:     testNW,
				Status:      workloadapi.WorkloadStatus_HEALTHY,
				NetworkMode: workloadapi.NetworkMode_HOST_NETWORK,
				ClusterId:   testC,
				Services: map[string]*workloadapi.PortList{
					"default/svc.default.svc.domain.suffix": {
						Ports: []*workloadapi.Port{{
							ServicePort: 80,
							TargetPort:  80,
						}},
					},
				},
			}},
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			mock := krttest.NewMock(t, tt.inputs)
			a := newAmbientUnitTest()
			WorkloadServices := krttest.GetMockCollection[model.ServiceInfo](mock)
			builder := a.endpointSlicesBuilder(
				GetMeshConfig(mock),
				WorkloadServices,
			)
			res := builder(krt.TestingDummyContext{}, tt.slice)
			wl := slices.Map(res, func(e model.WorkloadInfo) *workloadapi.Workload {
				return e.Workload
			})
			assert.Equal(t, wl, tt.result)
		})
	}
}

func kubernetesAPIServerService(ip string) model.ServiceInfo {
	return model.ServiceInfo{
		Service: &workloadapi.Service{
			Name:      "kubernetes",
			Namespace: "default",
			Hostname:  "kubernetes.default.svc.domain.suffix",
			Addresses: []*workloadapi.NetworkAddress{{
				Network: testNW,
				Address: netip.MustParseAddr(ip).AsSlice(),
			}},
			Ports: []*workloadapi.Port{
				{
					ServicePort: 443,
					TargetPort:  6443,
				},
			},
		},
		PortNames: map[int32]model.ServicePortName{
			443: {PortName: "https"},
		},
		Source: model.TypedObject{Kind: kind.Service},
	}
}

func kubernetesAPIServerEndpoint(ip string) *discovery.EndpointSlice {
	return &discovery.EndpointSlice{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "kubernetes",
			Namespace: "default",
			Labels: map[string]string{
				discovery.LabelServiceName: "kubernetes",
			},
		},
		AddressType: discovery.AddressTypeIPv4,
		Endpoints: []discovery.Endpoint{
			{
				Addresses: []string{ip},
				Conditions: discovery.EndpointConditions{
					Ready: ptr.Of(true),
				},
			},
		},
		Ports: []discovery.EndpointPort{
			{
				Name:     ptr.Of("https"),
				Protocol: ptr.Of(v1.ProtocolTCP),
				Port:     ptr.Of(int32(6443)),
			},
		},
	}
}

func newAmbientUnitTest() *index {
	return &index{
		networkUpdateTrigger: krt.NewRecomputeTrigger(true),
		ClusterID:            testC,
		DomainSuffix:         "domain.suffix",
		Network: func(endpointIP string, labels labels.Instance) network.ID {
			return testNW
		},
		Flags: FeatureFlags{
			DefaultAllowFromWaypoint:              features.DefaultAllowFromWaypoint,
			EnableK8SServiceSelectWorkloadEntries: features.EnableK8SServiceSelectWorkloadEntries,
		},
		LookupNetworkGateways: func() []model.NetworkGateway {
			return []model.NetworkGateway{
				{
					Network:   "remote-network",
					Addr:      "9.9.9.9",
					Cluster:   "cluster-a",
					Port:      15008,
					HBONEPort: 15008,
					ServiceAccount: types.NamespacedName{
						Namespace: "ns-gtw",
						Name:      "sa-gtw",
					},
				},
				{
					Network:   "remote-network-hostname",
					Addr:      "networkgateway.example.com",
					Cluster:   "cluster-a",
					Port:      15008,
					HBONEPort: 15008,
					ServiceAccount: types.NamespacedName{
						Namespace: "ns-gtw",
						Name:      "sa-gtw",
					},
				},
			}
		},
	}
}

var podReady = []v1.PodCondition{
	{
		Type:               v1.PodReady,
		Status:             v1.ConditionTrue,
		LastTransitionTime: metav1.Now(),
	},
}

// Special case to handle wrapper type. Can this be generic?
func GetMeshConfig(mc *krttest.MockCollection) krt.StaticSingleton[MeshConfig] {
	attempt := krttest.GetMockSingleton[MeshConfig](mc)
	if attempt.Get() == nil {
		return krt.NewStatic(&MeshConfig{mesh.DefaultMeshConfig()}, true)
	}
	return attempt
}
