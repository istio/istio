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

package controller

import (
	"reflect"
	"testing"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/config/labels"
)

func TestHasProxyIP(t *testing.T) {
	tests := []struct {
		name      string
		addresses []v1.EndpointAddress
		proxyIP   string
		expected  bool
	}{
		{
			"has proxy ip",
			[]v1.EndpointAddress{{IP: "172.17.0.1"}, {IP: "172.17.0.2"}},
			"172.17.0.1",
			true,
		},
		{
			"has no proxy ip",
			[]v1.EndpointAddress{{IP: "172.17.0.1"}, {IP: "172.17.0.2"}},
			"172.17.0.100",
			false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := hasProxyIP(test.addresses, test.proxyIP)
			if test.expected != got {
				t.Errorf("Expected %v, but got %v", test.expected, got)
			}
		})
	}
}

func TestGetLabelValue(t *testing.T) {
	tests := []struct {
		name               string
		node               *v1.Node
		expectedLabelValue string
	}{
		{
			"Chooses beta label",
			&v1.Node{ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{NodeRegionLabel: "beta-region", NodeRegionLabelGA: "ga-region"}}},
			"beta-region",
		},
		{
			"Fallback no beta label defined",
			&v1.Node{ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{NodeRegionLabelGA: "ga-region"}}},
			"ga-region",
		},
		{
			"Only beta label specified",
			&v1.Node{ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{NodeRegionLabel: "beta-region"}}},
			"beta-region",
		},
		{
			"No label defined at all",
			&v1.Node{},
			"",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := getLabelValue(test.node.ObjectMeta, NodeRegionLabel, NodeRegionLabelGA)
			if test.expectedLabelValue != got {
				t.Errorf("Expected %v, but got %v", test.expectedLabelValue, got)
			}
		})
	}
}

func TestPodKeyByProxy(t *testing.T) {
	testCases := []struct {
		name        string
		proxy       *model.Proxy
		expectedKey types.NamespacedName
	}{
		{
			name: "invalid id: bad format",
			proxy: &model.Proxy{
				ID: "invalid",
				Metadata: &model.NodeMetadata{
					Namespace: "default",
				},
			},
		},
		{
			name: "invalid id: namespace mismatch",
			proxy: &model.Proxy{
				ID: "pod1.ns1",
				Metadata: &model.NodeMetadata{
					Namespace: "default",
				},
			},
		},
		{
			name: "invalid id: namespace mismatch",
			proxy: &model.Proxy{
				ID: "pod1.ns1",
				Metadata: &model.NodeMetadata{
					Namespace: "ns1",
				},
			},
			expectedKey: types.NamespacedName{Namespace: "ns1", Name: "pod1"},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			key := podKeyByProxy(tc.proxy)
			if key != tc.expectedKey {
				t.Errorf("expected key %s != %s", tc.expectedKey, key)
			}
		})
	}
}

func TestGetNodeSelectorsForService(t *testing.T) {
	testCases := []struct {
		name                  string
		svc                   *v1.Service
		expectedLabelSelector labels.Instance
	}{
		{
			name:                  "empty selector",
			svc:                   makeFakeSvc(""),
			expectedLabelSelector: nil,
		},
		{
			name:                  "invalid selector",
			svc:                   makeFakeSvc("invalid value"),
			expectedLabelSelector: nil,
		},
		{
			name:                  "wildcard match",
			svc:                   makeFakeSvc("{}"),
			expectedLabelSelector: labels.Instance{},
		},
		{
			name:                  "specific match",
			svc:                   makeFakeSvc(`{"kubernetes.io/hostname": "node1"}`),
			expectedLabelSelector: labels.Instance{"kubernetes.io/hostname": "node1"},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			selector := getNodeSelectorsForService(tc.svc)
			if !reflect.DeepEqual(selector, tc.expectedLabelSelector) {
				t.Errorf("expected selector %v != %v", tc.expectedLabelSelector, selector)
			}
		})
	}
}

