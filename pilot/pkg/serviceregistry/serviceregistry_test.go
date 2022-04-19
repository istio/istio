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
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"testing"
	"time"

	v1 "k8s.io/api/core/v1"
	discovery "k8s.io/api/discovery/v1"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/kubernetes"

	meshconfig "istio.io/api/mesh/v1alpha1"
	"istio.io/api/meta/v1alpha1"
	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/config/memory"
	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/model/status"
	"istio.io/istio/pilot/pkg/serviceregistry"
	"istio.io/istio/pilot/pkg/serviceregistry/aggregate"
	kubecontroller "istio.io/istio/pilot/pkg/serviceregistry/kube/controller"
	"istio.io/istio/pilot/pkg/serviceregistry/serviceentry"
	"istio.io/istio/pilot/pkg/xds"
	"istio.io/istio/pilot/test/util"
	"istio.io/istio/pilot/test/xdstest"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/host"
	"istio.io/istio/pkg/config/mesh"
	"istio.io/istio/pkg/config/schema/collections"
	"istio.io/istio/pkg/config/schema/gvk"
	kubeclient "istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/test/util/retry"
)

func setupTest(t *testing.T) (
	*kubecontroller.Controller,
	*serviceentry.Controller,
	model.ConfigStoreController,
	kubernetes.Interface,
	*xds.FakeXdsUpdater) {
	t.Helper()
	client := kubeclient.NewFakeClient()

	eventch := make(chan xds.FakeXdsEvent, 100)

	xdsUpdater := &xds.FakeXdsUpdater{
		Events: eventch,
	}
	meshWatcher := mesh.NewFixedWatcher(&meshconfig.MeshConfig{})
	kc := kubecontroller.NewController(
		client,
		kubecontroller.Options{
			XDSUpdater:            xdsUpdater,
			DomainSuffix:          "cluster.local",
			MeshWatcher:           meshWatcher,
			MeshServiceController: aggregate.NewController(aggregate.Options{meshWatcher}),
		},
	)
	configController := memory.NewController(memory.Make(collections.Pilot))

	stop := make(chan struct{})
	t.Cleanup(func() {
		close(stop)
	})
	go configController.Run(stop)

	istioStore := model.MakeIstioStore(configController)
	se := serviceentry.NewController(configController, istioStore, xdsUpdater)
	client.RunAndWait(stop)

	kc.AppendWorkloadHandler(se.WorkloadInstanceHandler)
	se.AppendWorkloadHandler(kc.WorkloadInstanceHandler)

	go kc.Run(stop)
	go se.Run(stop)

	return kc, se, configController, client.Kube(), xdsUpdater
}

