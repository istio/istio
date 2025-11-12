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

package serviceregistry_test

import (
	"context"
	"fmt"
	"reflect"
	"sort"
	"testing"
	"time"

	core "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	v1 "k8s.io/api/core/v1"
	discovery "k8s.io/api/discovery/v1"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/kubernetes"

	"istio.io/api/annotation"
	"istio.io/api/label"
	meshconfig "istio.io/api/mesh/v1alpha1"
	"istio.io/api/meta/v1alpha1"
	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/config/memory"
	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/model/status"
	"istio.io/istio/pilot/pkg/serviceregistry/aggregate"
	kubecontroller "istio.io/istio/pilot/pkg/serviceregistry/kube/controller"
	"istio.io/istio/pilot/pkg/serviceregistry/serviceentry"
	"istio.io/istio/pilot/pkg/serviceregistry/util/xdsfake"
	v3 "istio.io/istio/pilot/pkg/xds/v3"
	"istio.io/istio/pilot/test/xds"
	"istio.io/istio/pilot/test/xdstest"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/config/mesh"
	"istio.io/istio/pkg/config/mesh/meshwatcher"
	"istio.io/istio/pkg/config/schema/collections"
	"istio.io/istio/pkg/config/schema/gvk"
	kubeclient "istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/maps"
	pm "istio.io/istio/pkg/model"
	"istio.io/istio/pkg/slices"
	istiotest "istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/util/assert"
	"istio.io/istio/pkg/test/util/retry"
)

func setupTest(t *testing.T) (model.ConfigStoreController, kubernetes.Interface, *xdsfake.Updater) {
	t.Helper()
	client := kubeclient.NewFakeClient()

	endpoints := model.NewEndpointIndex(model.DisabledCache{})
	delegate := model.NewEndpointIndexUpdater(endpoints)
	xdsUpdater := xdsfake.NewWithDelegate(delegate)
	delegate.ConfigUpdateFunc = xdsUpdater.ConfigUpdate
	meshWatcher := meshwatcher.NewTestWatcher(&meshconfig.MeshConfig{})
	kc := kubecontroller.NewController(
		client,
		kubecontroller.Options{
			XDSUpdater:            xdsUpdater,
			DomainSuffix:          "cluster.local",
			MeshWatcher:           meshWatcher,
			MeshServiceController: aggregate.NewController(aggregate.Options{MeshHolder: meshWatcher}),
		},
	)
	configController := memory.NewController(memory.Make(collections.Pilot))

	stop := istiotest.NewStop(t)
	go configController.Run(stop)

	se := serviceentry.NewController(configController, xdsUpdater, meshWatcher)
	client.RunAndWait(stop)

	kc.AppendWorkloadHandler(se.WorkloadInstanceHandler)
	se.AppendWorkloadHandler(kc.WorkloadInstanceHandler)

	go kc.Run(stop)
	go se.Run(stop)

	return configController, client.Kube(), xdsUpdater
}