func makeFakeSvc(nodeSelector string) *v1.Service {
	svc := &v1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "service",
			Namespace: "ns",
		},
		Spec: v1.ServiceSpec{
			Ports: []v1.ServicePort{{
				Name: "http",
				Port: 80,
			}},
			Selector:  map[string]string{"app": "helloworld"},
			ClusterIP: "9.9.9.9",
		},
	}

	if nodeSelector != "" {
		svc.Annotations = map[string]string{
			"traffic.istio.io/nodeSelector": nodeSelector,
		}
	}
	return svc
}

func getDataPointer[E comparable](d E) *E {
	p := new(E)
	*p = d
	return p
}

func TestServiceEqual(t *testing.T) {
	type args struct {
		first  *v1.Service
		second *v1.Service
	}
	tests := []struct {
		name string
		args args
		want bool
	}{
		{
			name: "Test two identical services",
			args: args{
				first: &v1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Annotations: map[string]string{
							"app":     "nginx",
							"version": "1.0",
						},
						Labels: map[string]string{
							"app":     "nginx",
							"version": "1.0",
						},
						Name:      "nginx",
						Namespace: "default",
					},
					Spec: v1.ServiceSpec{
						Selector: map[string]string{
							"app": "nginx",
						},
						Ports: []v1.ServicePort{
							{
								Name: "http",
								Port: 80,
							},
						},
						Type:       v1.ServiceTypeClusterIP,
						ClusterIP:  "10.0.0.1",
						ClusterIPs: []string{"10.0.0.1", "10.0.0.2"},
						ExternalIPs: []string{
							"192.168.0.1",
							"192.168.0.2",
						},
						LoadBalancerIP: "192.168.0.3",
						LoadBalancerSourceRanges: []string{
							"10.0.0.0/16",
							"192.168.0.0/24",
						},
						ExternalName:    "example.com",
						SessionAffinity: v1.ServiceAffinityClientIP,
						SessionAffinityConfig: &v1.SessionAffinityConfig{
							ClientIP: &v1.ClientIPConfig{
								TimeoutSeconds: getDataPointer[int32](10),
							},
						},
						PublishNotReadyAddresses:      true,
						IPFamilies:                    []v1.IPFamily{v1.IPv6Protocol, v1.IPv4Protocol},
						IPFamilyPolicy:                getDataPointer[v1.IPFamilyPolicy](v1.IPFamilyPolicySingleStack),
						ExternalTrafficPolicy:         v1.ServiceExternalTrafficPolicyCluster,
						HealthCheckNodePort:           100,
						AllocateLoadBalancerNodePorts: getDataPointer[bool](true),
						LoadBalancerClass:             getDataPointer[string]("test"),
						InternalTrafficPolicy:         getDataPointer[v1.ServiceInternalTrafficPolicy](v1.ServiceInternalTrafficPolicyCluster),
					},
					Status: v1.ServiceStatus{
						LoadBalancer: v1.LoadBalancerStatus{
							Ingress: []v1.LoadBalancerIngress{
								{
									IP:       "192.168.0.1",
									Hostname: "example.com",
									Ports: []v1.PortStatus{
										{
											Protocol: "http",
											Port:     80,
											Error:    getDataPointer[string]("err"),
										},
									},
								},
							},
						},
					},
				},
				second: &v1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Annotations: map[string]string{
							"app":     "nginx",
							"version": "1.0",
						},
						Labels: map[string]string{
							"app":     "nginx",
							"version": "1.0",
						},
						Name:      "nginx",
						Namespace: "default",
					},
					Spec: v1.ServiceSpec{
						Selector: map[string]string{
							"app": "nginx",
						},
						Ports: []v1.ServicePort{
							{
								Name: "http",
								Port: 80,
							},
						},
						Type:       v1.ServiceTypeClusterIP,
						ClusterIP:  "10.0.0.1",
						ClusterIPs: []string{"10.0.0.1", "10.0.0.2"},
						ExternalIPs: []string{
							"192.168.0.1",
							"192.168.0.2",
						},
						LoadBalancerIP: "192.168.0.3",
						LoadBalancerSourceRanges: []string{
							"10.0.0.0/16",
							"192.168.0.0/24",
						},
						ExternalName:    "example.com",
						SessionAffinity: v1.ServiceAffinityClientIP,
						SessionAffinityConfig: &v1.SessionAffinityConfig{
							ClientIP: &v1.ClientIPConfig{
								TimeoutSeconds: getDataPointer[int32](10),
							},
						},
						PublishNotReadyAddresses:      true,
						IPFamilies:                    []v1.IPFamily{v1.IPv6Protocol, v1.IPv4Protocol},
						IPFamilyPolicy:                getDataPointer[v1.IPFamilyPolicy](v1.IPFamilyPolicySingleStack),
						ExternalTrafficPolicy:         v1.ServiceExternalTrafficPolicyCluster,
						HealthCheckNodePort:           100,
						AllocateLoadBalancerNodePorts: getDataPointer[bool](true),
						LoadBalancerClass:             getDataPointer[string]("test"),
						InternalTrafficPolicy:         getDataPointer[v1.ServiceInternalTrafficPolicy](v1.ServiceInternalTrafficPolicyCluster),
					},
					Status: v1.ServiceStatus{
						LoadBalancer: v1.LoadBalancerStatus{
							Ingress: []v1.LoadBalancerIngress{
								{
									IP:       "192.168.0.1",
									Hostname: "example.com",
									Ports: []v1.PortStatus{
										{
											Protocol: "http",
											Port:     80,
											Error:    getDataPointer[string]("err"),
										},
									},
								},
							},
						},
					},
				},
			},
			want: true,
		},
		{
			name: "Test two services with different Names",
			args: args{
				first: &v1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name: "nginx",
					},
				},
				second: &v1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name: "apache",
					},
				},
			},
			want: false,
		},
		{
			name: "Test two services with different Namespaces",
			args: args{
				first: &v1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: "default",
					},
				},
				second: &v1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: "kube-system",
					},
				},
			},
			want: false,
		},
		{
			name: "Test two services with different Annotations",
			args: args{
				first: &v1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Annotations: map[string]string{
							"app":     "nginx",
							"version": "1.0",
						},
					},
				},
				second: &v1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Annotations: map[string]string{
							"app":     "apache",
							"version": "2.0",
						},
					},
				},
			},
			want: true,
		},
		{
			name: "Test two services with different Labels",
			args: args{
				first: &v1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Labels: map[string]string{
							"app":     "nginx",
							"version": "1.0",
						},
					},
				},
				second: &v1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Labels: map[string]string{
							"app":     "apache",
							"version": "2.0",
						},
					},
				},
			},
			want: false,
		},
		{
			name: "Test two services with different selectors",
			args: args{
				first: &v1.Service{
					Spec: v1.ServiceSpec{
						Selector: map[string]string{
							"app": "nginx",
						},
					},
				},
				second: &v1.Service{
					Spec: v1.ServiceSpec{
						Selector: map[string]string{
							"app": "apache",
						},
					},
				},
			},
			want: false,
		},
		{
			name: "Test two services with different ports",
			args: args{
				first: &v1.Service{
					Spec: v1.ServiceSpec{
						Ports: []v1.ServicePort{
							{
								Name: "http",
								Port: 80,
							},
						},
					},
				},
				second: &v1.Service{
					Spec: v1.ServiceSpec{
						Ports: []v1.ServicePort{
							{
								Name: "https",
								Port: 443,
							},
						},
					},
				},
			},
			want: false,
		},
		{
			name: "Test two services with different types",
			args: args{
				first: &v1.Service{
					Spec: v1.ServiceSpec{
						Type: v1.ServiceTypeClusterIP,
					},
				},
				second: &v1.Service{
					Spec: v1.ServiceSpec{
						Type: v1.ServiceTypeNodePort,
					},
				},
			},
			want: false,
		},
		{
			name: "Test two services with different ClusterIP",
			args: args{
				first: &v1.Service{
					Spec: v1.ServiceSpec{
						ClusterIP: "10.0.0.1",
					},
				},
				second: &v1.Service{
					Spec: v1.ServiceSpec{
						ClusterIP: "10.0.0.2",
					},
				},
			},
			want: false,
		},
		{
			name: "Test two services with different ClusterIPs",
			args: args{
				first: &v1.Service{
					Spec: v1.ServiceSpec{
						ClusterIPs: []string{"10.0.0.1", "10.0.0.2"},
					},
				},
				second: &v1.Service{
					Spec: v1.ServiceSpec{
						ClusterIPs: []string{"10.0.0.2", "10.0.0.3"},
					},
				},
			},
			want: false,
		},
		{
			name: "Test two services with different ExternalIPs",
			args: args{
				first: &v1.Service{
					Spec: v1.ServiceSpec{
						ExternalIPs: []string{
							"192.168.0.1",
							"192.168.0.2",
						},
					},
				},
				second: &v1.Service{
					Spec: v1.ServiceSpec{
						ExternalIPs: []string{
							"192.168.0.3",
							"192.168.0.4",
						},
					},
				},
			},
			want: false,
		},
		{
			name: "Test two services with different AllocateLoadBalancerNodePorts",
			args: args{
				first: &v1.Service{
					Spec: v1.ServiceSpec{
						AllocateLoadBalancerNodePorts: getDataPointer[bool](true),
					},
				},
				second: &v1.Service{
					Spec: v1.ServiceSpec{
						AllocateLoadBalancerNodePorts: getDataPointer[bool](false),
					},
				},
			},
			want: false,
		},
		{
			name: "Test two services with different LoadBalancerIP",
			args: args{
				first: &v1.Service{
					Spec: v1.ServiceSpec{
						LoadBalancerIP: "192.168.0.1",
					},
				},
				second: &v1.Service{
					Spec: v1.ServiceSpec{
						LoadBalancerIP: "192.168.0.2",
					},
				},
			},
			want: false,
		},
		{
			name: "Test two services with different LoadBalancerSourceRanges",
			args: args{
				first: &v1.Service{
					Spec: v1.ServiceSpec{
						LoadBalancerSourceRanges: []string{
							"10.0.0.0/16",
							"192.168.0.0/24",
						},
					},
				},
				second: &v1.Service{
					Spec: v1.ServiceSpec{
						LoadBalancerSourceRanges: []string{
							"10.0.0.0/8",
							"192.168.0.0/16",
						},
					},
				},
			},
			want: false,
		},
		{
			name: "Test two services with different ExternalNames",
			args: args{
				first: &v1.Service{
					Spec: v1.ServiceSpec{
						ExternalName: "example.com",
					},
				},
				second: &v1.Service{
					Spec: v1.ServiceSpec{
						ExternalName: "example.org",
					},
				},
			},
			want: false,
		},
		{
			name: "Test two services with different ExternalTrafficPolicy",
			args: args{
				first: &v1.Service{
					Spec: v1.ServiceSpec{
						ExternalTrafficPolicy: v1.ServiceExternalTrafficPolicyCluster,
					},
				},
				second: &v1.Service{
					Spec: v1.ServiceSpec{
						ExternalTrafficPolicy: v1.ServiceExternalTrafficPolicyLocal,
					},
				},
			},
			want: false,
		},
		{
			name: "Test two services with different HealthCheckNodePort",
			args: args{
				first: &v1.Service{
					Spec: v1.ServiceSpec{
						HealthCheckNodePort: 10000,
					},
				},
				second: &v1.Service{
					Spec: v1.ServiceSpec{
						HealthCheckNodePort: 20000,
					},
				},
			},
			want: false,
		},
		{
			name: "Test two services with different SessionAffinity",
			args: args{
				first: &v1.Service{
					Spec: v1.ServiceSpec{
						SessionAffinity: v1.ServiceAffinityClientIP,
					},
				},
				second: &v1.Service{
					Spec: v1.ServiceSpec{
						SessionAffinity: v1.ServiceAffinityNone,
					},
				},
			},
			want: false,
		},
		{
			name: "Test two services with different SessionAffinityConfig",
			args: args{
				first: &v1.Service{
					Spec: v1.ServiceSpec{
						SessionAffinityConfig: &v1.SessionAffinityConfig{
							ClientIP: &v1.ClientIPConfig{
								TimeoutSeconds: getDataPointer[int32](10),
							},
						},
					},
				},
				second: &v1.Service{
					Spec: v1.ServiceSpec{
						SessionAffinityConfig: &v1.SessionAffinityConfig{
							ClientIP: &v1.ClientIPConfig{
								TimeoutSeconds: getDataPointer[int32](20),
							},
						},
					},
				},
			},
			want: false,
		},
		{
			name: "Test two services with different PublishNotReadyAddresses",
			args: args{
				first: &v1.Service{
					Spec: v1.ServiceSpec{
						PublishNotReadyAddresses: true,
					},
				},
				second: &v1.Service{
					Spec: v1.ServiceSpec{
						PublishNotReadyAddresses: false,
					},
				},
			},
			want: false,
		},
		{
			name: "Test two services with different IPFamilies",
			args: args{
				first: &v1.Service{
					Spec: v1.ServiceSpec{
						IPFamilies: []v1.IPFamily{v1.IPv6Protocol},
					},
				},
				second: &v1.Service{
					Spec: v1.ServiceSpec{
						IPFamilies: []v1.IPFamily{v1.IPv4Protocol},
					},
				},
			},
			want: false,
		},
		{
			name: "Test two services with different IPFamilyPolicy",
			args: args{
				first: &v1.Service{
					Spec: v1.ServiceSpec{
						IPFamilyPolicy: getDataPointer[v1.IPFamilyPolicy](v1.IPFamilyPolicySingleStack),
					},
				},
				second: &v1.Service{
					Spec: v1.ServiceSpec{
						IPFamilyPolicy: getDataPointer[v1.IPFamilyPolicy](v1.IPFamilyPolicyPreferDualStack),
					},
				},
			},
			want: false,
		},
		{
			name: "Test two services with different LoadBalancer",
			args: args{
				first: &v1.Service{
					Status: v1.ServiceStatus{
						LoadBalancer: v1.LoadBalancerStatus{
							Ingress: []v1.LoadBalancerIngress{
								{
									IP:       "192.168.0.1",
									Hostname: "example1.com",
									Ports: []v1.PortStatus{{
										Port:     8080,
										Protocol: "http",
										Error:    getDataPointer[string]("err1"),
									}},
								},
							},
						},
					},
				},
				second: &v1.Service{
					Status: v1.ServiceStatus{
						LoadBalancer: v1.LoadBalancerStatus{
							Ingress: []v1.LoadBalancerIngress{
								{
									IP:       "192.168.0.1",
									Hostname: "example1.com",
									Ports: []v1.PortStatus{{
										Port:     8080,
										Protocol: "http",
										Error:    getDataPointer[string]("err2"),
									}},
								},
							},
						},
					},
				},
			},
			want: false,
		},
		{
			name: "Test two services with different Conditions",
			args: args{
				first: &v1.Service{
					Status: v1.ServiceStatus{
						Conditions: []metav1.Condition{
							{
								Type: "type1",
							},
						},
					},
				},
				second: &v1.Service{
					Status: v1.ServiceStatus{
						Conditions: []metav1.Condition{
							{
								Type: "type2",
							},
						},
					},
				},
			},
			want: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := serviceEqual(tt.args.first, tt.args.second); got != tt.want {
				t.Errorf("EqualServices() = %v, want %v", got, tt.want)
			}
		})
	}
}
