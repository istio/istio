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
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/kubernetes"

	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/config/memory"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/serviceregistry"
	kubecontroller "istio.io/istio/pilot/pkg/serviceregistry/kube/controller"
	"istio.io/istio/pilot/pkg/serviceregistry/serviceentry"
	"istio.io/istio/pilot/pkg/xds"
	"istio.io/istio/pilot/test/util"
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

func (fx *FakeXdsUpdater) EDSUpdate(_, hostname string, namespace string, entry []*model.IstioEndpoint) error {
	fx.Events <- Event{kind: "eds", host: hostname, namespace: namespace, endpoints: len(entry)}
	return nil
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
	serviceEntry := model.Config{
		ConfigMeta: model.ConfigMeta{
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
		Status: v1.PodStatus{PodIP: "1.2.3.4"},
	}
	workloadEntry := model.Config{
		ConfigMeta: model.ConfigMeta{
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
			Namespace:      namespace,
			Name:           "service",
			LabelSelectors: labels,
		},
	}

	t.Run("Kubernetes only", func(t *testing.T) {
		kc, _, _, kube, _ := setupTest(t)
		makeService(t, kube, service)
		makePod(t, kube, pod)
		createEndpoints(t, kube, "service", namespace, []v1.EndpointPort{{Name: "http", Port: 80}}, []string{pod.Status.PodIP})

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
		makePod(t, kube, pod)
		createEndpoints(t, kube, service.Name, namespace, []v1.EndpointPort{{Name: "http", Port: 80}}, []string{pod.Status.PodIP})
		event := xdsUpdater.Wait("eds")
		if event == nil {
			t.Fatalf("expecting eds event")
		}
		event = xdsUpdater.Wait("xds")
		if event == nil {
			t.Fatalf("expecting xds event")
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
		makeIstioObject(t, store, model.Config{
			ConfigMeta: model.ConfigMeta{
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
		makeIstioObject(t, store, model.Config{
			ConfigMeta: model.ConfigMeta{
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
		makeIstioObject(t, store, model.Config{
			ConfigMeta: model.ConfigMeta{
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
		makeIstioObject(t, store, model.Config{
			ConfigMeta: model.ConfigMeta{
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
		makeIstioObject(t, store, model.Config{
			ConfigMeta: model.ConfigMeta{
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
		createEndpoints(t, kube, "service", namespace, []v1.EndpointPort{{Name: "http", Port: 80}}, []string{pod.Status.PodIP})
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
		createEndpoints(t, kube, "service", namespace, []v1.EndpointPort{{Name: "http", Port: 80}}, []string{pod.Status.PodIP})
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
		if err := store.Delete(gvk.WorkloadEntry, workloadEntry.Name, workloadEntry.Namespace); err != nil {
			t.Fatal(err)
		}
		expectServiceInstances(t, wc, expectedSvc, 80, []ServiceInstanceResponse{})
		expectServiceInstances(t, kc, expectedSvc, 80, []ServiceInstanceResponse{})
	})

	t.Run("Service selects WorkloadEntry: update service", func(t *testing.T) {
		s := xds.NewFakeDiscoveryServer(t, xds.FakeOptions{})
		makeService(t, s.KubeClient, service)
		makeIstioObject(t, s.Store, workloadEntry)
		expectEndpoints(t, s, "outbound|80||service.namespace.svc.cluster.local", []string{"2.3.4.5:80"})

		newSvc := service.DeepCopy()
		newSvc.Spec.Ports[0].Port = 8080
		makeService(t, s.KubeClient, newSvc)
		expectEndpoints(t, s, "outbound|80||service.namespace.svc.cluster.local", nil)
		expectEndpoints(t, s, "outbound|8080||service.namespace.svc.cluster.local", []string{"2.3.4.5:8080"})

		newSvc.Spec.Ports[0].TargetPort = intstr.IntOrString{IntVal: 9090}
		makeService(t, s.KubeClient, newSvc)
		expectEndpoints(t, s, "outbound|80||service.namespace.svc.cluster.local", nil)
		expectEndpoints(t, s, "outbound|8080||service.namespace.svc.cluster.local", []string{"2.3.4.5:9090"})

		if err := s.KubeClient.CoreV1().Services(newSvc.Namespace).Delete(context.Background(), newSvc.Name, metav1.DeleteOptions{}); err != nil {
			t.Fatal(err)
		}
		expectEndpoints(t, s, "outbound|8080||service.namespace.svc.cluster.local", nil)
	})

	t.Run("Service selects WorkloadEntry: update workloadEntry", func(t *testing.T) {
		s := xds.NewFakeDiscoveryServer(t, xds.FakeOptions{})
		makeService(t, s.KubeClient, service)
		makeIstioObject(t, s.Store, workloadEntry)
		expectEndpoints(t, s, "outbound|80||service.namespace.svc.cluster.local", []string{"2.3.4.5:80"})

		newWE := workloadEntry.DeepCopy()
		newWE.Spec.(*networking.WorkloadEntry).Address = "3.4.5.6"
		makeIstioObject(t, s.Store, newWE)
		expectEndpoints(t, s, "outbound|80||service.namespace.svc.cluster.local", []string{"3.4.5.6:80"})

		if err := s.Store.Delete(gvk.WorkloadEntry, newWE.Name, newWE.Namespace); err != nil {
			t.Fatal(err)
		}
		expectEndpoints(t, s, "outbound|80||service.namespace.svc.cluster.local", nil)
	})

	t.Run("ServiceEntry selects Pod: update service entry", func(t *testing.T) {
		s := xds.NewFakeDiscoveryServer(t, xds.FakeOptions{})
		makeIstioObject(t, s.Store, serviceEntry)
		makePod(t, s.KubeClient, pod)
		expectEndpoints(t, s, "outbound|80||service.namespace.svc.cluster.local", []string{"1.2.3.4:80"})

		newSE := serviceEntry.DeepCopy()
		newSE.Spec.(*networking.ServiceEntry).Ports = []*networking.Port{{
			Name:       "http",
			Number:     80,
			Protocol:   "http",
			TargetPort: 8080,
		}}
		makeIstioObject(t, s.Store, newSE)
		expectEndpoints(t, s, "outbound|80||service.namespace.svc.cluster.local", []string{"1.2.3.4:8080"})

		newSE = newSE.DeepCopy()
		newSE.Spec.(*networking.ServiceEntry).Ports = []*networking.Port{{
			Name:       "http",
			Number:     9090,
			Protocol:   "http",
			TargetPort: 9091,
		}}
		makeIstioObject(t, s.Store, newSE)
		expectEndpoints(t, s, "outbound|80||service.namespace.svc.cluster.local", nil)
		expectEndpoints(t, s, "outbound|9090||service.namespace.svc.cluster.local", []string{"1.2.3.4:9091"})

		if err := s.Store.Delete(gvk.ServiceEntry, newSE.Name, newSE.Namespace); err != nil {
			t.Fatal(err)
		}
		expectEndpoints(t, s, "outbound|80||service.namespace.svc.cluster.local", nil)
		expectEndpoints(t, s, "outbound|9090||service.namespace.svc.cluster.local", nil)
	})

	t.Run("ServiceEntry selects Pod: update pod", func(t *testing.T) {
		s := xds.NewFakeDiscoveryServer(t, xds.FakeOptions{})
		makeIstioObject(t, s.Store, serviceEntry)
		makePod(t, s.KubeClient, pod)
		expectEndpoints(t, s, "outbound|80||service.namespace.svc.cluster.local", []string{"1.2.3.4:80"})

		newPod := pod.DeepCopy()
		newPod.Status.PodIP = "2.3.4.5"
		makePod(t, s.KubeClient, newPod)
		expectEndpoints(t, s, "outbound|80||service.namespace.svc.cluster.local", []string{"2.3.4.5:80"})

		if err := s.KubeClient.CoreV1().Pods(newPod.Namespace).Delete(context.Background(), newPod.Name, metav1.DeleteOptions{}); err != nil {
			t.Fatal(err)
		}
		expectEndpoints(t, s, "outbound|80||service.namespace.svc.cluster.local", nil)
	})
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
		got := xds.ExtractEndpoints(s.Endpoints(s.SetupProxy(nil)))
		if !reflect.DeepEqual(got[cluster], expected) {
			return fmt.Errorf("wanted %v got %v. All endpoints: %+v", expected, got[cluster], got)
		}
		return nil
	}, retry.Converge(2), retry.Timeout(time.Second*2))
}

// nolint: unparam
func expectServiceInstances(t *testing.T, sd serviceregistry.Instance, svc *model.Service, port int, expected []ServiceInstanceResponse) {
	t.Helper()
	svc.Attributes.ServiceRegistry = string(sd.Provider())
	// The system is eventually consistent, so add some retries
	retry.UntilSuccessOrFail(t, func() error {
		instances, err := sd.InstancesByPort(svc, port, nil)
		if err != nil {
			return fmt.Errorf("instancesByPort() encountered unexpected error: %v", err)
		}
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
	_, err = c.CoreV1().Pods(pod.Namespace).UpdateStatus(context.TODO(), newPod, metav1.UpdateOptions{})
	if err != nil {
		t.Fatal(err)
	}
}

func makeService(t *testing.T, c kubernetes.Interface, svc *v1.Service) {
	t.Helper()
	_, err := c.CoreV1().Services(svc.Namespace).Create(context.Background(), svc, metav1.CreateOptions{})
	if kerrors.IsAlreadyExists(err) {
		_, err = c.CoreV1().Services(svc.Namespace).Update(context.Background(), svc, metav1.UpdateOptions{})
	}
	if err != nil {
		t.Fatal(err)
	}
}

func makeIstioObject(t *testing.T, c model.ConfigStore, svc model.Config) {
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

}