// TestWorkloadInstances is effectively an integration test of composing the Kubernetes service registry with the
// external service registry, which have cross-references by workload instances.
func TestWorkloadInstances(t *testing.T) {
	istiotest.SetForTest(t, &features.WorkloadEntryHealthChecks, true)
	port := &networking.ServicePort{
		Name:     "http",
		Number:   80,
		Protocol: "http",
	}
	labels := map[string]string{
		"app": "foo",
	}
	namespace := "namespace"
	serviceEntry := config.Config{
		Meta: config.Meta{
			Name:             "service-entry",
			Namespace:        namespace,
			GroupVersionKind: gvk.ServiceEntry,
			Domain:           "cluster.local",
		},
		Spec: &networking.ServiceEntry{
			Hosts: []string{"service.namespace.svc.cluster.local"},
			Ports: []*networking.ServicePort{port},
			WorkloadSelector: &networking.WorkloadSelector{
				Labels: labels,
			},
			Resolution: networking.ServiceEntry_STATIC,
		},
	}
	service := &v1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "service",
			Namespace: namespace,
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
	headlessServiceHTTP := &v1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "service",
			Namespace: namespace,
		},
		Spec: v1.ServiceSpec{
			Ports: []v1.ServicePort{{
				Name: "http",
				Port: 80,
			}},
			Selector:  labels,
			ClusterIP: v1.ClusterIPNone,
		},
	}
	headlessServiceTCP := &v1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "service",
			Namespace: namespace,
		},
		Spec: v1.ServiceSpec{
			Ports: []v1.ServicePort{{
				Name: "tcp",
				Port: 80,
			}},
			Selector:  labels,
			ClusterIP: v1.ClusterIPNone,
		},
	}
	pod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "pod",
			Namespace:   namespace,
			Labels:      labels,
			Annotations: map[string]string{},
		},
		Status: v1.PodStatus{
			PodIP: "1.2.3.4",
			Phase: v1.PodPending,
		},
	}
	workloadEntry := config.Config{
		Meta: config.Meta{
			Name:             "workload",
			Namespace:        namespace,
			GroupVersionKind: gvk.WorkloadEntry,
			Domain:           "cluster.local",
		},
		Spec: &networking.WorkloadEntry{
			Address: "2.3.4.5",
			Labels:  labels,
		},
	}
	expectedSvc := &model.Service{
		Hostname: "service.namespace.svc.cluster.local",
		Ports: []*model.Port{{
			Name:     "http",
			Port:     80,
			Protocol: "http",
		}, {
			Name:     "http2",
			Port:     90,
			Protocol: "http",
		}, {
			Name:     "tcp",
			Port:     70,
			Protocol: "tcp",
		}},
		Attributes: model.ServiceAttributes{
			Namespace:      namespace,
			Name:           "service",
			LabelSelectors: labels,
		},
	}

	t.Run("Kubernetes only", func(t *testing.T) {
		_, kube, fx := setupTest(t)
		makeService(t, kube, service)
		makePod(t, kube, pod)
		createEndpoints(t, kube, service.Name, namespace, []v1.EndpointPort{{Name: "http", Port: 80}}, []string{pod.Status.PodIP})

		instances := []EndpointResponse{{
			Address: pod.Status.PodIP,
			Port:    80,
		}}
		expectServiceEndpoints(t, fx, expectedSvc, 80, instances)
	})

	t.Run("Kubernetes pod labels update", func(t *testing.T) {
		_, kube, xdsUpdater := setupTest(t)
		makeService(t, kube, service)
		xdsUpdater.WaitOrFail(t, "service")
		makePod(t, kube, pod)
		xdsUpdater.WaitOrFail(t, "proxy")
		newPod := pod.DeepCopy()
		newPod.Labels["newlabel"] = "new"
		makePod(t, kube, newPod)
		xdsUpdater.WaitOrFail(t, "proxy")
	})

	t.Run("Kubernetes only: headless pure HTTP service", func(t *testing.T) {
		_, kube, fx := setupTest(t)
		makeService(t, kube, headlessServiceHTTP)
		fx.WaitOrFail(t, "service")
		makePod(t, kube, pod)
		createEndpoints(t, kube, service.Name, namespace, []v1.EndpointPort{{Name: "http", Port: 80}}, []string{pod.Status.PodIP})
		fx.WaitOrFail(t, "eds")
		// Endpoint update is triggered since its a brand new service
		if ev := fx.WaitOrFail(t, "xds full"); !ev.Reason.Has(model.EndpointUpdate) {
			t.Fatalf("xds push reason does not contain %v: %v", model.EndpointUpdate, ev)
		}
		// headless service update must trigger nds push, so we trigger a full push.
		if ev := fx.WaitOrFail(t, "xds full"); !ev.Reason.Has(model.HeadlessEndpointUpdate) {
			t.Fatalf("xds push reason does not contain %v: %v", model.HeadlessEndpointUpdate, ev)
		}

		// pure HTTP headless services should not need a full push since they do not
		// require a Listener based on IP: https://github.com/istio/istio/issues/48207
		instances := []EndpointResponse{{
			Address: pod.Status.PodIP,
			Port:    80,
		}}
		expectServiceEndpoints(t, fx, expectedSvc, 80, instances)
	})

	t.Run("Kubernetes only: headless non-HTTP service", func(t *testing.T) {
		_, kube, fx := setupTest(t)
		makeService(t, kube, headlessServiceTCP)
		fx.WaitOrFail(t, "service")
		makePod(t, kube, pod)
		createEndpoints(t, kube, service.Name, namespace, []v1.EndpointPort{{Name: "tcp", Port: 70}}, []string{pod.Status.PodIP})
		fx.WaitOrFail(t, "eds")
		// Endpoint update is triggered since its a brand new service
		if ev := fx.WaitOrFail(t, "xds full"); !ev.Reason.Has(model.EndpointUpdate) {
			t.Fatalf("xds push reason does not contain %v: %v", model.EndpointUpdate, ev)
		}
		// headless service update must trigger nds push, so we trigger a full push.
		if ev := fx.WaitOrFail(t, "xds full"); !ev.Reason.Has(model.HeadlessEndpointUpdate) {
			t.Fatalf("xds push reason does not contain %v: %v", model.HeadlessEndpointUpdate, ev)
		}
		instances := []EndpointResponse{{
			Address: pod.Status.PodIP,
			Port:    70,
		}}
		expectServiceEndpoints(t, fx, expectedSvc, 70, instances)
	})

	t.Run("Kubernetes only: endpoint occur earlier", func(t *testing.T) {
		_, kube, fx := setupTest(t)
		makePod(t, kube, pod)

		createEndpoints(t, kube, service.Name, namespace, []v1.EndpointPort{{Name: "http", Port: 80}}, []string{pod.Status.PodIP})
		waitForEdsUpdate(t, fx, 1)

		// make service populated later than endpoint
		makeService(t, kube, service)
		fx.WaitOrFail(t, "eds cache")

		instances := []EndpointResponse{{
			Address: pod.Status.PodIP,
			Port:    80,
		}}
		expectServiceEndpoints(t, fx, expectedSvc, 80, instances)
	})

	t.Run("External only: workLoadEntry port and serviceEntry target port is not set, use serviceEntry port.number", func(t *testing.T) {
		store, _, fx := setupTest(t)
		makeIstioObject(t, store, serviceEntry)
		makeIstioObject(t, store, workloadEntry)

		instances := []EndpointResponse{{
			Address: workloadEntry.Spec.(*networking.WorkloadEntry).Address,
			Port:    80,
		}}
		expectServiceEndpoints(t, fx, expectedSvc, 80, instances)
	})

	t.Run("External only: the port name of the workloadEntry and serviceEntry does match, use workloadEntry port to override", func(t *testing.T) {
		store, _, fx := setupTest(t)
		makeIstioObject(t, store, serviceEntry)
		makeIstioObject(t, store, config.Config{
			Meta: config.Meta{
				Name:             "workload",
				Namespace:        namespace,
				GroupVersionKind: gvk.WorkloadEntry,
				Domain:           "cluster.local",
			},
			Spec: &networking.WorkloadEntry{
				Address: "2.3.4.5",
				Labels:  labels,
				Ports: map[string]uint32{
					serviceEntry.Spec.(*networking.ServiceEntry).Ports[0].Name: 8080,
				},
			},
		})

		instances := []EndpointResponse{{
			Address: workloadEntry.Spec.(*networking.WorkloadEntry).Address,
			Port:    8080,
		}}
		expectServiceEndpoints(t, fx, expectedSvc, 80, instances)
	})

	t.Run("External only: the port name of the workloadEntry and serviceEntry does match, "+
		"serviceEntry's targetPort not equal workloadEntry's, use workloadEntry port to override", func(t *testing.T) {
		store, _, fx := setupTest(t)
		se := serviceEntry.Spec.(*networking.ServiceEntry).DeepCopy()
		se.Ports[0].TargetPort = 8081 // respect wle port firstly, does not care about this value at all.

		makeIstioObject(t, store, config.Config{
			Meta: config.Meta{
				Name:             "workload",
				Namespace:        namespace,
				GroupVersionKind: gvk.WorkloadEntry,
				Domain:           "cluster.local",
			},
			Spec: &networking.WorkloadEntry{
				Address: "2.3.4.5",
				Labels:  labels,
				Ports: map[string]uint32{
					serviceEntry.Spec.(*networking.ServiceEntry).Ports[0].Name: 8080,
				},
			},
		})

		makeIstioObject(t, store, serviceEntry)

		instances := []EndpointResponse{{
			Address: workloadEntry.Spec.(*networking.WorkloadEntry).Address,
			Port:    8080,
		}}
		expectServiceEndpoints(t, fx, expectedSvc, 80, instances)
	})

	t.Run("External only: workloadEntry port is not set, use target port", func(t *testing.T) {
		store, _, fx := setupTest(t)
		makeIstioObject(t, store, config.Config{
			Meta: config.Meta{
				Name:             "service-entry",
				Namespace:        namespace,
				GroupVersionKind: gvk.ServiceEntry,
				Domain:           "cluster.local",
			},
			Spec: &networking.ServiceEntry{
				Hosts: []string{"service.namespace.svc.cluster.local"},
				Ports: []*networking.ServicePort{{
					Name:       "http",
					Number:     80,
					Protocol:   "http",
					TargetPort: 8080,
				}},
				WorkloadSelector: &networking.WorkloadSelector{
					Labels: labels,
				},
			},
		})
		makeIstioObject(t, store, workloadEntry)

		instances := []EndpointResponse{{
			Address: workloadEntry.Spec.(*networking.WorkloadEntry).Address,
			Port:    8080,
		}}
		expectServiceEndpoints(t, fx, expectedSvc, 80, instances)
	})

	t.Run("External only: the port name of the workloadEntry and serviceEntry does not match, use target port", func(t *testing.T) {
		store, _, fx := setupTest(t)
		makeIstioObject(t, store, config.Config{
			Meta: config.Meta{
				Name:             "service-entry",
				Namespace:        namespace,
				GroupVersionKind: gvk.ServiceEntry,
				Domain:           "cluster.local",
			},
			Spec: &networking.ServiceEntry{
				Hosts: []string{"service.namespace.svc.cluster.local"},
				Ports: []*networking.ServicePort{{
					Name:       "http",
					Number:     80,
					Protocol:   "http",
					TargetPort: 8080,
				}},
				WorkloadSelector: &networking.WorkloadSelector{
					Labels: labels,
				},
			},
		})
		makeIstioObject(t, store, config.Config{
			Meta: config.Meta{
				Name:             "workload",
				Namespace:        namespace,
				GroupVersionKind: gvk.WorkloadEntry,
				Domain:           "cluster.local",
			},
			Spec: &networking.WorkloadEntry{
				Address: "2.3.4.5",
				Labels:  labels,
				Ports: map[string]uint32{
					"different-port-name": 8081,
				},
			},
		})

		instances := []EndpointResponse{{
			Address: workloadEntry.Spec.(*networking.WorkloadEntry).Address,
			Port:    8080,
		}}
		expectServiceEndpoints(t, fx, expectedSvc, 80, instances)
	})

	t.Run("External only: the port name of the workloadEntry and serviceEntry does not match, "+
		"and the serivceEntry target port is not set, use serviceEntry port.number", func(t *testing.T) {
		store, _, fx := setupTest(t)
		makeIstioObject(t, store, serviceEntry)
		makeIstioObject(t, store, config.Config{
			Meta: config.Meta{
				Name:             "workload",
				Namespace:        namespace,
				GroupVersionKind: gvk.WorkloadEntry,
				Domain:           "cluster.local",
			},
			Spec: &networking.WorkloadEntry{
				Address: "2.3.4.5",
				Labels:  labels,
				Ports: map[string]uint32{
					"different-port-name": 8081,
				},
			},
		})

		instances := []EndpointResponse{{
			Address: workloadEntry.Spec.(*networking.WorkloadEntry).Address,
			Port:    80,
		}}
		expectServiceEndpoints(t, fx, expectedSvc, 80, instances)
	})

	t.Run("External only: workloadEntry port is changed", func(t *testing.T) {
		store, _, fx := setupTest(t)
		makeIstioObject(t, store, config.Config{
			Meta: config.Meta{
				Name:             "service-entry",
				Namespace:        namespace,
				GroupVersionKind: gvk.ServiceEntry,
				Domain:           "cluster.local",
			},
			Spec: &networking.ServiceEntry{
				Hosts: []string{"service.namespace.svc.cluster.local"},
				Ports: []*networking.ServicePort{{
					Name:     "http",
					Number:   80,
					Protocol: "http",
				}},
				WorkloadSelector: &networking.WorkloadSelector{
					Labels: labels,
				},
			},
		})
		makeIstioObject(t, store, workloadEntry)
		fx.WaitOrFail(t, "xds full")

		instances := []EndpointResponse{{
			Address: workloadEntry.Spec.(*networking.WorkloadEntry).Address,
			Port:    80,
		}}
		expectServiceEndpoints(t, fx, expectedSvc, 80, instances)

		fx.Clear()
		// Update the port
		newWorkloadEntry := workloadEntry.DeepCopy()
		spec := workloadEntry.Spec.(*networking.WorkloadEntry).DeepCopy()
		spec.Ports = map[string]uint32{
			"http": 1234,
		}
		newWorkloadEntry.Spec = spec
		makeIstioObject(t, store, newWorkloadEntry)
		instances = []EndpointResponse{{
			Address: workloadEntry.Spec.(*networking.WorkloadEntry).Address,
			Port:    1234,
		}}
		fx.WaitOrFail(t, "xds")
		expectServiceEndpoints(t, fx, expectedSvc, 80, instances)
	})

	t.Run("External only: workloadEntry with overlapping IPs", func(t *testing.T) {
		store, _, fx := setupTest(t)
		makeIstioObject(t, store, config.Config{
			Meta: config.Meta{
				Name:             "service-entry",
				Namespace:        namespace,
				GroupVersionKind: gvk.ServiceEntry,
			},
			Spec: &networking.ServiceEntry{
				Hosts: []string{"example.com"},
				Ports: []*networking.ServicePort{
					{Number: 445, Name: "http-445", Protocol: "http"},
				},
				WorkloadSelector: &networking.WorkloadSelector{
					Labels: labels,
				},
			},
		})
		expectedSvc := &model.Service{
			Hostname: "example.com",
			Ports: []*model.Port{{
				Name:     "http-445",
				Port:     445,
				Protocol: "http",
			}},
			Attributes: model.ServiceAttributes{
				Namespace:      namespace,
				Name:           "service",
				LabelSelectors: labels,
			},
		}
		we1 := config.Config{
			Meta: config.Meta{
				Name:             "we1",
				Namespace:        namespace,
				GroupVersionKind: gvk.WorkloadEntry,
				Domain:           "cluster.local",
			},
			Spec: &networking.WorkloadEntry{
				Address: "2.3.4.5",
				Labels:  labels,
				Ports: map[string]uint32{
					"http-445": 1234,
				},
			},
		}
		we2 := config.Config{
			Meta: config.Meta{
				Name:             "we2",
				Namespace:        namespace,
				GroupVersionKind: gvk.WorkloadEntry,
				Domain:           "cluster.local",
			},
			Spec: &networking.WorkloadEntry{
				Address: "2.3.4.5",
				Labels:  labels,
				Ports: map[string]uint32{
					"http-445": 5678,
				},
			},
		}
		makeIstioObject(t, store, we1)
		makeIstioObject(t, store, we2)
		fx.WaitOrFail(t, "xds full")

		instances := []EndpointResponse{{
			Address: workloadEntry.Spec.(*networking.WorkloadEntry).Address,
			Port:    1234,
		}, {
			Address: workloadEntry.Spec.(*networking.WorkloadEntry).Address,
			Port:    5678,
		}}
		expectServiceEndpoints(t, fx, expectedSvc, 445, instances)

		fx.Clear()

		// Delete one of the WE
		_ = store.Delete(gvk.WorkloadEntry, we2.Name, we2.Namespace, nil)

		fx.WaitOrFail(t, "xds")
		instances = []EndpointResponse{
			{
				Address: workloadEntry.Spec.(*networking.WorkloadEntry).Address,
				Port:    1234,
			},
		}
		expectServiceEndpoints(t, fx, expectedSvc, 445, instances)
	})

	t.Run("External only: workloadEntry with overlapping IPs and multiple SE", func(t *testing.T) {
		store, _, fx := setupTest(t)
		makeIstioObject(t, store, config.Config{
			Meta: config.Meta{
				Name:             "service-entry1",
				Namespace:        namespace,
				GroupVersionKind: gvk.ServiceEntry,
			},
			Spec: &networking.ServiceEntry{
				Hosts: []string{"1.example.com"},
				Ports: []*networking.ServicePort{
					{Number: 445, Name: "http-445", Protocol: "http"},
				},
				WorkloadSelector: &networking.WorkloadSelector{
					Labels: labels,
				},
			},
		})
		makeIstioObject(t, store, config.Config{
			Meta: config.Meta{
				Name:             "service-entry2",
				Namespace:        namespace,
				GroupVersionKind: gvk.ServiceEntry,
			},
			Spec: &networking.ServiceEntry{
				Hosts: []string{"2.example.com"},
				Ports: []*networking.ServicePort{
					{Number: 445, Name: "http-445", Protocol: "http"},
				},
				WorkloadSelector: &networking.WorkloadSelector{
					Labels: labels,
				},
			},
		})
		expectedSvc := &model.Service{
			Hostname: "1.example.com",
			Ports: []*model.Port{{
				Name:     "http-445",
				Port:     445,
				Protocol: "http",
			}},
			Attributes: model.ServiceAttributes{
				Namespace:      namespace,
				Name:           "service",
				LabelSelectors: labels,
			},
		}
		we1 := config.Config{
			Meta: config.Meta{
				Name:             "we1",
				Namespace:        namespace,
				GroupVersionKind: gvk.WorkloadEntry,
				Domain:           "cluster.local",
			},
			Spec: &networking.WorkloadEntry{
				Address: "2.3.4.5",
				Labels:  labels,
				Ports: map[string]uint32{
					"http-445": 1234,
				},
			},
		}
		we2 := config.Config{
			Meta: config.Meta{
				Name:             "we2",
				Namespace:        namespace,
				GroupVersionKind: gvk.WorkloadEntry,
				Domain:           "cluster.local",
			},
			Spec: &networking.WorkloadEntry{
				Address: "2.3.4.5",
				Labels:  labels,
				Ports: map[string]uint32{
					"http-445": 5678,
				},
			},
		}
		makeIstioObject(t, store, we1)
		makeIstioObject(t, store, we2)
		fx.WaitOrFail(t, "xds full")

		instances := []EndpointResponse{{
			Address: workloadEntry.Spec.(*networking.WorkloadEntry).Address,
			Port:    1234,
		}, {
			Address: workloadEntry.Spec.(*networking.WorkloadEntry).Address,
			Port:    5678,
		}}
		expectServiceEndpoints(t, fx, expectedSvc, 445, instances)

		fx.Clear()

		// Delete one of the WE
		_ = store.Delete(gvk.WorkloadEntry, we2.Name, we2.Namespace, nil)

		fx.WaitOrFail(t, "xds")
		newInstances := []EndpointResponse{
			{
				Address: workloadEntry.Spec.(*networking.WorkloadEntry).Address,
				Port:    1234,
			},
		}
		expectServiceEndpoints(t, fx, expectedSvc, 445, newInstances)

		makeIstioObject(t, store, we2)
		expectServiceEndpoints(t, fx, expectedSvc, 445, instances)
	})

	t.Run("Service selects WorkloadEntry", func(t *testing.T) {
		store, kube, fx := setupTest(t)
		makeService(t, kube, service)
		makeIstioObject(t, store, workloadEntry)

		instances := []EndpointResponse{{
			Address: workloadEntry.Spec.(*networking.WorkloadEntry).Address,
			Port:    80,
		}}
		expectServiceEndpoints(t, fx, expectedSvc, 80, instances)
	})

	t.Run("Service selects WorkloadEntry: wle occur earlier", func(t *testing.T) {
		store, kube, fx := setupTest(t)
		makeIstioObject(t, store, workloadEntry)
		// 	Other than proxy update, no event pushed when workload entry created as no service entry
		fx.WaitOrFail(t, "proxy")
		fx.AssertEmpty(t, 40*time.Millisecond)

		makeService(t, kube, service)
		fx.MatchOrFail(t, xdsfake.Event{Type: "eds cache", EndpointCount: 1})

		instances := []EndpointResponse{{
			Address: workloadEntry.Spec.(*networking.WorkloadEntry).Address,
			Port:    80,
		}}
		expectServiceEndpoints(t, fx, expectedSvc, 80, instances)
	})

	t.Run("Service selects both pods and WorkloadEntry", func(t *testing.T) {
		store, kube, fx := setupTest(t)
		makeService(t, kube, service)
		fx.WaitOrFail(t, "service")

		makeIstioObject(t, store, workloadEntry)
		fx.WaitOrFail(t, "eds")

		makePod(t, kube, pod)
		createEndpoints(t, kube, service.Name, namespace, []v1.EndpointPort{{Name: "http", Port: 80}}, []string{pod.Status.PodIP})
		waitForEdsUpdate(t, fx, 2)

		instances := []EndpointResponse{
			{
				Address: pod.Status.PodIP,
				Port:    80,
			},
			{
				Address: workloadEntry.Spec.(*networking.WorkloadEntry).Address,
				Port:    80,
			},
		}
		expectServiceEndpoints(t, fx, expectedSvc, 80, instances)
	})

	t.Run("Service selects both pods and WorkloadEntry: wle occur earlier", func(t *testing.T) {
		store, kube, fx := setupTest(t)
		makeIstioObject(t, store, workloadEntry)

		// 	Other than proxy update, no event pushed when workload entry created as no service entry
		fx.WaitOrFail(t, "proxy")
		fx.AssertEmpty(t, 200*time.Millisecond)

		makePod(t, kube, pod)
		createEndpoints(t, kube, service.Name, namespace, []v1.EndpointPort{{Name: "http", Port: 80}}, []string{pod.Status.PodIP})
		waitForEdsUpdate(t, fx, 1)

		makeService(t, kube, service)
		fx.WaitOrFail(t, "eds cache")

		instances := []EndpointResponse{
			{
				Address: pod.Status.PodIP,
				Port:    80,
			},
			{
				Address: workloadEntry.Spec.(*networking.WorkloadEntry).Address,
				Port:    80,
			},
		}
		expectServiceEndpoints(t, fx, expectedSvc, 80, instances)
	})

	t.Run("Service selects WorkloadEntry with port name", func(t *testing.T) {
		store, kube, fx := setupTest(t)
		expectedSvc := &model.Service{
			Hostname: "service.namespace.svc.cluster.local",
			Ports: []*model.Port{{
				Name:     "my-port",
				Port:     80,
				Protocol: "http",
			}},
			Attributes: model.ServiceAttributes{
				Namespace:      namespace,
				Name:           "service",
				LabelSelectors: labels,
			},
		}
		makeService(t, kube, &v1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "service",
				Namespace: namespace,
			},
			Spec: v1.ServiceSpec{
				Ports: []v1.ServicePort{{
					Name: "my-port",
					Port: 80,
				}},
				Selector:  labels,
				ClusterIP: "9.9.9.9",
			},
		})
		makeIstioObject(t, store, config.Config{
			Meta: config.Meta{
				Name:             "workload",
				Namespace:        namespace,
				GroupVersionKind: gvk.WorkloadEntry,
				Domain:           "cluster.local",
			},
			Spec: &networking.WorkloadEntry{
				Address: "2.3.4.5",
				Labels:  labels,
				Ports: map[string]uint32{
					"my-port": 8080,
				},
			},
		})

		instances := []EndpointResponse{{
			Address: workloadEntry.Spec.(*networking.WorkloadEntry).Address,
			Port:    8080,
		}}
		expectServiceEndpoints(t, fx, expectedSvc, 80, instances)
	})

	t.Run("Service selects WorkloadEntry with targetPort name", func(t *testing.T) {
		store, kube, fx := setupTest(t)
		makeService(t, kube, &v1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "service",
				Namespace: namespace,
			},
			Spec: v1.ServiceSpec{
				Ports: []v1.ServicePort{{
					Name:       "http",
					Port:       80,
					TargetPort: intstr.Parse("my-port"),
				}},
				Selector:  labels,
				ClusterIP: "9.9.9.9",
			},
		})
		makeIstioObject(t, store, config.Config{
			Meta: config.Meta{
				Name:             "workload",
				Namespace:        namespace,
				GroupVersionKind: gvk.WorkloadEntry,
				Domain:           "cluster.local",
			},
			Spec: &networking.WorkloadEntry{
				Address: "2.3.4.5",
				Labels:  labels,
				Ports: map[string]uint32{
					"my-port": 8080,
				},
			},
		})

		instances := []EndpointResponse{{
			Address: workloadEntry.Spec.(*networking.WorkloadEntry).Address,
			Port:    8080,
		}}
		expectServiceEndpoints(t, fx, expectedSvc, 80, instances)
	})

	t.Run("Service selects WorkloadEntry with targetPort number", func(t *testing.T) {
		s := xds.NewFakeDiscoveryServer(t, xds.FakeOptions{})
		ei := s.Discovery.Env.EndpointIndex
		makeService(t, s.KubeClient().Kube(), &v1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "service",
				Namespace: namespace,
			},
			Spec: v1.ServiceSpec{
				Ports: []v1.ServicePort{
					{
						Name:       "http",
						Port:       80,
						TargetPort: intstr.FromInt32(8080),
					},
					{
						Name:       "http2",
						Port:       90,
						TargetPort: intstr.FromInt32(9090),
					},
				},
				Selector:  labels,
				ClusterIP: "9.9.9.9",
			},
		})
		makeIstioObject(t, s.Store(), config.Config{
			Meta: config.Meta{
				Name:             "workload",
				Namespace:        namespace,
				GroupVersionKind: gvk.WorkloadEntry,
				Domain:           "cluster.local",
			},
			Spec: &networking.WorkloadEntry{
				Address: "2.3.4.5",
				Labels:  labels,
			},
		})

		instances := []EndpointResponse{{
			Address: workloadEntry.Spec.(*networking.WorkloadEntry).Address,
			Port:    8080,
		}}
		expectServiceEndpointsFromIndex(t, ei, expectedSvc, 80, instances)
		expectEndpoints(t, s, "outbound|80||service.namespace.svc.cluster.local", []string{"2.3.4.5:8080"}, nil)
		instances = []EndpointResponse{{
			Address: workloadEntry.Spec.(*networking.WorkloadEntry).Address,
			Port:    9090,
		}}
		expectServiceEndpointsFromIndex(t, ei, expectedSvc, 90, instances)
		expectEndpoints(t, s, "outbound|90||service.namespace.svc.cluster.local", []string{"2.3.4.5:9090"}, nil)
	})

	t.Run("ServiceEntry selects Pod", func(t *testing.T) {
		store, kube, fx := setupTest(t)
		makeIstioObject(t, store, serviceEntry)
		makePod(t, kube, pod)

		instances := []EndpointResponse{{
			Address: pod.Status.PodIP,
			Port:    80,
		}}
		expectServiceEndpoints(t, fx, expectedSvc, 80, instances)
	})

	t.Run("ServiceEntry selects Pod that is in transit states", func(t *testing.T) {
		store, kube, fx := setupTest(t)
		makeIstioObject(t, store, serviceEntry)
		makePod(t, kube, pod)

		instances := []EndpointResponse{{
			Address: pod.Status.PodIP,
			Port:    80,
		}}
		expectServiceEndpoints(t, fx, expectedSvc, 80, instances)

		// when pods become unready, we should see the instances being removed from the registry
		setPodUnready(pod)
		_, err := kube.CoreV1().Pods(pod.Namespace).UpdateStatus(context.TODO(), pod, metav1.UpdateOptions{})
		if err != nil {
			t.Fatal(err)
		}
		expectServiceEndpoints(t, fx, expectedSvc, 80, []EndpointResponse{})

		setPodReady(pod)
		_, err = kube.CoreV1().Pods(pod.Namespace).UpdateStatus(context.TODO(), pod, metav1.UpdateOptions{})
		if err != nil {
			t.Fatal(err)
		}
		expectServiceEndpoints(t, fx, expectedSvc, 80, instances)
	})

	t.Run("ServiceEntry selects Pod that is Failed without IP", func(t *testing.T) {
		store, kube, fx := setupTest(t)
		makeIstioObject(t, store, serviceEntry)
		makePod(t, kube, pod)
		// Copy the pod since other tests expect it to have an IP.
		p2 := pod.DeepCopy()
		instances := []EndpointResponse{{
			Address: p2.Status.PodIP,
			Port:    80,
		}}
		expectServiceEndpoints(t, fx, expectedSvc, 80, instances)

		// Failed pods should have their endpoints removed from the registry, despite not having an IP.
		p2.Status.PodIP = ""
		p2.Status.PodIPs = nil
		p2.Status.Phase = v1.PodFailed
		_, err := kube.CoreV1().Pods(p2.Namespace).UpdateStatus(context.TODO(), p2, metav1.UpdateOptions{})
		if err != nil {
			t.Fatal(err)
		}
		expectServiceEndpoints(t, fx, expectedSvc, 80, []EndpointResponse{})
	})

	t.Run("ServiceEntry selects Pod that is Failed with an IP", func(t *testing.T) {
		store, kube, fx := setupTest(t)
		makeIstioObject(t, store, serviceEntry)
		makePod(t, kube, pod)
		p2 := pod.DeepCopy()
		instances := []EndpointResponse{{
			Address: p2.Status.PodIP,
			Port:    80,
		}}
		expectServiceEndpoints(t, fx, expectedSvc, 80, instances)

		// Failed pods should have their endpoints removed from the registry
		p2.Status.Phase = v1.PodFailed
		_, err := kube.CoreV1().Pods(p2.Namespace).UpdateStatus(context.TODO(), p2, metav1.UpdateOptions{})
		if err != nil {
			t.Fatal(err)
		}
		expectServiceEndpoints(t, fx, expectedSvc, 80, []EndpointResponse{})

		// Removing the IP should be a no-op
		p2.Status.PodIP = ""
		p2.Status.PodIPs = nil
		_, err = kube.CoreV1().Pods(p2.Namespace).UpdateStatus(context.TODO(), p2, metav1.UpdateOptions{})
		if err != nil {
			t.Fatal(err)
		}
		expectServiceEndpoints(t, fx, expectedSvc, 80, []EndpointResponse{})
	})

	t.Run("ServiceEntry selects Pod with IP removed", func(t *testing.T) {
		store, kube, fx := setupTest(t)
		makeIstioObject(t, store, serviceEntry)
		makePod(t, kube, pod)
		p2 := pod.DeepCopy()
		instances := []EndpointResponse{{
			Address: p2.Status.PodIP,
			Port:    80,
		}}
		expectServiceEndpoints(t, fx, expectedSvc, 80, instances)

		// Pods without an IP can't be ready.
		p2.Status.PodIP = ""
		p2.Status.PodIPs = nil
		_, err := kube.CoreV1().Pods(p2.Namespace).UpdateStatus(context.TODO(), p2, metav1.UpdateOptions{})
		if err != nil {
			t.Fatal(err)
		}
		expectServiceEndpoints(t, fx, expectedSvc, 80, []EndpointResponse{})

		// Failing the pod should be a no-op
		p2.Status.Phase = v1.PodFailed
		_, err = kube.CoreV1().Pods(p2.Namespace).UpdateStatus(context.TODO(), p2, metav1.UpdateOptions{})
		if err != nil {
			t.Fatal(err)
		}
		expectServiceEndpoints(t, fx, expectedSvc, 80, []EndpointResponse{})
	})
	t.Run("ServiceEntry selects Pod with targetPort number", func(t *testing.T) {
		store, kube, fx := setupTest(t)
		makeIstioObject(t, store, config.Config{
			Meta: config.Meta{
				Name:             "service-entry",
				Namespace:        namespace,
				GroupVersionKind: gvk.ServiceEntry,
				Domain:           "cluster.local",
			},
			Spec: &networking.ServiceEntry{
				Hosts: []string{"service.namespace.svc.cluster.local"},
				Ports: []*networking.ServicePort{{
					Name:       "http",
					Number:     80,
					Protocol:   "http",
					TargetPort: 8080,
				}},
				WorkloadSelector: &networking.WorkloadSelector{
					Labels: labels,
				},
			},
		})
		makePod(t, kube, pod)

		instances := []EndpointResponse{{
			Address: pod.Status.PodIP,
			Port:    8080,
		}}
		expectServiceEndpoints(t, fx, expectedSvc, 80, instances)
	})

	t.Run("ServiceEntry selects WorkloadEntry", func(t *testing.T) {
		store, _, fx := setupTest(t)
		makeIstioObject(t, store, config.Config{
			Meta: config.Meta{
				Name:             "service-entry-1",
				Namespace:        namespace,
				GroupVersionKind: gvk.ServiceEntry,
				Domain:           "cluster.local",
			},
			Spec: &networking.ServiceEntry{
				Hosts: []string{"service-1.namespace.svc.cluster.local"},
				Ports: []*networking.ServicePort{port},
				WorkloadSelector: &networking.WorkloadSelector{
					Labels: map[string]string{
						"app.foo": "true",
					},
				},
				Resolution: networking.ServiceEntry_STATIC,
			},
		})
		makeIstioObject(t, store, config.Config{
			Meta: config.Meta{
				Name:             "service-entry-2",
				Namespace:        namespace,
				GroupVersionKind: gvk.ServiceEntry,
				Domain:           "cluster.local",
			},
			Spec: &networking.ServiceEntry{
				Hosts: []string{"service-2.namespace.svc.cluster.local"},
				Ports: []*networking.ServicePort{port},
				WorkloadSelector: &networking.WorkloadSelector{
					Labels: map[string]string{
						"app.bar": "true",
					},
				},
				Resolution: networking.ServiceEntry_STATIC,
			},
		})
		// Both service entries share a common workload entry
		makeIstioObject(t, store, config.Config{
			Meta: config.Meta{
				Name:             "workloadentry",
				Namespace:        namespace,
				GroupVersionKind: gvk.WorkloadEntry,
				Domain:           "cluster.local",
			},
			Spec: &networking.WorkloadEntry{
				Address: "2.3.4.5",
				Labels: map[string]string{
					"app.foo": "true",
					"app.bar": "true",
				},
			},
		})

		fx.WaitOrFail(t, "xds full")
		instances := []EndpointResponse{{
			Address: "2.3.4.5",
			Port:    80,
		}}
		expectedSvc1 := &model.Service{
			Hostname: "service-1.namespace.svc.cluster.local",
			Ports: []*model.Port{{
				Name:     "http",
				Port:     80,
				Protocol: "http",
			}},
			Attributes: model.ServiceAttributes{
				Namespace: namespace,
				Name:      "service-entry-1",
				LabelSelectors: map[string]string{
					"app.foo": "true",
				},
			},
		}
		expectedSvc2 := &model.Service{
			Hostname: "service-2.namespace.svc.cluster.local",
			Ports: []*model.Port{{
				Name:     "http",
				Port:     80,
				Protocol: "http",
			}},
			Attributes: model.ServiceAttributes{
				Namespace: namespace,
				Name:      "service-entry-2",
				LabelSelectors: map[string]string{
					"app.bar": "true",
				},
			},
		}
		expectServiceEndpoints(t, fx, expectedSvc1, 80, instances)
		expectServiceEndpoints(t, fx, expectedSvc2, 80, instances)
		fx.Clear()
		// Delete the `app.foo` label to unselect service entry
		makeIstioObject(t, store, config.Config{
			Meta: config.Meta{
				Name:             "workloadentry",
				Namespace:        namespace,
				GroupVersionKind: gvk.WorkloadEntry,
				Domain:           "cluster.local",
			},
			Spec: &networking.WorkloadEntry{
				Address: "2.3.4.5",
				Labels: map[string]string{
					"app.bar": "true",
				},
			},
		})
		fx.WaitOrFail(t, "xds")
		expectServiceEndpoints(t, fx, expectedSvc1, 80, []EndpointResponse{})
		expectServiceEndpoints(t, fx, expectedSvc2, 80, instances)
	})

	t.Run("Multiport ServiceEntry selects WorkloadEntry", func(t *testing.T) {
		store, _, fx := setupTest(t)
		makeIstioObject(t, store, config.Config{
			Meta: config.Meta{
				Name:             "service-entry",
				Namespace:        namespace,
				GroupVersionKind: gvk.ServiceEntry,
				Domain:           "cluster.local",
			},
			Spec: &networking.ServiceEntry{
				Hosts: []string{"service.namespace.svc.cluster.local"},
				Ports: []*networking.ServicePort{
					{
						Name:     "http",
						Number:   80,
						Protocol: "http",
					},
					{
						Name:     "tcp",
						Number:   3000,
						Protocol: "tcp",
					},
				},
				WorkloadSelector: &networking.WorkloadSelector{
					Labels: map[string]string{
						"app.foo": "true",
					},
				},
				Resolution: networking.ServiceEntry_STATIC,
			},
		})
		makeIstioObject(t, store, config.Config{
			Meta: config.Meta{
				Name:             "workloadentry",
				Namespace:        namespace,
				GroupVersionKind: gvk.WorkloadEntry,
				Domain:           "cluster.local",
			},
			Spec: &networking.WorkloadEntry{
				Address: "2.3.4.5",
				Labels: map[string]string{
					"app.foo": "true",
				},
				Ports: map[string]uint32{
					"http": 8080,
					"tcp":  3000,
				},
			},
		})

		fx.WaitOrFail(t, "xds full")
		expectedSvc := &model.Service{
			Hostname: "service.namespace.svc.cluster.local",
			Ports: []*model.Port{
				{
					Name:     "http",
					Port:     80,
					Protocol: "http",
				},
				{
					Name:     "tcp",
					Port:     3000,
					Protocol: "tcp",
				},
			},
			Attributes: model.ServiceAttributes{
				Namespace: namespace,
				Name:      "service-entry",
				LabelSelectors: map[string]string{
					"app.foo": "true",
				},
			},
		}
		// Two endpoints will be generated: one for service port 80 and another for service port 3000.
		expectServiceEndpoints(t, fx, expectedSvc, 80, []EndpointResponse{{Address: "2.3.4.5", Port: 8080}})
		expectServiceEndpoints(t, fx, expectedSvc, 3000, []EndpointResponse{{Address: "2.3.4.5", Port: 3000}})
		fx.Clear()
		// Update the TCP port to 4000.
		// This update should trigger a comparison with the TCP endpoint, not the HTTP one,
		// which could cause unnecessary pushes.
		makeIstioObject(t, store, config.Config{
			Meta: config.Meta{
				Name:             "workloadentry",
				Namespace:        namespace,
				GroupVersionKind: gvk.WorkloadEntry,
				Domain:           "cluster.local",
			},
			Spec: &networking.WorkloadEntry{
				Address: "2.3.4.5",
				Labels: map[string]string{
					"app.foo": "true",
				},
				Ports: map[string]uint32{
					"http": 8080,
					"tcp":  4000,
				},
			},
		})
		fx.WaitOrFail(t, "xds")
		expectServiceEndpoints(t, fx, expectedSvc, 80, []EndpointResponse{{Address: "2.3.4.5", Port: 8080}})
		expectServiceEndpoints(t, fx, expectedSvc, 3000, []EndpointResponse{{Address: "2.3.4.5", Port: 4000}})
	})

	t.Run("All directions", func(t *testing.T) {
		store, kube, fx := setupTest(t)
		makeService(t, kube, service)
		makeIstioObject(t, store, serviceEntry)

		makePod(t, kube, pod)
		createEndpoints(t, kube, service.Name, namespace, []v1.EndpointPort{{Name: "http", Port: 80}}, []string{pod.Status.PodIP})
		makeIstioObject(t, store, workloadEntry)

		instances := []EndpointResponse{
			{Address: pod.Status.PodIP, Port: 80},
			{Address: pod.Status.PodIP, Port: 80},
			{Address: workloadEntry.Spec.(*networking.WorkloadEntry).Address, Port: 80},
			{Address: workloadEntry.Spec.(*networking.WorkloadEntry).Address, Port: 80},
		}

		expectServiceEndpoints(t, fx, expectedSvc, 80, instances)
	})

	t.Run("All directions with deletion", func(t *testing.T) {
		store, kube, fx := setupTest(t)
		makeService(t, kube, service)
		makeIstioObject(t, store, serviceEntry)

		makePod(t, kube, pod)
		createEndpoints(t, kube, service.Name, namespace, []v1.EndpointPort{{Name: "http", Port: 80}}, []string{pod.Status.PodIP})
		makeIstioObject(t, store, workloadEntry)

		instances := []EndpointResponse{
			{Address: pod.Status.PodIP, Port: 80},
			{Address: pod.Status.PodIP, Port: 80},
			{Address: workloadEntry.Spec.(*networking.WorkloadEntry).Address, Port: 80},
			{Address: workloadEntry.Spec.(*networking.WorkloadEntry).Address, Port: 80},
		}

		expectServiceEndpoints(t, fx, expectedSvc, 80, instances)

		_ = kube.CoreV1().Pods(pod.Namespace).Delete(context.TODO(), pod.Name, metav1.DeleteOptions{})
		_ = kube.DiscoveryV1().EndpointSlices(pod.Namespace).Delete(context.TODO(), "service", metav1.DeleteOptions{})
		_ = store.Delete(gvk.WorkloadEntry, workloadEntry.Name, workloadEntry.Namespace, nil)
		expectServiceEndpoints(t, fx, expectedSvc, 80, []EndpointResponse{})
	})

	t.Run("Service selects WorkloadEntry: update service", func(t *testing.T) {
		s := xds.NewFakeDiscoveryServer(t, xds.FakeOptions{})
		makeService(t, s.KubeClient().Kube(), service)
		makeIstioObject(t, s.Store(), workloadEntry)
		expectEndpoints(t, s, "outbound|80||service.namespace.svc.cluster.local", []string{"2.3.4.5:80"}, nil)

		newSvc := service.DeepCopy()
		newSvc.Spec.Ports[0].Port = 8080
		makeService(t, s.KubeClient().Kube(), newSvc)
		expectEndpoints(t, s, "outbound|80||service.namespace.svc.cluster.local", nil, nil)
		expectEndpoints(t, s, "outbound|8080||service.namespace.svc.cluster.local", []string{"2.3.4.5:8080"}, nil)

		newSvc.Spec.Ports[0].TargetPort = intstr.IntOrString{IntVal: 9090}
		makeService(t, s.KubeClient().Kube(), newSvc)
		expectEndpoints(t, s, "outbound|80||service.namespace.svc.cluster.local", nil, nil)
		expectEndpoints(t, s, "outbound|8080||service.namespace.svc.cluster.local", []string{"2.3.4.5:9090"}, nil)

		if err := s.KubeClient().Kube().CoreV1().Services(newSvc.Namespace).Delete(context.Background(), newSvc.Name, metav1.DeleteOptions{}); err != nil {
			t.Fatal(err)
		}
		expectEndpoints(t, s, "outbound|8080||service.namespace.svc.cluster.local", nil, nil)
	})

	t.Run("Service selects WorkloadEntry: update workloadEntry", func(t *testing.T) {
		s := xds.NewFakeDiscoveryServer(t, xds.FakeOptions{})
		makeService(t, s.KubeClient().Kube(), service)
		makeIstioObject(t, s.Store(), workloadEntry)
		expectEndpoints(t, s, "outbound|80||service.namespace.svc.cluster.local", []string{"2.3.4.5:80"}, nil)

		newWE := workloadEntry.DeepCopy()
		newWE.Spec.(*networking.WorkloadEntry).Address = "3.4.5.6"
		makeIstioObject(t, s.Store(), newWE)
		expectEndpoints(t, s, "outbound|80||service.namespace.svc.cluster.local", []string{"3.4.5.6:80"}, nil)

		if err := s.Store().Delete(gvk.WorkloadEntry, newWE.Name, newWE.Namespace, nil); err != nil {
			t.Fatal(err)
		}
		expectEndpoints(t, s, "outbound|80||service.namespace.svc.cluster.local", nil, nil)
	})

	t.Run("Service selects WorkloadEntry: health status", func(t *testing.T) {
		store, kube, fx := setupTest(t)
		makeService(t, kube, service)

		// Start as unhealthy, should have no instances
		makeIstioObject(t, store, setHealth(workloadEntry, false))
		instances := []EndpointResponse{}
		expectServiceEndpoints(t, fx, expectedSvc, 80, instances)

		// Mark healthy, get instances
		makeIstioObject(t, store, setHealth(workloadEntry, true))
		instances = []EndpointResponse{{
			Address: workloadEntry.Spec.(*networking.WorkloadEntry).Address,
			Port:    80,
		}}
		expectServiceEndpoints(t, fx, expectedSvc, 80, instances)

		// Set back to unhealthy
		makeIstioObject(t, store, setHealth(workloadEntry, false))
		instances = []EndpointResponse{}
		expectServiceEndpoints(t, fx, expectedSvc, 80, instances)

		// Remove health status entirely
		makeIstioObject(t, store, workloadEntry)
		instances = []EndpointResponse{{
			Address: workloadEntry.Spec.(*networking.WorkloadEntry).Address,
			Port:    80,
		}}
		expectServiceEndpoints(t, fx, expectedSvc, 80, instances)
	})

	istiotest.SetForTest(t, &features.EnableSidecarHBONEListening, true)
	istiotest.SetForTest(t, &features.EnableAmbient, true)
	for _, ambient := range []bool{false, true} {
		name := "disabled"
		if ambient {
			name = "enabled"
		}
		m := mesh.DefaultMeshConfig()
		var nodeMeta *model.NodeMetadata
		if ambient {
			nodeMeta = &model.NodeMetadata{EnableHBONE: true}
			pod = pod.DeepCopy()
			pod.Annotations[annotation.AmbientRedirection.Name] = constants.AmbientRedirectionEnabled
		}
		opts := xds.FakeOptions{MeshConfig: m}
		t.Run("ambient "+name, func(t *testing.T) {
			t.Run("ServiceEntry selects Pod: update service entry", func(t *testing.T) {
				s := xds.NewFakeDiscoveryServer(t, opts)
				makeIstioObject(t, s.Store(), serviceEntry)
				makePod(t, s.KubeClient().Kube(), pod)
				expectEndpoints(t, s, "outbound|80||service.namespace.svc.cluster.local", expectAmbient([]string{"1.2.3.4:80"}, ambient), nodeMeta)

				newSE := serviceEntry.DeepCopy()
				newSE.Spec.(*networking.ServiceEntry).Ports = []*networking.ServicePort{{
					Name:       "http",
					Number:     80,
					Protocol:   "http",
					TargetPort: 8080,
				}}
				makeIstioObject(t, s.Store(), newSE)
				expectEndpoints(t, s, "outbound|80||service.namespace.svc.cluster.local", expectAmbient([]string{"1.2.3.4:8080"}, ambient), nodeMeta)

				newSE = newSE.DeepCopy()
				newSE.Spec.(*networking.ServiceEntry).Ports = []*networking.ServicePort{{
					Name:       "http",
					Number:     9090,
					Protocol:   "http",
					TargetPort: 9091,
				}}
				makeIstioObject(t, s.Store(), newSE)
				expectEndpoints(t, s, "outbound|80||service.namespace.svc.cluster.local", nil, nodeMeta)
				expectEndpoints(t, s, "outbound|9090||service.namespace.svc.cluster.local", expectAmbient([]string{"1.2.3.4:9091"}, ambient), nodeMeta)

				if err := s.Store().Delete(gvk.ServiceEntry, newSE.Name, newSE.Namespace, nil); err != nil {
					t.Fatal(err)
				}
				expectEndpoints(t, s, "outbound|80||service.namespace.svc.cluster.local", nil, nodeMeta)
				expectEndpoints(t, s, "outbound|9090||service.namespace.svc.cluster.local", nil, nodeMeta)
			})

			t.Run("ServiceEntry selects Pod: update pod", func(t *testing.T) {
				s := xds.NewFakeDiscoveryServer(t, opts)
				makeIstioObject(t, s.Store(), serviceEntry)
				makePod(t, s.KubeClient().Kube(), pod)
				expectEndpoints(t, s, "outbound|80||service.namespace.svc.cluster.local", expectAmbient([]string{"1.2.3.4:80"}, ambient), nodeMeta)

				newPod := pod.DeepCopy()
				newPod.Status.PodIP = "2.3.4.5"
				makePod(t, s.KubeClient().Kube(), newPod)
				expectEndpoints(t, s, "outbound|80||service.namespace.svc.cluster.local", expectAmbient([]string{"2.3.4.5:80"}, ambient), nodeMeta)

				if err := s.KubeClient().Kube().CoreV1().Pods(newPod.Namespace).Delete(context.Background(), newPod.Name, metav1.DeleteOptions{}); err != nil {
					t.Fatal(err)
				}
				expectEndpoints(t, s, "outbound|80||service.namespace.svc.cluster.local", nil, nodeMeta)
			})

			t.Run("ServiceEntry selects Pod: deleting pod", func(t *testing.T) {
				s := xds.NewFakeDiscoveryServer(t, opts)
				makeIstioObject(t, s.Store(), serviceEntry)
				makePod(t, s.KubeClient().Kube(), pod)
				expectEndpoints(t, s, "outbound|80||service.namespace.svc.cluster.local", expectAmbient([]string{"1.2.3.4:80"}, ambient), nodeMeta)

				// Simulate pod being deleted by setting deletion timestamp
				newPod := pod.DeepCopy()
				newPod.DeletionTimestamp = &metav1.Time{Time: time.Now()}
				makePod(t, s.KubeClient().Kube(), newPod)
				expectEndpoints(t, s, "outbound|80||service.namespace.svc.cluster.local", nil, nodeMeta)

				if err := s.KubeClient().Kube().CoreV1().Pods(newPod.Namespace).Delete(context.Background(), newPod.Name, metav1.DeleteOptions{}); err != nil {
					t.Fatal(err)
				}
				expectEndpoints(t, s, "outbound|80||service.namespace.svc.cluster.local", nil, nodeMeta)
			})
		})
	}
}

