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
	"context"
	"testing"
	"time"

	cluster "github.com/envoyproxy/go-control-plane/envoy/config/cluster/v3"
	tls "github.com/envoyproxy/go-control-plane/envoy/extensions/transport_sockets/tls/v3"
	"istio.io/istio/pkg/test/util/assert"
	v1 "k8s.io/api/core/v1"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/xds"
	v3 "istio.io/istio/pilot/pkg/xds/v3"
	"istio.io/istio/pilot/test/xdstest"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/schema/gvk"
	"istio.io/istio/pkg/kube"
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
	se := config.Config{
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
			WorkloadSelector: &networking.WorkloadSelector{
				Labels: labels,
			},
			Resolution: networking.ServiceEntry_STATIC,
		},
	}
	t.Run("Service selects WorkloadEntry: health status", func(t *testing.T) {
		var kubeClient kube.Client
		s := xds.NewFakeDiscoveryServer(t, xds.FakeOptions{
			KubeClientModifier: func(c kube.Client) {
				kubeClient = c
			},
		})
		makeIstioObject(t, s.Store(), dr)
		makeIstioObject(t, s.Store(), se)

		makeService(t, kubeClient.Kube(), service)
		makePod(t, kubeClient.Kube(), pod)
		createEndpoints(t, kubeClient.Kube(), service.Name, service.Namespace, []v1.EndpointPort{{Name: "http", Port: 80}}, []string{pod.Status.PodIP})
		time.Sleep(time.Second * 2)

		assertSANs := func(t *testing.T, clusters []*cluster.Cluster, c string, sans ...string) {
			cluster := xdstest.ExtractClusters(clusters)[c]
			if cluster == nil {
				t.Fatal("cluster not found")
			}
			cluster.GetTransportSocket().GetTypedConfig()
			tl := xdstest.UnmarshalAny[tls.UpstreamTlsContext](t, cluster.GetTransportSocket().GetTypedConfig())
			names := sets.New()
			for _, n := range tl.GetCommonTlsContext().GetCombinedValidationContext().GetDefaultValidationContext().GetMatchSubjectAltNames() {
				names.Insert(n.String())
			}
			assert.Equal(t, names.SortedList(), sets.New(sans...).SortedList())
		}
		{
			clusters := s.Clusters(s.SetupProxy(&model.Proxy{ConfigNamespace: "test"}))
			assertSANs(t, clusters, "outbound|80||example.default.svc.cluster.local")
		}
		{
			clusters := s.Clusters(s.SetupProxy(&model.Proxy{ConfigNamespace: "test"}))
			assertSANs(t, clusters, "outbound|80||example.default.svc.cluster.local", "spiffe://cluster.local/ns/default/sa/")
		}
	})
}

func createEndpoints(t *testing.T, c kubernetes.Interface, name, namespace string, ports []v1.EndpointPort, ips []string) {
	eas := make([]v1.EndpointAddress, 0)
	for _, ip := range ips {
		eas = append(eas, v1.EndpointAddress{IP: ip, TargetRef: &v1.ObjectReference{
			Kind:      "Pod",
			Name:      "pod",
			Namespace: namespace,
		}})
	}

	endpoint := &v1.Endpoints{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Subsets: []v1.EndpointSubset{{
			Addresses: eas,
			Ports:     ports,
		}},
	}
	if _, err := c.CoreV1().Endpoints(namespace).Create(context.TODO(), endpoint, metav1.CreateOptions{}); err != nil {
		t.Fatalf("failed to create endpoints %s in namespace %s (error %v)", name, namespace, err)
	}
}

func setPodReady(pod *v1.Pod) {
	pod.Status.Conditions = []v1.PodCondition{
		{
			Type:               v1.PodReady,
			Status:             v1.ConditionTrue,
			LastTransitionTime: metav1.Now(),
		},
	}
}

func makePod(t *testing.T, c kubernetes.Interface, pod *v1.Pod) {
	t.Helper()
	newPod, err := c.CoreV1().Pods(pod.Namespace).Create(context.Background(), pod, metav1.CreateOptions{})
	if kerrors.IsAlreadyExists(err) {
		newPod, err = c.CoreV1().Pods(pod.Namespace).Update(context.Background(), pod, metav1.UpdateOptions{})
	}
	if err != nil {
		t.Fatal(err)
	}
	// Apiserver doesn't allow Create/Update to modify the pod status. Creating doesn't result in
	// events - since PodIP will be "".
	newPod.Status.PodIP = pod.Status.PodIP
	newPod.Status.Phase = v1.PodRunning

	// Also need to sets the pod to be ready as now we only add pod into service entry endpoint when it's ready
	setPodReady(newPod)
	_, err = c.CoreV1().Pods(pod.Namespace).UpdateStatus(context.TODO(), newPod, metav1.UpdateOptions{})
	if err != nil {
		t.Fatal(err)
	}
}

func makeService(t *testing.T, c kubernetes.Interface, svc *v1.Service) {
	t.Helper()
	// avoid mutating input
	svc = svc.DeepCopy()
	// simulate actual k8s behavior
	for i, port := range svc.Spec.Ports {
		if port.TargetPort.IntVal == 0 && port.TargetPort.StrVal == "" {
			svc.Spec.Ports[i].TargetPort.IntVal = port.Port
		}
	}

	_, err := c.CoreV1().Services(svc.Namespace).Create(context.Background(), svc, metav1.CreateOptions{})
	if kerrors.IsAlreadyExists(err) {
		_, err = c.CoreV1().Services(svc.Namespace).Update(context.Background(), svc, metav1.UpdateOptions{})
	}
	if err != nil {
		t.Fatal(err)
	}
}

func makeIstioObject(t *testing.T, c model.ConfigStore, svc config.Config) {
	t.Helper()
	_, err := c.Create(svc)
	if err != nil && err.Error() == "item already exists" {
		_, err = c.Update(svc)
	}
	if err != nil {
		t.Fatal(err)
	}
}