// TestWorkloadInstances is effectively an integration test of composing the Kubernetes service registry with the
// external service registry, which have cross-references by workload instances.
func TestWorkloadInstances(t *testing.T) {
	features.WorkloadEntryHealthChecks = true
	port := &networking.Port{
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
			Ports: []*networking.Port{port},
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
	headlessService := &v1.Service{
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
	pod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "pod",
			Namespace: namespace,
			Labels:    labels,
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
		}},
		Attributes: model.ServiceAttributes{
			Namespace:      namespace,
			Name:           "service",
			LabelSelectors: labels,
		},
	}

	t.Run("Kubernetes only", func(t *testing.T) {
		kc, _, _, kube, _ := setupTest(t)
		makeService(t, kube, service)
		makePod(t, kube, pod)
		createEndpoints(t, kube, service.Name, namespace, []v1.EndpointPort{{Name: "http", Port: 80}}, []string{pod.Status.PodIP})

		instances := []ServiceInstanceResponse{{
			Hostname:   expectedSvc.Hostname,
			Namestring: expectedSvc.Attributes.Namespace,
			Address:    pod.Status.PodIP,
			Port:       80,
		}}
		expectServiceInstances(t, kc, expectedSvc, 80, instances)
	})

	t.Run("Kubernetes only: headless service", func(t *testing.T) {
		kc, _, _, kube, xdsUpdater := setupTest(t)
		makeService(t, kube, headlessService)
		xdsUpdater.WaitOrFail(t, "svcupdate")
		makePod(t, kube, pod)
		createEndpoints(t, kube, service.Name, namespace, []v1.EndpointPort{{Name: "http", Port: 80}}, []string{pod.Status.PodIP})
		xdsUpdater.WaitOrFail(t, "eds")
		xdsUpdater.WaitOrFail(t, "xds")
		instances := []ServiceInstanceResponse{{
			Hostname:   expectedSvc.Hostname,
			Namestring: expectedSvc.Attributes.Namespace,
			Address:    pod.Status.PodIP,
			Port:       80,
		}}
		expectServiceInstances(t, kc, expectedSvc, 80, instances)
	})

	t.Run("Kubernetes only: endpoint occur earlier", func(t *testing.T) {
		kc, _, _, kube, xdsUpdater := setupTest(t)
		makePod(t, kube, pod)

		createEndpoints(t, kube, service.Name, namespace, []v1.EndpointPort{{Name: "http", Port: 80}}, []string{pod.Status.PodIP})
		waitForEdsUpdate(t, xdsUpdater, 1)

		// make service populated later than endpoint
		makeService(t, kube, service)
		waitForEdsUpdate(t, xdsUpdater, 1)

		instances := []ServiceInstanceResponse{{
			Hostname:   expectedSvc.Hostname,
			Namestring: expectedSvc.Attributes.Namespace,
			Address:    pod.Status.PodIP,
			Port:       80,
		}}
		expectServiceInstances(t, kc, expectedSvc, 80, instances)
	})

	t.Run("External only: workLoadEntry port and serviceEntry target port is not set, use serviceEntry port.number", func(t *testing.T) {
		_, wc, store, _, _ := setupTest(t)
		makeIstioObject(t, store, serviceEntry)
		makeIstioObject(t, store, workloadEntry)

		instances := []ServiceInstanceResponse{{
			Hostname:   expectedSvc.Hostname,
			Namestring: expectedSvc.Attributes.Namespace,
			Address:    workloadEntry.Spec.(*networking.WorkloadEntry).Address,
			Port:       80,
		}}
		expectServiceInstances(t, wc, expectedSvc, 80, instances)
	})

	t.Run("External only: the port name of the workloadEntry and serviceEntry does match, use workloadEntry port to override", func(t *testing.T) {
		_, wc, store, _, _ := setupTest(t)
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

		instances := []ServiceInstanceResponse{{
			Hostname:   expectedSvc.Hostname,
			Namestring: expectedSvc.Attributes.Namespace,
			Address:    workloadEntry.Spec.(*networking.WorkloadEntry).Address,
			Port:       8080,
		}}
		expectServiceInstances(t, wc, expectedSvc, 80, instances)
	})

	t.Run("External only: workloadEntry port is not set, use target port", func(t *testing.T) {
		_, wc, store, _, _ := setupTest(t)
		makeIstioObject(t, store, config.Config{
			Meta: config.Meta{
				Name:             "service-entry",
				Namespace:        namespace,
				GroupVersionKind: gvk.ServiceEntry,
				Domain:           "cluster.local",
			},
			Spec: &networking.ServiceEntry{
				Hosts: []string{"service.namespace.svc.cluster.local"},
				Ports: []*networking.Port{{
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

		instances := []ServiceInstanceResponse{{
			Hostname:   expectedSvc.Hostname,
			Namestring: expectedSvc.Attributes.Namespace,
			Address:    workloadEntry.Spec.(*networking.WorkloadEntry).Address,
			Port:       8080,
		}}
		expectServiceInstances(t, wc, expectedSvc, 80, instances)
	})

	t.Run("External only: the port name of the workloadEntry and serviceEntry does not match, use target port", func(t *testing.T) {
		_, wc, store, _, _ := setupTest(t)
		makeIstioObject(t, store, config.Config{
			Meta: config.Meta{
				Name:             "service-entry",
				Namespace:        namespace,
				GroupVersionKind: gvk.ServiceEntry,
				Domain:           "cluster.local",
			},
			Spec: &networking.ServiceEntry{
				Hosts: []string{"service.namespace.svc.cluster.local"},
				Ports: []*networking.Port{{
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

		instances := []ServiceInstanceResponse{{
			Hostname:   expectedSvc.Hostname,
			Namestring: expectedSvc.Attributes.Namespace,
			Address:    workloadEntry.Spec.(*networking.WorkloadEntry).Address,
			Port:       8080,
		}}
		expectServiceInstances(t, wc, expectedSvc, 80, instances)
	})

	t.Run("External only: the port name of the workloadEntry and serviceEntry does not match, "+
		"and the serivceEntry target port is not set, use serviceEntry port.number", func(t *testing.T) {
		_, wc, store, _, _ := setupTest(t)
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

		instances := []ServiceInstanceResponse{{
			Hostname:   expectedSvc.Hostname,
			Namestring: expectedSvc.Attributes.Namespace,
			Address:    workloadEntry.Spec.(*networking.WorkloadEntry).Address,
			Port:       80,
		}}
		expectServiceInstances(t, wc, expectedSvc, 80, instances)
	})

	t.Run("Service selects WorkloadEntry", func(t *testing.T) {
		kc, _, store, kube, _ := setupTest(t)
		makeService(t, kube, service)
		makeIstioObject(t, store, workloadEntry)

		instances := []ServiceInstanceResponse{{
			Hostname:   expectedSvc.Hostname,
			Namestring: expectedSvc.Attributes.Namespace,
			Address:    workloadEntry.Spec.(*networking.WorkloadEntry).Address,
			Port:       80,
		}}
		expectServiceInstances(t, kc, expectedSvc, 80, instances)
	})

	t.Run("Service selects WorkloadEntry: wle occur earlier", func(t *testing.T) {
		kc, _, store, kube, xdsUpdater := setupTest(t)
		makeIstioObject(t, store, workloadEntry)

		// Wait no event pushed when workload entry created as no service entry
		select {
		case ev := <-xdsUpdater.Events:
			t.Fatalf("Got %s event, expect none", ev.Kind)
		case <-time.After(40 * time.Millisecond):
		}

		makeService(t, kube, service)
		event := xdsUpdater.WaitOrFail(t, "edscache")
		if event.Endpoints != 1 {
			t.Errorf("expecting 1 endpoints, but got %d ", event.Endpoints)
		}

		instances := []ServiceInstanceResponse{{
			Hostname:   expectedSvc.Hostname,
			Namestring: expectedSvc.Attributes.Namespace,
			Address:    workloadEntry.Spec.(*networking.WorkloadEntry).Address,
			Port:       80,
		}}
		expectServiceInstances(t, kc, expectedSvc, 80, instances)
	})

	t.Run("Service selects both pods and WorkloadEntry", func(t *testing.T) {
		kc, _, store, kube, xdsUpdater := setupTest(t)
		makeService(t, kube, service)
		xdsUpdater.WaitOrFail(t, "svcupdate")

		makeIstioObject(t, store, workloadEntry)
		xdsUpdater.WaitOrFail(t, "eds")

		makePod(t, kube, pod)
		createEndpoints(t, kube, service.Name, namespace, []v1.EndpointPort{{Name: "http", Port: 80}}, []string{pod.Status.PodIP})
		waitForEdsUpdate(t, xdsUpdater, 2)

		instances := []ServiceInstanceResponse{
			{
				Hostname:   expectedSvc.Hostname,
				Namestring: expectedSvc.Attributes.Namespace,
				Address:    pod.Status.PodIP,
				Port:       80,
			},
			{
				Hostname:   expectedSvc.Hostname,
				Namestring: expectedSvc.Attributes.Namespace,
				Address:    workloadEntry.Spec.(*networking.WorkloadEntry).Address,
				Port:       80,
			},
		}
		expectServiceInstances(t, kc, expectedSvc, 80, instances)
	})

	t.Run("Service selects both pods and WorkloadEntry: wle occur earlier", func(t *testing.T) {
		kc, _, store, kube, xdsUpdater := setupTest(t)
		makeIstioObject(t, store, workloadEntry)

		// Wait no event pushed when workload entry created as no service entry
		select {
		case ev := <-xdsUpdater.Events:
			t.Fatalf("Got %s event, expect none", ev.Kind)
		case <-time.After(200 * time.Millisecond):
		}

		makePod(t, kube, pod)
		createEndpoints(t, kube, service.Name, namespace, []v1.EndpointPort{{Name: "http", Port: 80}}, []string{pod.Status.PodIP})
		waitForEdsUpdate(t, xdsUpdater, 1)

		makeService(t, kube, service)
		waitForEdsUpdate(t, xdsUpdater, 2)

		instances := []ServiceInstanceResponse{
			{
				Hostname:   expectedSvc.Hostname,
				Namestring: expectedSvc.Attributes.Namespace,
				Address:    pod.Status.PodIP,
				Port:       80,
			},
			{
				Hostname:   expectedSvc.Hostname,
				Namestring: expectedSvc.Attributes.Namespace,
				Address:    workloadEntry.Spec.(*networking.WorkloadEntry).Address,
				Port:       80,
			},
		}
		expectServiceInstances(t, kc, expectedSvc, 80, instances)
	})

	t.Run("Service selects WorkloadEntry with port name", func(t *testing.T) {
		kc, _, store, kube, _ := setupTest(t)
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

		instances := []ServiceInstanceResponse{{
			Hostname:   expectedSvc.Hostname,
			Namestring: expectedSvc.Attributes.Namespace,
			Address:    workloadEntry.Spec.(*networking.WorkloadEntry).Address,
			Port:       8080,
		}}
		expectServiceInstances(t, kc, expectedSvc, 80, instances)
	})

	t.Run("Service selects WorkloadEntry with targetPort name", func(t *testing.T) {
		kc, _, store, kube, _ := setupTest(t)
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

		instances := []ServiceInstanceResponse{{
			Hostname:   expectedSvc.Hostname,
			Namestring: expectedSvc.Attributes.Namespace,
			Address:    workloadEntry.Spec.(*networking.WorkloadEntry).Address,
			Port:       8080,
		}}
		expectServiceInstances(t, kc, expectedSvc, 80, instances)
	})

	t.Run("Service selects WorkloadEntry with targetPort number", func(t *testing.T) {
		s := xds.NewFakeDiscoveryServer(t, xds.FakeOptions{})
		makeService(t, s.KubeClient(), &v1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "service",
				Namespace: namespace,
			},
			Spec: v1.ServiceSpec{
				Ports: []v1.ServicePort{
					{
						Name:       "http",
						Port:       80,
						TargetPort: intstr.FromInt(8080),
					},
					{
						Name:       "http2",
						Port:       90,
						TargetPort: intstr.FromInt(9090),
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

		instances := []ServiceInstanceResponse{{
			Hostname:   expectedSvc.Hostname,
			Namestring: expectedSvc.Attributes.Namespace,
			Address:    workloadEntry.Spec.(*networking.WorkloadEntry).Address,
			Port:       8080,
		}}
		expectServiceInstances(t, s.KubeRegistry, expectedSvc, 80, instances)
		expectEndpoints(t, s, "outbound|80||service.namespace.svc.cluster.local", []string{"2.3.4.5:8080"})
		instances = []ServiceInstanceResponse{{
			Hostname:   expectedSvc.Hostname,
			Namestring: expectedSvc.Attributes.Namespace,
			Address:    workloadEntry.Spec.(*networking.WorkloadEntry).Address,
			Port:       9090,
		}}
		expectServiceInstances(t, s.KubeRegistry, expectedSvc, 90, instances)
		expectEndpoints(t, s, "outbound|90||service.namespace.svc.cluster.local", []string{"2.3.4.5:9090"})
	})

	t.Run("ServiceEntry selects Pod", func(t *testing.T) {
		_, wc, store, kube, _ := setupTest(t)
		makeIstioObject(t, store, serviceEntry)
		makePod(t, kube, pod)

		instances := []ServiceInstanceResponse{{
			Hostname:   expectedSvc.Hostname,
			Namestring: expectedSvc.Attributes.Namespace,
			Address:    pod.Status.PodIP,
			Port:       80,
		}}
		expectServiceInstances(t, wc, expectedSvc, 80, instances)
	})

	t.Run("ServiceEntry selects Pod that is in transit states", func(t *testing.T) {
		_, wc, store, kube, _ := setupTest(t)
		makeIstioObject(t, store, serviceEntry)
		makePod(t, kube, pod)

		instances := []ServiceInstanceResponse{{
			Hostname:   expectedSvc.Hostname,
			Namestring: expectedSvc.Attributes.Namespace,
			Address:    pod.Status.PodIP,
			Port:       80,
		}}
		expectServiceInstances(t, wc, expectedSvc, 80, instances)

		// when pods become unready, we should see the instances being removed from the registry
		setPodUnready(pod)
		_, err := kube.CoreV1().Pods(pod.Namespace).UpdateStatus(context.TODO(), pod, metav1.UpdateOptions{})
		if err != nil {
			t.Fatal(err)
		}
		expectServiceInstances(t, wc, expectedSvc, 80, []ServiceInstanceResponse{})

		setPodReady(pod)
		_, err = kube.CoreV1().Pods(pod.Namespace).UpdateStatus(context.TODO(), pod, metav1.UpdateOptions{})
		if err != nil {
			t.Fatal(err)
		}
		expectServiceInstances(t, wc, expectedSvc, 80, instances)
	})

	t.Run("ServiceEntry selects Pod with targetPort number", func(t *testing.T) {
		_, wc, store, kube, _ := setupTest(t)
		makeIstioObject(t, store, config.Config{
			Meta: config.Meta{
				Name:             "service-entry",
				Namespace:        namespace,
				GroupVersionKind: gvk.ServiceEntry,
				Domain:           "cluster.local",
			},
			Spec: &networking.ServiceEntry{
				Hosts: []string{"service.namespace.svc.cluster.local"},
				Ports: []*networking.Port{{
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

		instances := []ServiceInstanceResponse{{
			Hostname:   expectedSvc.Hostname,
			Namestring: expectedSvc.Attributes.Namespace,
			Address:    pod.Status.PodIP,
			Port:       8080,
		}}
		expectServiceInstances(t, wc, expectedSvc, 80, instances)
	})

	t.Run("All directions", func(t *testing.T) {
		kc, wc, store, kube, _ := setupTest(t)
		makeService(t, kube, service)
		makeIstioObject(t, store, serviceEntry)

		makePod(t, kube, pod)
		createEndpoints(t, kube, service.Name, namespace, []v1.EndpointPort{{Name: "http", Port: 80}}, []string{pod.Status.PodIP})
		makeIstioObject(t, store, workloadEntry)

		instances := []ServiceInstanceResponse{
			{
				Hostname:   expectedSvc.Hostname,
				Namestring: expectedSvc.Attributes.Namespace,
				Address:    pod.Status.PodIP,
				Port:       80,
			},
			{
				Hostname:   expectedSvc.Hostname,
				Namestring: expectedSvc.Attributes.Namespace,
				Address:    workloadEntry.Spec.(*networking.WorkloadEntry).Address,
				Port:       80,
			},
		}

		expectServiceInstances(t, wc, expectedSvc, 80, instances)
		expectServiceInstances(t, kc, expectedSvc, 80, instances)
	})

	t.Run("All directions with deletion", func(t *testing.T) {
		kc, wc, store, kube, _ := setupTest(t)
		makeService(t, kube, service)
		makeIstioObject(t, store, serviceEntry)

		makePod(t, kube, pod)
		createEndpoints(t, kube, service.Name, namespace, []v1.EndpointPort{{Name: "http", Port: 80}}, []string{pod.Status.PodIP})
		makeIstioObject(t, store, workloadEntry)

		instances := []ServiceInstanceResponse{
			{
				Hostname:   expectedSvc.Hostname,
				Namestring: expectedSvc.Attributes.Namespace,
				Address:    pod.Status.PodIP,
				Port:       80,
			},
			{
				Hostname:   expectedSvc.Hostname,
				Namestring: expectedSvc.Attributes.Namespace,
				Address:    workloadEntry.Spec.(*networking.WorkloadEntry).Address,
				Port:       80,
			},
		}
		expectServiceInstances(t, wc, expectedSvc, 80, instances)
		expectServiceInstances(t, kc, expectedSvc, 80, instances)

		_ = kube.CoreV1().Pods(pod.Namespace).Delete(context.TODO(), pod.Name, metav1.DeleteOptions{})
		_ = kube.CoreV1().Endpoints(pod.Namespace).Delete(context.TODO(), "service", metav1.DeleteOptions{})
		_ = store.Delete(gvk.WorkloadEntry, workloadEntry.Name, workloadEntry.Namespace, nil)
		expectServiceInstances(t, wc, expectedSvc, 80, []ServiceInstanceResponse{})
		expectServiceInstances(t, kc, expectedSvc, 80, []ServiceInstanceResponse{})
	})

	t.Run("Service selects WorkloadEntry: update service", func(t *testing.T) {
		s := xds.NewFakeDiscoveryServer(t, xds.FakeOptions{})
		makeService(t, s.KubeClient(), service)
		makeIstioObject(t, s.Store(), workloadEntry)
		expectEndpoints(t, s, "outbound|80||service.namespace.svc.cluster.local", []string{"2.3.4.5:80"})

		newSvc := service.DeepCopy()
		newSvc.Spec.Ports[0].Port = 8080
		makeService(t, s.KubeClient(), newSvc)
		expectEndpoints(t, s, "outbound|80||service.namespace.svc.cluster.local", nil)
		expectEndpoints(t, s, "outbound|8080||service.namespace.svc.cluster.local", []string{"2.3.4.5:8080"})

		newSvc.Spec.Ports[0].TargetPort = intstr.IntOrString{IntVal: 9090}
		makeService(t, s.KubeClient(), newSvc)
		expectEndpoints(t, s, "outbound|80||service.namespace.svc.cluster.local", nil)
		expectEndpoints(t, s, "outbound|8080||service.namespace.svc.cluster.local", []string{"2.3.4.5:9090"})

		if err := s.KubeClient().CoreV1().Services(newSvc.Namespace).Delete(context.Background(), newSvc.Name, metav1.DeleteOptions{}); err != nil {
			t.Fatal(err)
		}
		expectEndpoints(t, s, "outbound|8080||service.namespace.svc.cluster.local", nil)
	})

	t.Run("Service selects WorkloadEntry: update workloadEntry", func(t *testing.T) {
		s := xds.NewFakeDiscoveryServer(t, xds.FakeOptions{})
		makeService(t, s.KubeClient(), service)
		makeIstioObject(t, s.Store(), workloadEntry)
		expectEndpoints(t, s, "outbound|80||service.namespace.svc.cluster.local", []string{"2.3.4.5:80"})

		newWE := workloadEntry.DeepCopy()
		newWE.Spec.(*networking.WorkloadEntry).Address = "3.4.5.6"
		makeIstioObject(t, s.Store(), newWE)
		expectEndpoints(t, s, "outbound|80||service.namespace.svc.cluster.local", []string{"3.4.5.6:80"})

		if err := s.Store().Delete(gvk.WorkloadEntry, newWE.Name, newWE.Namespace, nil); err != nil {
			t.Fatal(err)
		}
		expectEndpoints(t, s, "outbound|80||service.namespace.svc.cluster.local", nil)
	})

	t.Run("ServiceEntry selects Pod: update service entry", func(t *testing.T) {
		s := xds.NewFakeDiscoveryServer(t, xds.FakeOptions{})
		makeIstioObject(t, s.Store(), serviceEntry)
		makePod(t, s.KubeClient(), pod)
		expectEndpoints(t, s, "outbound|80||service.namespace.svc.cluster.local", []string{"1.2.3.4:80"})

		newSE := serviceEntry.DeepCopy()
		newSE.Spec.(*networking.ServiceEntry).Ports = []*networking.Port{{
			Name:       "http",
			Number:     80,
			Protocol:   "http",
			TargetPort: 8080,
		}}
		makeIstioObject(t, s.Store(), newSE)
		expectEndpoints(t, s, "outbound|80||service.namespace.svc.cluster.local", []string{"1.2.3.4:8080"})

		newSE = newSE.DeepCopy()
		newSE.Spec.(*networking.ServiceEntry).Ports = []*networking.Port{{
			Name:       "http",
			Number:     9090,
			Protocol:   "http",
			TargetPort: 9091,
		}}
		makeIstioObject(t, s.Store(), newSE)
		expectEndpoints(t, s, "outbound|80||service.namespace.svc.cluster.local", nil)
		expectEndpoints(t, s, "outbound|9090||service.namespace.svc.cluster.local", []string{"1.2.3.4:9091"})

		if err := s.Store().Delete(gvk.ServiceEntry, newSE.Name, newSE.Namespace, nil); err != nil {
			t.Fatal(err)
		}
		expectEndpoints(t, s, "outbound|80||service.namespace.svc.cluster.local", nil)
		expectEndpoints(t, s, "outbound|9090||service.namespace.svc.cluster.local", nil)
	})

	t.Run("ServiceEntry selects Pod: update pod", func(t *testing.T) {
		s := xds.NewFakeDiscoveryServer(t, xds.FakeOptions{})
		makeIstioObject(t, s.Store(), serviceEntry)
		makePod(t, s.KubeClient(), pod)
		expectEndpoints(t, s, "outbound|80||service.namespace.svc.cluster.local", []string{"1.2.3.4:80"})

		newPod := pod.DeepCopy()
		newPod.Status.PodIP = "2.3.4.5"
		makePod(t, s.KubeClient(), newPod)
		expectEndpoints(t, s, "outbound|80||service.namespace.svc.cluster.local", []string{"2.3.4.5:80"})

		if err := s.KubeClient().CoreV1().Pods(newPod.Namespace).Delete(context.Background(), newPod.Name, metav1.DeleteOptions{}); err != nil {
			t.Fatal(err)
		}
		expectEndpoints(t, s, "outbound|80||service.namespace.svc.cluster.local", nil)
	})

	t.Run("ServiceEntry selects Pod: deleting pod", func(t *testing.T) {
		s := xds.NewFakeDiscoveryServer(t, xds.FakeOptions{})
		makeIstioObject(t, s.Store(), serviceEntry)
		makePod(t, s.KubeClient(), pod)
		expectEndpoints(t, s, "outbound|80||service.namespace.svc.cluster.local", []string{"1.2.3.4:80"})

		// Simulate pod being deleted by setting deletion timestamp
		newPod := pod.DeepCopy()
		newPod.DeletionTimestamp = &metav1.Time{Time: time.Now()}
		makePod(t, s.KubeClient(), newPod)
		expectEndpoints(t, s, "outbound|80||service.namespace.svc.cluster.local", nil)

		if err := s.KubeClient().CoreV1().Pods(newPod.Namespace).Delete(context.Background(), newPod.Name, metav1.DeleteOptions{}); err != nil {
			t.Fatal(err)
		}
		expectEndpoints(t, s, "outbound|80||service.namespace.svc.cluster.local", nil)
	})

	t.Run("Service selects WorkloadEntry: health status", func(t *testing.T) {
		kc, _, store, kube, _ := setupTest(t)
		makeService(t, kube, service)

		// Start as unhealthy, should have no instances
		makeIstioObject(t, store, setHealth(workloadEntry, false))
		instances := []ServiceInstanceResponse{}
		expectServiceInstances(t, kc, expectedSvc, 80, instances)

		// Mark healthy, get instances
		makeIstioObject(t, store, setHealth(workloadEntry, true))
		instances = []ServiceInstanceResponse{{
			Hostname:   expectedSvc.Hostname,
			Namestring: expectedSvc.Attributes.Namespace,
			Address:    workloadEntry.Spec.(*networking.WorkloadEntry).Address,
			Port:       80,
		}}
		expectServiceInstances(t, kc, expectedSvc, 80, instances)

		// Set back to unhealthy
		makeIstioObject(t, store, setHealth(workloadEntry, false))
		instances = []ServiceInstanceResponse{}
		expectServiceInstances(t, kc, expectedSvc, 80, instances)

		// Remove health status entirely
		makeIstioObject(t, store, workloadEntry)
		instances = []ServiceInstanceResponse{{
			Hostname:   expectedSvc.Hostname,
			Namestring: expectedSvc.Attributes.Namespace,
			Address:    workloadEntry.Spec.(*networking.WorkloadEntry).Address,
			Port:       80,
		}}
		expectServiceInstances(t, kc, expectedSvc, 80, instances)
	})
}

func setHealth(cfg config.Config, healthy bool) config.Config {
	cfg = cfg.DeepCopy()
	if cfg.Annotations == nil {
		cfg.Annotations = map[string]string{}
	}
	cfg.Annotations[status.WorkloadEntryHealthCheckAnnotation] = "true"
	if healthy {
		return status.UpdateConfigCondition(cfg, &v1alpha1.IstioCondition{
			Type:   status.ConditionHealthy,
			Status: status.StatusTrue,
		})
	}
	return status.UpdateConfigCondition(cfg, &v1alpha1.IstioCondition{
		Type:   status.ConditionHealthy,
		Status: status.StatusFalse,
	})
}

func waitForEdsUpdate(t *testing.T, xdsUpdater *xds.FakeXdsUpdater, expected int) {
	t.Helper()
	retry.UntilSuccessOrFail(t, func() error {
		event := xdsUpdater.WaitOrFail(t, "eds", "edscache")
		if event.Endpoints != expected {
			return fmt.Errorf("expecting %d endpoints, but got %d", expected, event.Endpoints)
		}
		return nil
	}, retry.Delay(time.Millisecond*10), retry.Timeout(time.Second))
}

func TestEndpointsDeduping(t *testing.T) {
	s := xds.NewFakeDiscoveryServer(t, xds.FakeOptions{
		KubernetesEndpointMode: kubecontroller.EndpointSliceOnly,
	})
	namespace := "namespace"
	labels := map[string]string{
		"app": "bar",
	}
	makeService(t, s.KubeClient(), &v1.Service{
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
	createEndpointSlice(t, s.KubeClient(), "slice1", "service", namespace, []v1.EndpointPort{{Name: "http", Port: 80}}, []string{"1.2.3.4"})
	expectEndpoints(t, s, "outbound|80||service.namespace.svc.cluster.local", []string{"1.2.3.4:80"})

	// create an FQDN endpoint that should be ignored
	createEndpointSliceWithType(t, s.KubeClient(), "slice1", "service",
		namespace, []v1.EndpointPort{{Name: "http", Port: 80}}, []string{"foo.com"}, discovery.AddressTypeFQDN)
	expectEndpoints(t, s, "outbound|80||service.namespace.svc.cluster.local", []string{"1.2.3.4:80"})

	// Add another port endpoint
	createEndpointSlice(t, s.KubeClient(), "slice1", "service", namespace,
		[]v1.EndpointPort{{Name: "http-other", Port: 90}, {Name: "http", Port: 80}}, []string{"1.2.3.4", "2.3.4.5"})
	expectEndpoints(t, s, "outbound|80||service.namespace.svc.cluster.local", []string{"1.2.3.4:80", "2.3.4.5:80"})
	expectEndpoints(t, s, "outbound|90||service.namespace.svc.cluster.local", []string{"1.2.3.4:90", "2.3.4.5:90"})

	// Move the endpoint to another slice - transition phase where its duplicated
	createEndpointSlice(t, s.KubeClient(), "slice1", "service", namespace, []v1.EndpointPort{{Name: "http", Port: 80}}, []string{"1.2.3.5", "2.3.4.5"})
	createEndpointSlice(t, s.KubeClient(), "slice2", "service", namespace, []v1.EndpointPort{{Name: "http", Port: 80}}, []string{"2.3.4.5"})
	expectEndpoints(t, s, "outbound|80||service.namespace.svc.cluster.local", []string{"1.2.3.5:80", "2.3.4.5:80"})

	// Move the endpoint to another slice - completed
	createEndpointSlice(t, s.KubeClient(), "slice1", "service", namespace, []v1.EndpointPort{{Name: "http", Port: 80}}, []string{"1.2.3.4"})
	createEndpointSlice(t, s.KubeClient(), "slice2", "service", namespace, []v1.EndpointPort{{Name: "http", Port: 80}}, []string{"2.3.4.5"})
	expectEndpoints(t, s, "outbound|80||service.namespace.svc.cluster.local", []string{"1.2.3.4:80", "2.3.4.5:80"})

	// Delete endpoint
	createEndpointSlice(t, s.KubeClient(), "slice1", "service", namespace, []v1.EndpointPort{{Name: "http", Port: 80}}, []string{"1.2.3.4"})
	createEndpointSlice(t, s.KubeClient(), "slice2", "service", namespace, []v1.EndpointPort{{Name: "http", Port: 80}}, []string{})
	expectEndpoints(t, s, "outbound|80||service.namespace.svc.cluster.local", []string{"1.2.3.4:80"})

	_ = s.KubeClient().DiscoveryV1().EndpointSlices(namespace).Delete(context.TODO(), "slice1", metav1.DeleteOptions{})
	expectEndpoints(t, s, "outbound|80||service.namespace.svc.cluster.local", nil)

	// Ensure there is nothing is left over
	expectServiceInstances(t, s.KubeRegistry, &model.Service{
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
	}, 80, []ServiceInstanceResponse{})
}

// TestEndpointSlicingServiceUpdate is a regression test to ensure we do not end up with duplicate endpoints when a service changes.
func TestEndpointSlicingServiceUpdate(t *testing.T) {
	for _, version := range []string{"latest", "20"} {
		t.Run("kuberentes 1."+version, func(t *testing.T) {
			s := xds.NewFakeDiscoveryServer(t, xds.FakeOptions{
				KubernetesEndpointMode: kubecontroller.EndpointSliceOnly,
				KubernetesVersion:      version,
				EnableFakeXDSUpdater:   true,
			})
			namespace := "namespace"
			labels := map[string]string{
				"app": "bar",
			}
			makeService(t, s.KubeClient(), &v1.Service{
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
			xdsUpdater := s.XdsUpdater.(*xds.FakeXdsUpdater)
			createEndpointSlice(t, s.KubeClient(), "slice1", "service", namespace, []v1.EndpointPort{{Name: "http", Port: 80}}, []string{"1.2.3.4"})
			createEndpointSlice(t, s.KubeClient(), "slice2", "service", namespace, []v1.EndpointPort{{Name: "http", Port: 80}}, []string{"1.2.3.4"})
			expectEndpoints(t, s, "outbound|80||service.namespace.svc.cluster.local", []string{"1.2.3.4:80"})
			xdsUpdater.WaitOrFail(t, "svcupdate")

			// Trigger a service updates
			makeService(t, s.KubeClient(), &v1.Service{
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
			xdsUpdater.WaitOrFail(t, "svcupdate")
			expectEndpoints(t, s, "outbound|80||service.namespace.svc.cluster.local", []string{"1.2.3.4:80"})
		})
	}
}

func TestSameIPEndpointSlicing(t *testing.T) {
	s := xds.NewFakeDiscoveryServer(t, xds.FakeOptions{
		KubernetesEndpointMode: kubecontroller.EndpointSliceOnly,
		EnableFakeXDSUpdater:   true,
	})
	namespace := "namespace"
	labels := map[string]string{
		"app": "bar",
	}
	makeService(t, s.KubeClient(), &v1.Service{
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
	xdsUpdater := s.XdsUpdater.(*xds.FakeXdsUpdater)

	// Delete endpoints with same IP
	createEndpointSlice(t, s.KubeClient(), "slice1", "service", namespace, []v1.EndpointPort{{Name: "http", Port: 80}}, []string{"1.2.3.4"})
	createEndpointSlice(t, s.KubeClient(), "slice2", "service", namespace, []v1.EndpointPort{{Name: "http", Port: 80}}, []string{"1.2.3.4"})
	expectEndpoints(t, s, "outbound|80||service.namespace.svc.cluster.local", []string{"1.2.3.4:80"})

	// delete slice 1, it should still exist
	_ = s.KubeClient().DiscoveryV1().EndpointSlices(namespace).Delete(context.TODO(), "slice1", metav1.DeleteOptions{})
	xdsUpdater.WaitOrFail(t, "eds")
	expectEndpoints(t, s, "outbound|80||service.namespace.svc.cluster.local", []string{"1.2.3.4:80"})
	_ = s.KubeClient().DiscoveryV1().EndpointSlices(namespace).Delete(context.TODO(), "slice2", metav1.DeleteOptions{})
	xdsUpdater.WaitOrFail(t, "eds")
	expectEndpoints(t, s, "outbound|80||service.namespace.svc.cluster.local", nil)
}

type ServiceInstanceResponse struct {
	Hostname   host.Name
	Namestring string
	Address    string
	Port       uint32
}

func expectEndpoints(t *testing.T, s *xds.FakeDiscoveryServer, cluster string, expected []string) {
	t.Helper()
	retry.UntilSuccessOrFail(t, func() error {
		got := xdstest.ExtractLoadAssignments(s.Endpoints(s.SetupProxy(nil)))
		sort.Strings(got[cluster])
		sort.Strings(expected)
		if !reflect.DeepEqual(got[cluster], expected) {
			return fmt.Errorf("wanted %v got %v. All endpoints: %+v", expected, got[cluster], got)
		}
		return nil
	}, retry.Converge(2), retry.Timeout(time.Second*2), retry.Delay(time.Millisecond*10))
}

// nolint: unparam
func expectServiceInstances(t *testing.T, sd serviceregistry.Instance, svc *model.Service, port int, expected []ServiceInstanceResponse) {
	t.Helper()
	svc.Attributes.ServiceRegistry = sd.Provider()
	// The system is eventually consistent, so add some retries
	retry.UntilSuccessOrFail(t, func() error {
		instances := sd.InstancesByPort(svc, port, nil)
		sortServiceInstances(instances)
		got := []ServiceInstanceResponse{}
		for _, i := range instances {
			got = append(got, ServiceInstanceResponse{
				Hostname:   i.Service.Hostname,
				Namestring: i.Service.Attributes.Namespace,
				Address:    i.Endpoint.Address,
				Port:       i.Endpoint.EndpointPort,
			})
		}
		if err := compare(t, got, expected); err != nil {
			return fmt.Errorf("%v", err)
		}
		return nil
	}, retry.Converge(2), retry.Timeout(time.Second*2), retry.Delay(time.Millisecond*10))
}

func compare(t *testing.T, actual, expected interface{}) error {
	return util.Compare(jsonBytes(t, actual), jsonBytes(t, expected))
}

func sortServiceInstances(instances []*model.ServiceInstance) {
	sort.Slice(instances, func(i, j int) bool {
		if instances[i].Service.Hostname == instances[j].Service.Hostname {
			return instances[i].Endpoint.Address < instances[j].Endpoint.Address
		}
		return instances[i].Service.Hostname < instances[j].Service.Hostname
	})
}

func jsonBytes(t *testing.T, v interface{}) []byte {
	data, err := json.MarshalIndent(v, "", " ")
	if err != nil {
		t.Fatal(t)
	}
	return data
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

// nolint: unparam
func createEndpointSlice(t *testing.T, c kubernetes.Interface, name, serviceName, namespace string, ports []v1.EndpointPort, addrs []string) {
	createEndpointSliceWithType(t, c, name, serviceName, namespace, ports, addrs, discovery.AddressTypeIPv4)
}

// nolint: unparam
func createEndpointSliceWithType(t *testing.T, c kubernetes.Interface, name, serviceName, namespace string,
	ports []v1.EndpointPort, ips []string, addrType discovery.AddressType) {
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