func expectAmbient(strings []string, ambient bool) []string {
	if !ambient {
		return strings
	}
	out := make([]string, 0, len(strings))
	for _, s := range strings {
		out = append(out, "connect_originate;"+s)
	}
	return out
}

func setHealth(cfg config.Config, healthy bool) config.Config {
	cfg = cfg.DeepCopy()
	if cfg.Annotations == nil {
		cfg.Annotations = map[string]string{}
	}
	cfg.Annotations[status.WorkloadEntryHealthCheckAnnotation] = "true"
	if healthy {
		return status.UpdateIstioConfigCondition(cfg, &v1alpha1.IstioCondition{
			Type:   status.ConditionHealthy,
			Status: status.StatusTrue,
		})
	}
	return status.UpdateIstioConfigCondition(cfg, &v1alpha1.IstioCondition{
		Type:   status.ConditionHealthy,
		Status: status.StatusFalse,
	})
}

func waitForEdsUpdate(t *testing.T, xdsUpdater *xdsfake.Updater, expected int) {
	t.Helper()
	xdsUpdater.MatchOrFail(t, xdsfake.Event{Type: "eds", EndpointCount: expected})
}

func TestEndpointsDeduping(t *testing.T) {
	s := xds.NewFakeDiscoveryServer(t, xds.FakeOptions{})
	namespace := "namespace"
	labels := map[string]string{
		"app": "bar",
	}
	ei := s.Env().EndpointIndex
	makeService(t, s.KubeClient().Kube(), &v1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "service",
			Namespace: namespace,
		},
		Spec: v1.ServiceSpec{
			Ports: []v1.ServicePort{{
				Name: "http",
				Port: 80,
			}, {
				Name: "http-other",
				Port: 90,
			}},
			Selector:  labels,
			ClusterIP: "9.9.9.9",
		},
	})
	// Create an expect endpoint
	createEndpointSlice(t, s.KubeClient().Kube(), "slice1", "service", namespace, []v1.EndpointPort{{Name: "http", Port: 80}}, []string{"1.2.3.4"})
	expectEndpoints(t, s, "outbound|80||service.namespace.svc.cluster.local", []string{"1.2.3.4:80"}, nil)

	// create an FQDN endpoint that should be ignored
	createEndpointSliceWithType(t, s.KubeClient().Kube(), "slice1", "service",
		namespace, []v1.EndpointPort{{Name: "http", Port: 80}}, []string{"foo.com"}, discovery.AddressTypeFQDN)
	expectEndpoints(t, s, "outbound|80||service.namespace.svc.cluster.local", []string{"1.2.3.4:80"}, nil)

	// Add another port endpoint
	createEndpointSlice(t, s.KubeClient().Kube(), "slice1", "service", namespace,
		[]v1.EndpointPort{{Name: "http-other", Port: 90}, {Name: "http", Port: 80}}, []string{"1.2.3.4", "2.3.4.5"})
	expectEndpoints(t, s, "outbound|80||service.namespace.svc.cluster.local", []string{"1.2.3.4:80", "2.3.4.5:80"}, nil)
	expectEndpoints(t, s, "outbound|90||service.namespace.svc.cluster.local", []string{"1.2.3.4:90", "2.3.4.5:90"}, nil)

	// Move the endpoint to another slice - transition phase where its duplicated
	createEndpointSlice(t, s.KubeClient().Kube(), "slice1", "service", namespace, []v1.EndpointPort{{Name: "http", Port: 80}}, []string{"1.2.3.5", "2.3.4.5"})
	createEndpointSlice(t, s.KubeClient().Kube(), "slice2", "service", namespace, []v1.EndpointPort{{Name: "http", Port: 80}}, []string{"2.3.4.5"})
	expectEndpoints(t, s, "outbound|80||service.namespace.svc.cluster.local", []string{"1.2.3.5:80", "2.3.4.5:80"}, nil)

	// Move the endpoint to another slice - completed
	createEndpointSlice(t, s.KubeClient().Kube(), "slice1", "service", namespace, []v1.EndpointPort{{Name: "http", Port: 80}}, []string{"1.2.3.4"})
	createEndpointSlice(t, s.KubeClient().Kube(), "slice2", "service", namespace, []v1.EndpointPort{{Name: "http", Port: 80}}, []string{"2.3.4.5"})
	expectEndpoints(t, s, "outbound|80||service.namespace.svc.cluster.local", []string{"1.2.3.4:80", "2.3.4.5:80"}, nil)

	// Delete endpoint
	createEndpointSlice(t, s.KubeClient().Kube(), "slice1", "service", namespace, []v1.EndpointPort{{Name: "http", Port: 80}}, []string{"1.2.3.4"})
	createEndpointSlice(t, s.KubeClient().Kube(), "slice2", "service", namespace, []v1.EndpointPort{{Name: "http", Port: 80}}, []string{})
	expectEndpoints(t, s, "outbound|80||service.namespace.svc.cluster.local", []string{"1.2.3.4:80"}, nil)

	_ = s.KubeClient().Kube().DiscoveryV1().EndpointSlices(namespace).Delete(context.TODO(), "slice1", metav1.DeleteOptions{})
	expectEndpoints(t, s, "outbound|80||service.namespace.svc.cluster.local", nil, nil)

	// Ensure there is nothing is left over
	expectServiceEndpointsFromIndex(t, ei, &model.Service{
		Hostname: "service.namespace.svc.cluster.local",
		Ports: []*model.Port{{
			Name:     "http",
			Port:     80,
			Protocol: "http",
		}},
		Attributes: model.ServiceAttributes{
			Namespace:      namespace,
			Name:           "service",
			LabelSelectors: labels,
		},
	}, 80, []EndpointResponse{})
}

