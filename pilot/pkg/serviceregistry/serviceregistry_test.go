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
	"sort"
	"testing"
	"time"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/kubernetes"

	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/config/memory"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/serviceregistry"
	kubecontroller "istio.io/istio/pilot/pkg/serviceregistry/kube/controller"
	"istio.io/istio/pilot/pkg/serviceregistry/serviceentry"
	"istio.io/istio/pilot/test/util"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/host"
	"istio.io/istio/pkg/config/schema/collections"
	"istio.io/istio/pkg/config/schema/gvk"
	kubeclient "istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/test/util/retry"
)

type Event struct {
	kind      string
	host      string
	namespace string
	endpoints int
	pushReq   *model.PushRequest
}

type FakeXdsUpdater struct {
	// Events tracks notifications received by the updater
	Events chan Event
}

var _ model.XDSUpdater = &FakeXdsUpdater{}

func (fx *FakeXdsUpdater) EDSUpdate(_, hostname string, namespace string, entry []*model.IstioEndpoint) {
	fx.Events <- Event{kind: "eds", host: hostname, namespace: namespace, endpoints: len(entry)}
}

func (fx *FakeXdsUpdater) EDSCacheUpdate(_, hostname string, namespace string, entry []*model.IstioEndpoint) {
	fx.Events <- Event{kind: "edscache", host: hostname, namespace: namespace, endpoints: len(entry)}
}

func (fx *FakeXdsUpdater) ConfigUpdate(req *model.PushRequest) {
	fx.Events <- Event{kind: "xds", pushReq: req}
}

func (fx *FakeXdsUpdater) ProxyUpdate(_, _ string) {
}

func (fx *FakeXdsUpdater) SvcUpdate(_, hostname string, namespace string, _ model.Event) {
	fx.Events <- Event{kind: "svcupdate", host: hostname, namespace: namespace}
}

func (fx *FakeXdsUpdater) Wait(et string) *Event {
	for {
		select {
		case e := <-fx.Events:
			if e.kind == et {
				return &e
			}
			continue
		case <-time.After(5 * time.Second):
			return nil
		}
	}
}

func setupTest(t *testing.T) (
	*kubecontroller.Controller,
	*serviceentry.ServiceEntryStore,
	model.ConfigStoreCache,
	kubernetes.Interface,
	*FakeXdsUpdater) {
	t.Helper()
	client := kubeclient.NewFakeClient()

	eventch := make(chan Event, 100)

	xdsUpdater := &FakeXdsUpdater{
		Events: eventch,
	}
	kc := kubecontroller.NewController(client, kubecontroller.Options{XDSUpdater: xdsUpdater, DomainSuffix: "cluster.local"})
	configController := memory.NewController(memory.Make(collections.Pilot))

	stop := make(chan struct{})
	t.Cleanup(func() {
		close(stop)
	})
	go configController.Run(stop)

	istioStore := model.MakeIstioStore(configController)
	wc := serviceentry.NewServiceDiscovery(configController, istioStore, xdsUpdater)
	client.RunAndWait(stop)

	kc.AppendWorkloadHandler(wc.WorkloadInstanceHandler)
	wc.AppendWorkloadHandler(kc.WorkloadInstanceHandler)

	go kc.Run(stop)
	go wc.Run(stop)

	return kc, wc, configController, client.Kube(), xdsUpdater
}

