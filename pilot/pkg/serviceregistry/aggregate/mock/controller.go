// Copyright 2019 Istio Authors
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

package mock

import (
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/tools/cache"

	meshconfig "istio.io/api/mesh/v1alpha1"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/serviceregistry"
	"istio.io/istio/pilot/pkg/serviceregistry/aggregate"
	kubecontroller "istio.io/istio/pilot/pkg/serviceregistry/kube/controller"
	"istio.io/pkg/timedfn"
)

// FakeXdsUpdater is used to test the registry.
type FakeXdsUpdater struct {
	// Events tracks notifications received by the updater
	Events chan XdsEvent
}

// XdsEvent is used to watch XdsEvents
type XdsEvent struct {
	// Type of the event
	Type string

	// The id of the event
	ID string
}

func (*FakeXdsUpdater) ConfigUpdate(*model.PushRequest) {

}

func (fx *FakeXdsUpdater) EDSUpdate(shard, hostname string, namespace string, entry []*model.IstioEndpoint) error {
	select {
	case fx.Events <- XdsEvent{Type: "eds", ID: hostname}:
	default:
	}
	return nil

}

// SvcUpdate is called when a service port mapping definition is updated.
// This interface is WIP - labels, annotations and other changes to service may be
// updated to force a EDS and CDS recomputation and incremental push, as it doesn't affect
// LDS/RDS.
func (fx *FakeXdsUpdater) SvcUpdate(shard, hostname string, ports map[string]uint32, rports map[uint32]string) {
	select {
	case fx.Events <- XdsEvent{Type: "service", ID: hostname}:
	default:
	}
}

func (fx *FakeXdsUpdater) WorkloadUpdate(id string, labels map[string]string, annotations map[string]string) {
	select {
	case fx.Events <- XdsEvent{Type: "workload", ID: id}:
	default:
	}
}

// NewFakeXDS creates a XdsUpdater reporting events via a channel.
func NewFakeXDS() *FakeXdsUpdater {
	return &FakeXdsUpdater{
		Events: make(chan XdsEvent, 100),
	}
}

func NewFakeKubeController(clusterID string) (*kubecontroller.Controller, *fake.Clientset, func()) {
	clientSet := fake.NewSimpleClientset()
	c := kubecontroller.NewController(clientSet, kubecontroller.Options{
		WatchedNamespace: "", // tests create resources in multiple ns
		DomainSuffix:     "cluster.local",
		ClusterID:        clusterID,
		XDSUpdater:       NewFakeXDS(),
	})

	stop := make(chan struct{})

	go c.Run(stop)

	return c, clientSet, func() { close(stop) }
}

func NewFakeAggregateControllerForMultiCluster() (*aggregate.Controller, func()) {
	cluster1 := "cluster1"
	cluster2 := "cluster2"
	controller1, client1, cancel1 := NewFakeKubeController(cluster1)
	controller2, client2, cancel2 := NewFakeKubeController(cluster2)

	meshNetworks := &meshconfig.MeshNetworks{
		Networks: map[string]*meshconfig.Network{
			"network1": {
				Endpoints: []*meshconfig.Network_NetworkEndpoints{
					{
						Ne: &meshconfig.Network_NetworkEndpoints_FromRegistry{
							FromRegistry: cluster1,
						},
					},
				},
			},
			"network2": {
				Endpoints: []*meshconfig.Network_NetworkEndpoints{
					{
						Ne: &meshconfig.Network_NetworkEndpoints_FromRegistry{
							FromRegistry: cluster2,
						},
					},
				},
			},
		},
	}

	// Set controller.networkForRegistry
	controller1.InitNetworkLookup(meshNetworks)
	controller2.InitNetworkLookup(meshNetworks)

	registry1 := aggregate.Registry{
		Name:             serviceregistry.ServiceRegistry("mockKubernetes1"),
		ClusterID:        cluster1,
		ServiceDiscovery: controller1,
		Controller:       controller1,
	}

	registry2 := aggregate.Registry{
		Name:             serviceregistry.ServiceRegistry("mockKubernetes1"),
		ClusterID:        cluster2,
		ServiceDiscovery: controller2,
		Controller:       controller2,
	}

	ctls := aggregate.NewController()
	ctls.AddRegistry(registry1)
	ctls.AddRegistry(registry2)

	_ = ctls.AppendServiceHandler(func(*model.Service, model.Event) {})

	_, _ = client1.CoreV1().Pods("test1").Create(&corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "pod1",
			Namespace: "test1",
			Labels: map[string]string{
				"app": "cluster1",
			},
		},
		Spec: corev1.PodSpec{},
		Status: corev1.PodStatus{
			PodIP: "192.168.0.10",
			Phase: corev1.PodRunning,
		},
	})

	_, _ = client1.CoreV1().Services("test1").Create(&corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "svc1",
			Namespace: "test1",
			Labels: map[string]string{
				"app": "cluster1",
			},
		},
		Spec: corev1.ServiceSpec{
			Selector: map[string]string{
				"app": "cluster1",
			},
			ClusterIP: "127.0.0.1",
			Ports: []corev1.ServicePort{
				{
					Name:     "http",
					Protocol: corev1.ProtocolTCP,
					Port:     80,
				},
			},
		},
	})

	_, _ = client2.CoreV1().Pods("test2").Create(&corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "pod2",
			Namespace: "test2",
			Labels: map[string]string{
				"app": "cluster2",
			},
		},
		Spec: corev1.PodSpec{},
		Status: corev1.PodStatus{
			PodIP: "192.168.0.10",
			Phase: corev1.PodRunning,
		},
	})

	_, _ = client2.CoreV1().Services("test2").Create(&corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "svc2",
			Namespace: "test2",
			Labels: map[string]string{
				"app": "cluster2",
			},
		},
		Spec: corev1.ServiceSpec{
			Selector: map[string]string{
				"app": "cluster2",
			},
			ClusterIP: "127.0.0.1",
			Ports: []corev1.ServicePort{
				{
					Name:     "http",
					Protocol: corev1.ProtocolTCP,
					Port:     80,
				},
			},
		},
	})

	stop := make(chan struct{})
	if err := timedfn.WithTimeout(func() {
		cache.WaitForCacheSync(stop, controller1.HasSynced)
		cache.WaitForCacheSync(stop, controller2.HasSynced)
	}, time.Second); err != nil {
		close(stop)
	}

	return ctls, func() {
		cancel1()
		cancel2()
	}
}