// TestEndpointSlicingServiceUpdate is a regression test to ensure we do not end up with duplicate endpoints when a service changes.
func TestEndpointSlicingServiceUpdate(t *testing.T) {
	for _, version := range []string{"latest", "20"} {
		t.Run("kubernetes 1."+version, func(t *testing.T) {
			s := xds.NewFakeDiscoveryServer(t, xds.FakeOptions{
				KubernetesVersion:    version,
				EnableFakeXDSUpdater: true,
			})
			namespace := "namespace"
			labels := map[string]string{
				"app": "bar",
			}
			makeService(t, s.KubeClient().Kube(), &v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "service",
					Namespace: namespace,
				},
				Spec: v1.ServiceSpec{
					Ports: []v1.ServicePort{{
						Name: "http",
						Port: 80,
					}, {
						Name: "http-other",
						Port: 90,
					}},
					Selector:  labels,
					ClusterIP: "9.9.9.9",
				},
			})
			fx := s.XdsUpdater.(*xdsfake.Updater)
			createEndpointSlice(t, s.KubeClient().Kube(), "slice1", "service", namespace, []v1.EndpointPort{{Name: "http", Port: 80}}, []string{"1.2.3.4"})
			createEndpointSlice(t, s.KubeClient().Kube(), "slice2", "service", namespace, []v1.EndpointPort{{Name: "http", Port: 80}}, []string{"1.2.3.4"})
			expectEndpoints(t, s, "outbound|80||service.namespace.svc.cluster.local", []string{"1.2.3.4:80"}, nil)
			fx.WaitOrFail(t, "service")

			// Trigger a service updates
			makeService(t, s.KubeClient().Kube(), &v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "service",
					Namespace: namespace,
					Labels:    map[string]string{"foo": "bar"},
				},
				Spec: v1.ServiceSpec{
					Ports: []v1.ServicePort{{
						Name: "http",
						Port: 80,
					}, {
						Name: "http-other",
						Port: 90,
					}},
					Selector:  labels,
					ClusterIP: "9.9.9.9",
				},
			})
			fx.WaitOrFail(t, "service")
			expectEndpoints(t, s, "outbound|80||service.namespace.svc.cluster.local", []string{"1.2.3.4:80"}, nil)
		})
	}
}

