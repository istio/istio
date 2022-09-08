// Copyright Istio Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
package xds_test

import (
	"testing"

	cluster "github.com/envoyproxy/go-control-plane/envoy/config/cluster/v3"
	tls "github.com/envoyproxy/go-control-plane/envoy/extensions/transport_sockets/tls/v3"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"

	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/xds"
	v3 "istio.io/istio/pilot/pkg/xds/v3"
	"istio.io/istio/pilot/test/xdstest"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/schema/gvk"
	"istio.io/istio/pkg/test/util/assert"
	"istio.io/istio/pkg/util/sets"
)

func TestCDS(t *testing.T) {
	s := xds.NewFakeDiscoveryServer(t, xds.FakeOptions{})
	ads := s.ConnectADS().WithType(v3.ClusterType)
	ads.RequestResponseAck(t, nil)
}

func TestSAN(t *testing.T) {
	labels := map[string]string{"app": "test"}
	pod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "pod",
			Namespace: "default",
			Labels:    labels,
		},
		Spec: v1.PodSpec{ServiceAccountName: "pod"},
		Status: v1.PodStatus{
			PodIP: "1.2.3.4",
			Phase: v1.PodPending,
		},
	}
	service := &v1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "example",
			Namespace: "default",
		},
		Spec: v1.ServiceSpec{
			Ports: []v1.ServicePort{{
				Name: "http",
				Port: 80,
			}},
			Selector:  labels,
			ClusterIP: "9.9.9.9",
		},
	}
	endpoint := &v1.Endpoints{
		ObjectMeta: metav1.ObjectMeta{
			Name:      service.Name,
			Namespace: service.Namespace,
		},
		Subsets: []v1.EndpointSubset{{
			Addresses: []v1.EndpointAddress{{
				IP: pod.Status.PodIP,
				TargetRef: &v1.ObjectReference{
					Kind:      "Pod",
					Namespace: pod.Namespace,
					Name:      pod.Name,
				},
			}},
			Ports: []v1.EndpointPort{{Name: "http", Port: 80}},
		}},
	}
	dr := config.Config{
		Meta: config.Meta{
			GroupVersionKind: gvk.DestinationRule,
			Name:             "dr",
			Namespace:        "test",
		},
		Spec: &networking.DestinationRule{
			Host: "example.default.svc.cluster.local",
			TrafficPolicy: &networking.TrafficPolicy{Tls: &networking.ClientTLSSettings{
				Mode:              networking.ClientTLSSettings_MUTUAL,
				ClientCertificate: "fake",
				PrivateKey:        "fake",
				CaCertificates:    "fake",
			}},
		},
	}
	drIstioMTLS := config.Config{
		Meta: config.Meta{
			GroupVersionKind: gvk.DestinationRule,
			Name:             "dr",
			Namespace:        "test",
		},
		Spec: &networking.DestinationRule{
			Host: "example.default.svc.cluster.local",
			TrafficPolicy: &networking.TrafficPolicy{Tls: &networking.ClientTLSSettings{
				Mode:              networking.ClientTLSSettings_ISTIO_MUTUAL,
				ClientCertificate: "fake",
				PrivateKey:        "fake",
				CaCertificates:    "fake",
			}},
		},
	}
	seEDS := config.Config{
		Meta: config.Meta{
			Name:             "service-entry",
			Namespace:        "test",
			GroupVersionKind: gvk.ServiceEntry,
			Domain:           "cluster.local",
		},
		Spec: &networking.ServiceEntry{
			Hosts: []string{"example.default.svc.cluster.local"},
			Ports: []*networking.Port{{
				Number:   80,
				Protocol: "HTTP",
				Name:     "http",
			}},
			SubjectAltNames: []string{"se-top"},
			Resolution:      networking.ServiceEntry_STATIC,
			Endpoints: []*networking.WorkloadEntry{{
				Address:        "1.1.1.1",
				ServiceAccount: "se-endpoint",
			}},
		},
	}
	seNONE := config.Config{
		Meta: config.Meta{
			Name:             "service-entry",
			Namespace:        "test",
			GroupVersionKind: gvk.ServiceEntry,
			Domain:           "cluster.local",
		},
		Spec: &networking.ServiceEntry{
			Hosts: []string{"example.default.svc.cluster.local"},
			Ports: []*networking.Port{{
				Number:   80,
				Protocol: "HTTP",
				Name:     "http",
			}},
			SubjectAltNames: []string{"custom"},
			Resolution:      networking.ServiceEntry_NONE,
		},
	}
	cases := []struct {
		name    string
		objs    []runtime.Object
		configs []config.Config
		sans    []string
	}{
		{
			name:    "Kubernetes service and EDS ServiceEntry",
			objs:    []runtime.Object{service, pod, endpoint},
			configs: []config.Config{dr, seEDS},
			// The ServiceEntry rule will "win" the PushContext.ServiceAccounts.
			// However, the Service will be processed first into a cluster. Since its not external, we do not add the SANs automatically
			sans: nil,
		},
		{
			name:    "Kubernetes service and NONE ServiceEntry",
			objs:    []runtime.Object{service, pod, endpoint},
			configs: []config.Config{dr, seNONE},
			// Service properly sets SAN. Since it's not external, we do not add the SANs automatically though
			sans: nil,
		},
		{
			name:    "Kubernetes service and EDS ServiceEntry ISTIO_MUTUAL",
			objs:    []runtime.Object{service, pod, endpoint},
			configs: []config.Config{drIstioMTLS, seEDS},
			// The Service has precedence, so its cluster will be used
			sans: []string{"spiffe://cluster.local/ns/default/sa/pod"},
		},
		{
			name:    "Kubernetes service and NONE ServiceEntry ISTIO_MUTUAL",
			objs:    []runtime.Object{service, pod, endpoint},
			configs: []config.Config{drIstioMTLS, seNONE},
			// The Service has precedence, so its cluster will be used
			sans: []string{"spiffe://cluster.local/ns/default/sa/pod"},
		},
		{
			name:    "NONE ServiceEntry ISTIO_MUTUAL",
			configs: []config.Config{drIstioMTLS, seNONE},
			// Totally broken; service level ServiceAccount are ignored.
			sans: []string{"custom"},
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			s := xds.NewFakeDiscoveryServer(t, xds.FakeOptions{
				Configs:           tt.configs,
				KubernetesObjects: tt.objs,
			})

			assertSANs := func(t *testing.T, clusters []*cluster.Cluster, c string, sans []string) {
				t.Helper()
				cluster := xdstest.ExtractClusters(clusters)[c]
				if cluster == nil {
					t.Fatal("cluster not found")
				}
				cluster.GetTransportSocket().GetTypedConfig()
				tl := xdstest.UnmarshalAny[tls.UpstreamTlsContext](t, cluster.GetTransportSocket().GetTypedConfig())
				names := sets.New()
				// nolint: staticcheck
				for _, n := range tl.GetCommonTlsContext().GetCombinedValidationContext().GetDefaultValidationContext().GetMatchSubjectAltNames() {
					names.Insert(n.GetExact())
				}
				assert.Equal(t, names.SortedList(), sets.New(sans...).SortedList())
			}
			// Run multiple assertions to verify idempotency; previous versions had issues here.
			for i := 0; i < 2; i++ {
				clusters := s.Clusters(s.SetupProxy(&model.Proxy{ConfigNamespace: "test"}))
				assertSANs(t, clusters, "outbound|80||example.default.svc.cluster.local", tt.sans)
				t.Logf("iteration %d passed", i)
			}
		})
	}
}
