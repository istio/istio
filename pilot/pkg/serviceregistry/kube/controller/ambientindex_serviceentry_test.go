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
	"context"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	meshconfig "istio.io/api/mesh/v1alpha1"
	"istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/config/memory"
	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/mesh"
	"istio.io/istio/pkg/config/schema/collections"
	"istio.io/istio/pkg/config/schema/gvk"
	"istio.io/istio/pkg/kube/kclient/clienttest"
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/util/assert"
	"istio.io/istio/pkg/workloadapi"
)

func TestAmbientIndex_ServiceEntry(t *testing.T) {
	test.SetForTest(t, &features.EnableAmbientControllers, true)
	cfg := memory.NewSyncController(memory.MakeSkipValidation(collections.PilotGatewayAPI()))
	controller, fx := NewFakeControllerWithOptions(t, FakeControllerOptions{
		ConfigController:     cfg,
		MeshWatcher:          mesh.NewFixedWatcher(&meshconfig.MeshConfig{RootNamespace: "istio-system"}),
		ClusterID:            "cluster0",
		WorkloadEntryEnabled: true,
	})
	controller.opts.MeshServiceController.AppendServiceHandler(controller.ServiceEntryHandler)
	controller.network = "testnetwork"
	pc := clienttest.Wrap(t, controller.podsClient)
	cfg.RegisterEventHandler(gvk.AuthorizationPolicy, controller.AuthorizationPolicyHandler)
	cfg.RegisterEventHandler(gvk.WorkloadEntry, controller.WorkloadEntryHandler)
	go cfg.Run(test.NewStop(t))

	addWorkloadEntries := func(ip string, name, sa string, labels map[string]string) {
		t.Helper()

		controller.client.Kube().CoreV1().Namespaces().Create(context.Background(), &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{Name: "ns1", Labels: map[string]string{"istio.io/dataplane-mode": "ambient"}},
		}, metav1.CreateOptions{})

		wkEntry := generateWorkloadEntry(ip, name, "ns1", sa, labels, nil)

		w := config.Config{
			Meta: config.Meta{
				GroupVersionKind: gvk.WorkloadEntry,
				Name:             wkEntry.GetObjectMeta().GetName(),
				Namespace:        wkEntry.GetObjectMeta().GetNamespace(),
				Labels:           wkEntry.GetObjectMeta().GetLabels(),
			},
			Spec: wkEntry.Spec.DeepCopy(),
		}
		_, err := cfg.Create(w)
		if err != nil && strings.Contains(err.Error(), "item already exists") {
			_, err = cfg.Update(w)
		}
		if err != nil {
			t.Fatal(err)
		}
	}

	addServiceEntry := func(hosts, addresses []string, name, ns string, labels map[string]string) {
		t.Helper()

		controller.client.Kube().CoreV1().Namespaces().Create(context.Background(), &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{Name: ns, Labels: map[string]string{"istio.io/dataplane-mode": "ambient"}},
		}, metav1.CreateOptions{})

		serviceEntry := generateServiceEntry(hosts, addresses, labels)
		svc := &model.Service{
			Attributes: model.ServiceAttributes{
				ServiceEntryName:      name,
				ServiceEntryNamespace: ns,
				ServiceEntry:          serviceEntry,
			},
		}
		controller.ambientIndex.handleServiceEntry(svc, model.EventAdd)
	}

	deleteServiceEntry := func(hosts, addresses []string, name, ns string, labels map[string]string) {
		t.Helper()
		serviceEntry := generateServiceEntry(hosts, addresses, labels)
		svc := &model.Service{
			Attributes: model.ServiceAttributes{
				ServiceEntryName:      name,
				ServiceEntryNamespace: ns,
				ServiceEntry:          serviceEntry,
			},
		}
		controller.ambientIndex.handleServiceEntry(svc, model.EventDelete)
	}

	// test code path where service entry creates a workload entry via `ServiceEntry.endpoints`
	// and the inlined WE has a port override
	addServiceEntry([]string{"se.istio.io"}, []string{"240.240.23.45"}, "name1", "ns", nil)
	assertWorkloads(t, controller, "", workloadapi.WorkloadStatus_HEALTHY, "name1")
	assertEvent(t, fx, "cluster0/networking.istio.io/ServiceEntry/ns/name1/127.0.0.1", "ns/")
	assert.Equal(t, len(controller.ambientIndex.byWorkloadEntry), 1)
	assert.Equal(t, controller.ambientIndex.Lookup("testnetwork/127.0.0.1"), []*model.AddressInfo{{
		Address: &workloadapi.Address{
			Type: &workloadapi.Address_Workload{
				Workload: &workloadapi.Workload{
					Uid:               "cluster0/networking.istio.io/ServiceEntry/ns/name1/127.0.0.1",
					Name:              "name1",
					Namespace:         "ns",
					Addresses:         [][]byte{parseIP("127.0.0.1")},
					Node:              "",
					Network:           "testnetwork",
					CanonicalName:     "name1",
					CanonicalRevision: "latest",
					WorkloadType:      workloadapi.WorkloadType_POD,
					WorkloadName:      "name1",
					VirtualIps: map[string]*workloadapi.PortList{
						"testnetwork/240.240.23.45": {
							Ports: []*workloadapi.Port{
								{
									ServicePort: 80,
									TargetPort:  8081, // port is overidden by inlined WE port
								},
							},
						},
					},
				},
			},
		},
	}})

	deleteServiceEntry([]string{"se.istio.io"}, []string{"240.240.23.45"}, "name1", "ns", nil)
	assert.Equal(t, len(controller.ambientIndex.byWorkloadEntry), 0)
	assert.Equal(t, controller.ambientIndex.Lookup("testnetwork/127.0.0.1"), nil)
	fx.Clear()

	// test code path where service entry selects workloads via `ServiceEntry.workloadSelector`
	addPod(t, pc, "140.140.0.10", "pod1", "sa1", map[string]string{"app": "a"}, nil)
	assertEvent(t, fx, "cluster0//Pod/ns1/pod1")
	addPod(t, pc, "140.140.0.11", "pod2", "sa1", map[string]string{"app": "other"}, nil)
	assertEvent(t, fx, "cluster0//Pod/ns1/pod2")
	assertWorkloads(t, controller, "", workloadapi.WorkloadStatus_HEALTHY, "pod1", "pod2")
	addWorkloadEntries("240.240.34.56", "name1", "sa1", map[string]string{"app": "a"})
	assertWorkloads(t, controller, "", workloadapi.WorkloadStatus_HEALTHY, "pod1", "pod2", "name1")
	assertEvent(t, fx, "cluster0/networking.istio.io/WorkloadEntry/ns1/name1")

	addServiceEntry([]string{"se.istio.io"}, []string{"240.240.23.45"}, "name1", "ns", map[string]string{"app": "a"})
	assertWorkloads(t, controller, "", workloadapi.WorkloadStatus_HEALTHY, "pod1", "pod2", "name1")
	// we should see an update for the workloads selected by the service entry
	assertEvent(t, fx, "cluster0//Pod/ns1/pod1", "cluster0//Pod/ns1/pod2", "cluster0/networking.istio.io/WorkloadEntry/ns1/name1", "ns/")

	assert.Equal(t, controller.ambientIndex.Lookup("testnetwork/140.140.0.10"), []*model.AddressInfo{{
		Address: &workloadapi.Address{
			Type: &workloadapi.Address_Workload{
				Workload: &workloadapi.Workload{
					Uid:               "cluster0//Pod/ns1/pod1",
					Name:              "pod1",
					Namespace:         "ns1",
					Addresses:         [][]byte{parseIP("140.140.0.10")},
					Node:              "node1",
					Network:           "testnetwork",
					ClusterId:         "cluster0",
					CanonicalName:     "a",
					CanonicalRevision: "latest",
					ServiceAccount:    "sa1",
					WorkloadType:      workloadapi.WorkloadType_POD,
					WorkloadName:      "pod1",
					VirtualIps: map[string]*workloadapi.PortList{
						"240.240.23.45": {
							Ports: []*workloadapi.Port{
								{
									ServicePort: 80,
									TargetPort:  8080,
								},
							},
						},
					},
				},
			},
		},
	}})

	assert.Equal(t, controller.ambientIndex.Lookup("testnetwork/140.140.0.11"), []*model.AddressInfo{{
		Address: &workloadapi.Address{
			Type: &workloadapi.Address_Workload{
				Workload: &workloadapi.Workload{
					Uid:               "cluster0//Pod/ns1/pod2",
					Name:              "pod2",
					Namespace:         "ns1",
					Addresses:         [][]byte{parseIP("140.140.0.11")},
					Node:              "node1",
					Network:           "testnetwork",
					ClusterId:         "cluster0",
					CanonicalName:     "other",
					CanonicalRevision: "latest",
					ServiceAccount:    "sa1",
					WorkloadType:      workloadapi.WorkloadType_POD,
					WorkloadName:      "pod2",
					VirtualIps:        nil, // labels don't match workloadSelector, this should be nil
				},
			},
		},
	}})

	assert.Equal(t, controller.ambientIndex.Lookup("testnetwork/240.240.34.56"), []*model.AddressInfo{{
		Address: &workloadapi.Address{
			Type: &workloadapi.Address_Workload{
				Workload: &workloadapi.Workload{
					Uid:               "cluster0/networking.istio.io/WorkloadEntry/ns1/name1",
					Name:              "name1",
					Namespace:         "ns1",
					Addresses:         [][]byte{parseIP("240.240.34.56")},
					Node:              "",
					Network:           "testnetwork",
					CanonicalName:     "a",
					CanonicalRevision: "latest",
					ServiceAccount:    "sa1",
					WorkloadType:      workloadapi.WorkloadType_POD,
					WorkloadName:      "name1",
					VirtualIps: map[string]*workloadapi.PortList{
						"testnetwork/240.240.23.45": {
							Ports: []*workloadapi.Port{
								{
									ServicePort: 80,
									TargetPort:  8080,
								},
							},
						},
					},
				},
			},
		},
	}})

	deleteServiceEntry([]string{"se.istio.io"}, []string{"240.240.23.45"}, "name1", "ns", map[string]string{"app": "a"})
	assertWorkloads(t, controller, "", workloadapi.WorkloadStatus_HEALTHY, "pod1", "pod2", "name1")
	// we should see an update for the workloads selected by the service entry
	assertEvent(t, fx, "cluster0//Pod/ns1/pod1", "cluster0//Pod/ns1/pod2", "cluster0/networking.istio.io/WorkloadEntry/ns1/name1", "ns/")
	assert.Equal(t, controller.ambientIndex.Lookup("testnetwork/140.140.0.10"), []*model.AddressInfo{{
		Address: &workloadapi.Address{
			Type: &workloadapi.Address_Workload{
				Workload: &workloadapi.Workload{
					Uid:               "cluster0//Pod/ns1/pod1",
					Name:              "pod1",
					Namespace:         "ns1",
					Addresses:         [][]byte{parseIP("140.140.0.10")},
					Node:              "node1",
					Network:           "testnetwork",
					ClusterId:         "cluster0",
					CanonicalName:     "a",
					CanonicalRevision: "latest",
					ServiceAccount:    "sa1",
					WorkloadType:      workloadapi.WorkloadType_POD,
					WorkloadName:      "pod1",
					VirtualIps:        nil, // vips for pod1 should be gone now
				},
			},
		},
	}})

	assert.Equal(t, controller.ambientIndex.Lookup("testnetwork/240.240.34.56"), []*model.AddressInfo{{
		Address: &workloadapi.Address{
			Type: &workloadapi.Address_Workload{
				Workload: &workloadapi.Workload{
					Uid:               "cluster0/networking.istio.io/WorkloadEntry/ns1/name1",
					Name:              "name1",
					Namespace:         "ns1",
					Addresses:         [][]byte{parseIP("240.240.34.56")},
					Node:              "",
					Network:           "testnetwork",
					CanonicalName:     "a",
					CanonicalRevision: "latest",
					ServiceAccount:    "sa1",
					WorkloadType:      workloadapi.WorkloadType_POD,
					WorkloadName:      "name1",
					VirtualIps:        nil, // vips for workload entry 1 should be gone now
				},
			},
		},
	}})
}

func generateServiceEntry(hosts, addresses []string, labels map[string]string) *v1alpha3.ServiceEntry {
	var endpoints []*v1alpha3.WorkloadEntry
	var workloadSelector *v1alpha3.WorkloadSelector

	if len(labels) > 0 {
		workloadSelector = &v1alpha3.WorkloadSelector{
			Labels: labels,
		}
	} else {
		endpoints = []*v1alpha3.WorkloadEntry{
			{
				Address: "127.0.0.1",
				Ports: map[string]uint32{
					"http": 8081, // we will override the SE http port
				},
			},
		}
	}

	return &v1alpha3.ServiceEntry{
		Hosts:     hosts,
		Addresses: addresses,
		Ports: []*v1alpha3.ServicePort{
			{
				Name:       "http",
				Number:     80,
				TargetPort: 8080,
			},
		},
		WorkloadSelector: workloadSelector,
		Endpoints:        endpoints,
	}
}