func TestSameIPEndpointSlicing(t *testing.T) {
	s := xds.NewFakeDiscoveryServer(t, xds.FakeOptions{
		EnableFakeXDSUpdater: true,
	})
	namespace := "namespace"
	labels := map[string]string{
		"app": "bar",
	}
	makeService(t, s.KubeClient().Kube(), &v1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "service",
			Namespace: namespace,
		},
		Spec: v1.ServiceSpec{
			Ports: []v1.ServicePort{{
				Name: "http",
				Port: 80,
			}, {
				Name: "http-other",
				Port: 90,
			}},
			Selector:  labels,
			ClusterIP: "9.9.9.9",
		},
	})
	fx := s.XdsUpdater.(*xdsfake.Updater)

	// Delete endpoints with same IP
	createEndpointSlice(t, s.KubeClient().Kube(), "slice1", "service", namespace, []v1.EndpointPort{{Name: "http", Port: 80}}, []string{"1.2.3.4"})
	createEndpointSlice(t, s.KubeClient().Kube(), "slice2", "service", namespace, []v1.EndpointPort{{Name: "http", Port: 80}}, []string{"1.2.3.4"})
	expectEndpoints(t, s, "outbound|80||service.namespace.svc.cluster.local", []string{"1.2.3.4:80"}, nil)

	// delete slice 1, it should still exist
	_ = s.KubeClient().Kube().DiscoveryV1().EndpointSlices(namespace).Delete(context.TODO(), "slice1", metav1.DeleteOptions{})
	fx.WaitOrFail(t, "eds")
	expectEndpoints(t, s, "outbound|80||service.namespace.svc.cluster.local", []string{"1.2.3.4:80"}, nil)
	_ = s.KubeClient().Kube().DiscoveryV1().EndpointSlices(namespace).Delete(context.TODO(), "slice2", metav1.DeleteOptions{})
	fx.WaitOrFail(t, "eds")
	expectEndpoints(t, s, "outbound|80||service.namespace.svc.cluster.local", nil, nil)
}

