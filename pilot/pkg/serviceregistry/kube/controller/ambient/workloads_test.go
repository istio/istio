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
	"cmp"
	"net/netip"
	"testing"

	v1 "k8s.io/api/core/v1"
	discovery "k8s.io/api/discovery/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	"istio.io/api/annotation"
	"istio.io/api/label"
	networking "istio.io/api/networking/v1alpha3"
	networkingclient "istio.io/client-go/pkg/apis/networking/v1"
	securityclient "istio.io/client-go/pkg/apis/security/v1"
	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/config/mesh"
	"istio.io/istio/pkg/config/schema/kind"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/kube/krt"
	"istio.io/istio/pkg/kube/krt/krttest"
	"istio.io/istio/pkg/ptr"
	"istio.io/istio/pkg/slices"
	"istio.io/istio/pkg/test"
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
			name:   "pod from replicaset",
			inputs: []any{},
			pod: &v1.Pod{
				TypeMeta: metav1.TypeMeta{},
				ObjectMeta: metav1.ObjectMeta{
					Name:         "rs-xvnqd",
					Namespace:    "ns",
					GenerateName: "rs-",
					OwnerReferences: []metav1.OwnerReference{
						{
							Kind:       "ReplicaSet",
							APIVersion: "apps/v1",
							Name:       "rs",
							Controller: ptr.Of(true),
						},
					},
				},
				Spec: v1.PodSpec{},
				Status: v1.PodStatus{
					Phase: v1.PodRunning,
					PodIP: "1.2.3.4",
				},
			},
			result: &workloadapi.Workload{
				Uid:               "cluster0//Pod/ns/rs-xvnqd",
				Name:              "rs-xvnqd",
				Namespace:         "ns",
				Addresses:         [][]byte{netip.AddrFrom4([4]byte{1, 2, 3, 4}).AsSlice()},
				Network:           testNW,
				CanonicalName:     "rs",
				CanonicalRevision: "latest",
				WorkloadType:      workloadapi.WorkloadType_POD,
				WorkloadName:      "rs",
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
			name: "pod selected by ServiceEntry honors port name mapping",
			inputs: []any{
				model.ServiceInfo{
					Service: &workloadapi.Service{
						Name:      "httpbin",
						Namespace: "httpbin2",
						Hostname:  "httpbin.httpbin2.mesh.internal",
						Ports: []*workloadapi.Port{{
							ServicePort: 8002,
							TargetPort:  0,
						}},
					},
					PortNames: map[int32]model.ServicePortName{
						8002: {PortName: "http"},
					},
					LabelSelector: model.NewSelector(map[string]string{"app": "httpbin"}),
					Source:        model.TypedObject{Kind: kind.ServiceEntry},
				},
			},
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "httpbin-123",
					Namespace: "httpbin2",
					Labels:    map[string]string{"app": "httpbin"},
				},
				Spec: v1.PodSpec{
					Containers: []v1.Container{{Ports: []v1.ContainerPort{{
						Name:          "http",
						ContainerPort: 8000,
						Protocol:      v1.ProtocolTCP,
					}}}},
				},
				Status: v1.PodStatus{Phase: v1.PodRunning, Conditions: podReady, PodIP: "10.1.1.1"},
			},
			result: &workloadapi.Workload{
				Uid:               "cluster0//Pod/httpbin2/httpbin-123",
				Name:              "httpbin-123",
				Namespace:         "httpbin2",
				Addresses:         [][]byte{netip.MustParseAddr("10.1.1.1").AsSlice()},
				Network:           testNW,
				CanonicalName:     "httpbin",
				CanonicalRevision: "latest",
				WorkloadType:      workloadapi.WorkloadType_POD,
				WorkloadName:      "httpbin-123",
				Status:            workloadapi.WorkloadStatus_HEALTHY,
				ClusterId:         testC,
				Services: map[string]*workloadapi.PortList{
					"httpbin2/httpbin.httpbin2.mesh.internal": {
						Ports: []*workloadapi.Port{{ServicePort: 8002, TargetPort: 8000}},
					},
				},
			},
		},
		{
			name: "simple pod with locality",
			inputs: []any{
				Node{
					Name: "node",
					Locality: &workloadapi.Locality{
						Region: "region",
						Zone:   "zone",
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
			a := newAmbientUnitTest(t)
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
				krttest.GetMockCollection[Node](mock),
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
								TargetPort:  8081,
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
						81: {PortName: "81"},
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
								ServicePort: 81,
								TargetPort:  8180,
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
			name: "host pod with ServiceEntry workloadSelector",
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
					Address: "hostname",
				},
			},
			result: &workloadapi.Workload{
				Uid:               "cluster0/networking.istio.io/WorkloadEntry/ns/name",
				Name:              "name",
				Namespace:         "ns",
				Hostname:          "hostname",
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
			a := newAmbientUnitTest(t)
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
			name: "dns without endpoints",
			// This is kind of ugly, but we need to add the ServiceInfos to the inputs so that the ServiceEntryWorkloadBuilder can find them
			// Otherwise, we consider the ServiceEntry to have been deduplicated and we won't generate workloads for it
			inputs: []any{
				model.ServiceInfo{
					Service: &workloadapi.Service{
						Name:      "name",
						Namespace: "ns",
						Hostname:  "a.example.com",
						Ports: []*workloadapi.Port{{
							ServicePort: 80,
							TargetPort:  80,
						}},
					},
					PortNames: map[int32]model.ServicePortName{
						80: {PortName: "http"},
					},
					Source: model.TypedObject{
						Kind: kind.ServiceEntry,
						NamespacedName: types.NamespacedName{
							Namespace: "ns",
							Name:      "name",
						},
					},
				},
				model.ServiceInfo{
					Service: &workloadapi.Service{
						Name:      "name",
						Namespace: "ns",
						Hostname:  "b.example.com",
						Ports: []*workloadapi.Port{{
							ServicePort: 80,
							TargetPort:  80,
						}},
					},
					PortNames: map[int32]model.ServicePortName{
						80: {PortName: "http"},
					},
					Source: model.TypedObject{
						Kind: kind.ServiceEntry,
						NamespacedName: types.NamespacedName{
							Namespace: "ns",
							Name:      "name",
						},
					},
				},
			},
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
				model.ServiceInfo{
					Service: &workloadapi.Service{
						Name:      "name",
						Namespace: "ns",
						Hostname:  "a.example.com",
						Ports: []*workloadapi.Port{{
							ServicePort: 80,
							TargetPort:  80,
						}},
					},
					PortNames: map[int32]model.ServicePortName{
						80: {PortName: "http"},
					},
					Source: model.TypedObject{
						Kind: kind.ServiceEntry,
						NamespacedName: types.NamespacedName{
							Namespace: "ns",
							Name:      "name",
						},
					},
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
			a := newAmbientUnitTest(t)
			builder := a.serviceEntryWorkloadBuilder(
				GetMeshConfig(mock),
				krttest.GetMockCollection[model.WorkloadAuthorization](mock),
				krttest.GetMockCollection[*securityclient.PeerAuthentication](mock),
				krttest.GetMockCollection[Waypoint](mock),
				krttest.GetMockCollection[*v1.Namespace](mock),
				krttest.GetMockCollection[model.ServiceInfo](mock),
			)
			res := builder(krt.TestingDummyContext{}, tt.se)
			wl := slices.Map(res, func(e model.WorkloadInfo) *workloadapi.Workload {
				return e.Workload
			})
			slices.SortFunc(wl, func(a, b *workloadapi.Workload) int {
				return cmp.Compare(a.Uid, b.Uid)
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
			a := newAmbientUnitTest(t)
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

func newAmbientUnitTest(t test.Failer) *index {
	// Set up a basic network environment so tests have a default network and some gateways
	// Note: unlike other collections, networks are stored in the ambientIndex struct since they
	// are passed in almost everywhere. So we need to construct it here.
	mock := krttest.NewMock(t, []any{
		&v1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name:   systemNS,
				Labels: map[string]string{label.TopologyNetwork.Name: testNW},
			},
		},
		&gatewayv1.Gateway{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "remote-network-ip",
				Namespace: "ns-gtw",
				Annotations: map[string]string{
					annotation.GatewayServiceAccount.Name: "sa-gtw",
				},
				Labels: map[string]string{
					label.TopologyNetwork.Name: "remote-network",
				},
			},
			Spec: gatewayv1.GatewaySpec{
				GatewayClassName: "istio-remote",
				Listeners: []gatewayv1.Listener{
					{
						Name:     "cross-network",
						Port:     15008,
						Protocol: "HBONE",
					},
				},
			},
			Status: gatewayv1.GatewayStatus{
				Addresses: []gatewayv1.GatewayStatusAddress{
					{
						Type:  ptr.Of(gatewayv1.IPAddressType),
						Value: "9.9.9.9",
					},
				},
			},
		},
		&gatewayv1.Gateway{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "remote-network-hostname",
				Namespace: "ns-gtw",
				Annotations: map[string]string{
					annotation.GatewayServiceAccount.Name: "sa-gtw",
				},
				Labels: map[string]string{
					label.TopologyNetwork.Name: "remote-network-hostname",
				},
			},
			Spec: gatewayv1.GatewaySpec{
				GatewayClassName: "istio-remote",
				Listeners: []gatewayv1.Listener{
					{
						Name:     "cross-network",
						Port:     15008,
						Protocol: "HBONE",
					},
				},
			},
			Status: gatewayv1.GatewayStatus{
				Addresses: []gatewayv1.GatewayStatusAddress{
					{
						Type:  ptr.Of(gatewayv1.HostnameAddressType),
						Value: "networkgateway.example.com",
					},
				},
			},
		},
	})
	networks := buildNetworkCollections(
		krttest.GetMockCollection[*v1.Namespace](mock),
		krttest.GetMockCollection[*gatewayv1.Gateway](mock),
		Options{
			SystemNamespace: systemNS,
			ClusterID:       testC,
		}, krt.NewOptionsBuilder(test.NewStop(t), "", krt.GlobalDebugHandler))
	idx := &index{
		networks:        networks,
		SystemNamespace: systemNS,
		ClusterID:       testC,
		DomainSuffix:    "domain.suffix",
		Flags: FeatureFlags{
			DefaultAllowFromWaypoint:              features.DefaultAllowFromWaypoint,
			EnableK8SServiceSelectWorkloadEntries: features.EnableK8SServiceSelectWorkloadEntries,
		},
	}
	kube.WaitForCacheSync("test", test.NewStop(t), idx.networks.HasSynced)
	return idx
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
		return krt.NewStatic(&MeshConfig{MeshConfig: mesh.DefaultMeshConfig()}, true)
	}
	return attempt
}