// TestWorkloadInstances is effectively an integration test of composing the Kubernetes service registry with the
// external service registry, which have cross-references by workload instances.
func TestWorkloadInstances(t *testing.T) {
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
				Labels: labels},
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
		}},
		Attributes: model.ServiceAttributes{
			Namespace: namespace,
			Name:      "service",
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

	t.Run("Kubernetes only: endpoint occur earlier", func(t *testing.T) {
		kc, _, _, kube, xdsUpdater := setupTest(t)
		makePod(t, kube, pod)
		createEndpoints(t, kube, service.Name, namespace, []v1.EndpointPort{{Name: "http", Port: 80}}, []string{pod.Status.PodIP})

		// make service populated later than endpoint
		makeService(t, kube, service)

		event := xdsUpdater.Wait("edscache")
		if event == nil {
			t.Fatalf("expecting edscache event")
		}
		if event.endpoints != 1 {
			t.Errorf("expecting 1 endpoints, but got %d ", event.endpoints)
		}

		instances := []ServiceInstanceResponse{{
			Hostname:   expectedSvc.Hostname,
			Namestring: expectedSvc.Attributes.Namespace,
			Address:    pod.Status.PodIP,
			Port:       80,
		}}
		expectServiceInstances(t, kc, expectedSvc, 80, instances)
	})

	t.Run("External only", func(t *testing.T) {
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

	t.Run("External only with named port override", func(t *testing.T) {
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

	t.Run("External only with target port", func(t *testing.T) {
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

		makeService(t, kube, service)

		event := xdsUpdater.Wait("edscache")
		if event == nil {
			t.Fatalf("expecting edscache event")
		}
		if event.endpoints != 1 {
			t.Errorf("expecting 1 endpoints, but got %d ", event.endpoints)
		}

		instances := []ServiceInstanceResponse{{
			Hostname:   expectedSvc.Hostname,
			Namestring: expectedSvc.Attributes.Namespace,
			Address:    workloadEntry.Spec.(*networking.WorkloadEntry).Address,
			Port:       80,
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
					TargetPort: intstr.FromInt(8080),
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
		_ = store.Delete(gvk.WorkloadEntry, workloadEntry.Name, workloadEntry.Namespace)
		expectServiceInstances(t, wc, expectedSvc, 80, []ServiceInstanceResponse{})
		expectServiceInstances(t, kc, expectedSvc, 80, []ServiceInstanceResponse{})
	})
}

type ServiceInstanceResponse struct {
	Hostname   host.Name
	Namestring string
	Address    string
	Port       uint32
}

// nolint: unparam
func expectServiceInstances(t *testing.T, sd serviceregistry.Instance, svc *model.Service, port int, expected []ServiceInstanceResponse) {
	t.Helper()
	svc.Attributes.ServiceRegistry = string(sd.Provider())
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
	}, retry.Converge(2), retry.Timeout(time.Second*2))
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

func makePod(t *testing.T, c kubernetes.Interface, pod *v1.Pod) {
	t.Helper()
	newPod, err := c.CoreV1().Pods(pod.Namespace).Create(context.Background(), pod, metav1.CreateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	// Apiserver doesn't allow Create/Update to modify the pod status. Creating doesn't result in
	// events - since PodIP will be "".
	newPod.Status.PodIP = pod.Status.PodIP
	newPod.Status.Phase = v1.PodRunning
	_, err = c.CoreV1().Pods(pod.Namespace).UpdateStatus(context.TODO(), newPod, metav1.UpdateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(100 * time.Millisecond)
}

func makeService(t *testing.T, c kubernetes.Interface, svc *v1.Service) {
	t.Helper()
	_, err := c.CoreV1().Services(svc.Namespace).Create(context.Background(), svc, metav1.CreateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(100 * time.Millisecond)
}

func makeIstioObject(t *testing.T, c model.ConfigStore, svc config.Config) {
	t.Helper()
	_, err := c.Create(svc)
	if err != nil {
		t.Fatal(err)
	}
}

func createEndpoints(t *testing.T, c kubernetes.Interface, name, namespace string, ports []v1.EndpointPort, ips []string) {
	eas := make([]v1.EndpointAddress, 0)
	for _, ip := range ips {
		eas = append(eas, v1.EndpointAddress{IP: ip, TargetRef: &v1.ObjectReference{
			Kind:      "Pod",
			Name:      name,
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
	time.Sleep(100 * time.Millisecond)
}