type EndpointResponse struct {
	Address string
	Port    uint32
}

func expectEndpoints(t *testing.T, s *xds.FakeDiscoveryServer, cluster string, expected []string, metadata *model.NodeMetadata) {
	t.Helper()
	retry.UntilSuccessOrFail(t, func() error {
		got := xdstest.ExtractLoadAssignments(s.Endpoints(s.SetupProxy(&model.Proxy{Metadata: metadata})))
		sort.Strings(got[cluster])
		sort.Strings(expected)
		if !reflect.DeepEqual(got[cluster], expected) {
			return fmt.Errorf("wanted %v got %v. All endpoints: %+v", expected, got[cluster], got)
		}
		return nil
	}, retry.Converge(2), retry.Timeout(time.Second*2), retry.Delay(time.Millisecond*10))
}

func expectServiceEndpointsFromIndex(t *testing.T, ei *model.EndpointIndex, svc *model.Service, port int, expected []EndpointResponse) {
	t.Helper()
	// The system is eventually consistent, so add some retries
	retry.UntilSuccessOrFail(t, func() error {
		endpoints := GetEndpointsForPort(svc, ei, port)
		if endpoints == nil {
			endpoints = []*model.IstioEndpoint{} // To simplify tests a bit
		}
		got := slices.Map(endpoints, func(e *model.IstioEndpoint) EndpointResponse {
			return EndpointResponse{
				Address: e.Addresses[0],
				Port:    e.EndpointPort,
			}
		})
		slices.SortBy(got, func(a EndpointResponse) string {
			return fmt.Sprintf("%v:%d", a.Address, a.Port)
		})
		return assert.Compare(got, expected)
	}, retry.Converge(2), retry.Timeout(time.Second*2), retry.Delay(time.Millisecond*10))
}

// nolint: unparam
func expectServiceEndpoints(t *testing.T, fx *xdsfake.Updater, svc *model.Service, port int, expected []EndpointResponse) {
	t.Helper()
	expectServiceEndpointsFromIndex(t, fx.Delegate.(*model.FakeEndpointIndexUpdater).Index, svc, port, expected)
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

func setPodUnready(pod *v1.Pod) {
	pod.Status.Conditions = []v1.PodCondition{
		{
			Type:               v1.PodReady,
			Status:             v1.ConditionFalse,
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
	newPod.Status.PodIPs = []v1.PodIP{
		{
			IP: pod.Status.PodIP,
		},
	}
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

func createEndpoints(t *testing.T, c kubernetes.Interface, name, namespace string, ports []v1.EndpointPort, ips []string) {
	createEndpointSlice(t, c, name, name, namespace, ports, ips)
}

// nolint: unparam
func createEndpointSlice(t *testing.T, c kubernetes.Interface, name, serviceName, namespace string, ports []v1.EndpointPort, addrs []string) {
	createEndpointSliceWithType(t, c, name, serviceName, namespace, ports, addrs, discovery.AddressTypeIPv4)
}

// nolint: unparam
func createEndpointSliceWithType(t *testing.T, c kubernetes.Interface, name, serviceName, namespace string,
	ports []v1.EndpointPort, ips []string, addrType discovery.AddressType,
) {
	esps := make([]discovery.EndpointPort, 0)
	for _, name := range ports {
		n := name // Create a stable reference to take the pointer from
		esps = append(esps, discovery.EndpointPort{
			Name:        &n.Name,
			Protocol:    &n.Protocol,
			Port:        &n.Port,
			AppProtocol: n.AppProtocol,
		})
	}

	sliceEndpoint := []discovery.Endpoint{}
	for _, ip := range ips {
		sliceEndpoint = append(sliceEndpoint, discovery.Endpoint{
			Addresses: []string{ip},
		})
	}

	endpointSlice := &discovery.EndpointSlice{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels: map[string]string{
				discovery.LabelServiceName: serviceName,
			},
		},
		AddressType: addrType,
		Endpoints:   sliceEndpoint,
		Ports:       esps,
	}
	if _, err := c.DiscoveryV1().EndpointSlices(namespace).Create(context.TODO(), endpointSlice, metav1.CreateOptions{}); err != nil {
		if kerrors.IsAlreadyExists(err) {
			_, err = c.DiscoveryV1().EndpointSlices(namespace).Update(context.TODO(), endpointSlice, metav1.UpdateOptions{})
		}
		if err != nil {
			t.Fatalf("failed to create endpoint slice %s in namespace %s (error %v)", name, namespace, err)
		}
	}
}

func TestLocality(t *testing.T) {
	namespace := "default"
	basePod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-1",
			Namespace: namespace,
			Labels:    map[string]string{},
		},
		Spec: v1.PodSpec{NodeName: "node"},
		Status: v1.PodStatus{
			PodIP: "1.2.3.4",
			Phase: v1.PodRunning,
		},
	}
	setPodReady(basePod)
	baseNode := &v1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name:   "node",
			Labels: map[string]string{},
		},
	}
	cases := []struct {
		name     string
		pod      *v1.Pod
		node     *v1.Node
		obj      config.Config
		expected *core.Locality
	}{
		{
			name:     "no locality",
			pod:      basePod,
			node:     baseNode,
			expected: &core.Locality{},
		},
		{
			name: "pod specific label",
			pod: func() *v1.Pod {
				p := basePod.DeepCopy()
				p.Labels[pm.LocalityLabel] = "r.z.s"
				return p
			}(),
			node: baseNode,
			expected: &core.Locality{
				Region:  "r",
				Zone:    "z",
				SubZone: "s",
			},
		},
		{
			name: "node specific label",
			pod:  basePod,
			node: func() *v1.Node {
				p := baseNode.DeepCopy()
				p.Labels[kubecontroller.NodeRegionLabelGA] = "r"
				p.Labels[kubecontroller.NodeZoneLabelGA] = "z"
				p.Labels[label.TopologySubzone.Name] = "s"
				return p
			}(),
			expected: &core.Locality{
				Region:  "r",
				Zone:    "z",
				SubZone: "s",
			},
		},
		{
			name: "pod and node labels",
			pod: func() *v1.Pod {
				p := basePod.DeepCopy()
				p.Labels[pm.LocalityLabel] = "r.z.s"
				return p
			}(),
			node: func() *v1.Node {
				p := baseNode.DeepCopy()
				p.Labels[kubecontroller.NodeRegionLabelGA] = "nr"
				p.Labels[kubecontroller.NodeZoneLabelGA] = "nz"
				p.Labels[label.TopologySubzone.Name] = "ns"
				return p
			}(),
			expected: &core.Locality{
				Region:  "r",
				Zone:    "z",
				SubZone: "s",
			},
		},
		{
			name: "ServiceEntry with explicit locality",
			obj: config.Config{
				Meta: config.Meta{
					Name:             "service-entry",
					Namespace:        namespace,
					GroupVersionKind: gvk.ServiceEntry,
				},
				Spec: &networking.ServiceEntry{
					Hosts: []string{"service.namespace.svc.cluster.local"},
					Ports: []*networking.ServicePort{{Name: "http", Number: 80, Protocol: "http"}},
					Endpoints: []*networking.WorkloadEntry{{
						Address:  "1.2.3.4",
						Locality: "r/z/s",
					}},
					Resolution: networking.ServiceEntry_STATIC,
				},
			},
			expected: &core.Locality{
				Region:  "r",
				Zone:    "z",
				SubZone: "s",
			},
		},
		{
			name: "ServiceEntry with label locality",
			obj: config.Config{
				Meta: config.Meta{
					Name:             "service-entry",
					Namespace:        namespace,
					GroupVersionKind: gvk.ServiceEntry,
				},
				Spec: &networking.ServiceEntry{
					Hosts: []string{"service.namespace.svc.cluster.local"},
					Ports: []*networking.ServicePort{{Name: "http", Number: 80, Protocol: "http"}},
					Endpoints: []*networking.WorkloadEntry{{
						Address: "1.2.3.4",
						Labels: map[string]string{
							pm.LocalityLabel: "r.z.s",
						},
					}},
					Resolution: networking.ServiceEntry_STATIC,
				},
			},
			expected: &core.Locality{
				Region:  "r",
				Zone:    "z",
				SubZone: "s",
			},
		},
		{
			name: "ServiceEntry with both locality",
			obj: config.Config{
				Meta: config.Meta{
					Name:             "service-entry",
					Namespace:        namespace,
					GroupVersionKind: gvk.ServiceEntry,
				},
				Spec: &networking.ServiceEntry{
					Hosts: []string{"service.namespace.svc.cluster.local"},
					Ports: []*networking.ServicePort{{Name: "http", Number: 80, Protocol: "http"}},
					Endpoints: []*networking.WorkloadEntry{{
						Address:  "1.2.3.4",
						Locality: "r/z/s",
						Labels: map[string]string{
							pm.LocalityLabel: "lr.lz.ls",
						},
					}},
					Resolution: networking.ServiceEntry_STATIC,
				},
			},
			expected: &core.Locality{
				Region:  "r",
				Zone:    "z",
				SubZone: "s",
			},
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			opts := xds.FakeOptions{}
			proxyName := ""
			if tt.pod != nil {
				opts.KubernetesObjects = append(opts.KubernetesObjects, tt.pod)
				proxyName = tt.pod.Name + "." + tt.pod.Namespace
			}
			if tt.node != nil {
				opts.KubernetesObjects = append(opts.KubernetesObjects, tt.node)
			}
			if tt.obj.Name != "" {
				opts.Configs = append(opts.Configs, tt.obj)
			}
			s := xds.NewFakeDiscoveryServer(t, opts)
			s.Connect(s.SetupProxy(&model.Proxy{ID: proxyName, IPAddresses: []string{"1.2.3.4"}}), nil, []string{v3.ClusterType})
			retry.UntilSuccessOrFail(t, func() error {
				clients := s.Discovery.AllClients()
				if len(clients) != 1 {
					return fmt.Errorf("got %d clients", len(clients))
				}
				locality := clients[0].Proxy().Locality
				return assert.Compare(tt.expected, locality)
			}, retry.Timeout(time.Second*2))
		})
	}
}

func GetEndpointsForPort(s *model.Service, endpoints *model.EndpointIndex, port int) []*model.IstioEndpoint {
	shards, ok := endpoints.ShardsForService(string(s.Hostname), s.Attributes.Namespace)
	if !ok {
		return nil
	}
	var pn string
	for _, p := range s.Ports {
		if p.Port == port {
			pn = p.Name
			break
		}
	}
	if pn == "" && port != 0 {
		return nil
	}
	shards.RLock()
	defer shards.RUnlock()
	return slices.FilterInPlace(slices.Flatten(maps.Values(shards.Shards)), func(endpoint *model.IstioEndpoint) bool {
		return pn == "" || endpoint.ServicePortName == pn
	})
}
