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
	"fmt"
	"sync"
	"testing"
	"time"

	"go.uber.org/atomic"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"istio.io/api/label"
	meshconfig "istio.io/api/mesh/v1alpha1"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/config/mesh"
	"istio.io/istio/pkg/test/util/retry"
)

func TestNetworkUpdateTriggers(t *testing.T) {
	meshNetworks := mesh.NewFixedNetworksWatcher(nil)
	c, _ := NewFakeControllerWithOptions(t, FakeControllerOptions{ClusterID: "Kubernetes", NetworksWatcher: meshNetworks, DomainSuffix: "cluster.local"})

	if len(c.NetworkGateways()) != 0 {
		t.Fatal("did not expect any gateways yet")
	}

	notified := atomic.NewBool(false)
	var (
		gwMu sync.Mutex
		gws  []model.NetworkGateway
	)
	setGws := func(v []model.NetworkGateway) {
		gwMu.Lock()
		defer gwMu.Unlock()
		gws = v
	}
	getGws := func() []model.NetworkGateway {
		gwMu.Lock()
		defer gwMu.Unlock()
		return gws
	}

	c.AppendNetworkGatewayHandler(func() {
		notified.Store(true)
		setGws(c.NetworkGateways())
	})
	expectGateways := func(t *testing.T, expectedGws int) {
		defer notified.Store(false)
		// 1. wait for a notification
		retry.UntilSuccessOrFail(t, func() error {
			if !notified.Load() {
				return fmt.Errorf("no gateway notify")
			}
			if n := len(getGws()); n != expectedGws {
				return fmt.Errorf("expected %d gateways but got %d", expectedGws, n)
			}
			return nil
		}, retry.Timeout(5*time.Second), retry.Delay(10*time.Millisecond))
	}

	t.Run("add meshnetworks", func(t *testing.T) {
		addMeshNetworksFromRegistryGateway(t, c, meshNetworks)
		expectGateways(t, 2)
	})
	fmt.Println(c.NetworkGateways())
	t.Run("add labeled service", func(t *testing.T) {
		addLabeledServiceGateway(t, c, "nw0")
		expectGateways(t, 3)
	})
	t.Run("update labeled service network", func(t *testing.T) {
		addLabeledServiceGateway(t, c, "nw1")
		expectGateways(t, 3)
	})
	t.Run("remove labeled service", func(t *testing.T) {
		removeLabeledServiceGateway(t, c)
		expectGateways(t, 2)
	})
	t.Run("remove meshnetworks", func(t *testing.T) {
		meshNetworks.SetNetworks(nil)
		expectGateways(t, 0)
	})
}

func addLabeledServiceGateway(t *testing.T, c *FakeController, nw string) {
	ctx := context.TODO()

	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: "istio-labeled-gw", Namespace: "arbitrary-ns", Labels: map[string]string{
			label.TopologyNetwork.Name: nw,
		}},
		Spec: corev1.ServiceSpec{
			Type:  corev1.ServiceTypeLoadBalancer,
			Ports: []corev1.ServicePort{{Port: 15443, Protocol: corev1.ProtocolTCP}},
		},
		Status: corev1.ServiceStatus{LoadBalancer: corev1.LoadBalancerStatus{Ingress: []corev1.LoadBalancerIngress{{
			IP:    "2.3.4.6",
			Ports: []corev1.PortStatus{{Port: 15443, Protocol: corev1.ProtocolTCP}},
		}}}},
	}

	if _, err := c.client.Kube().CoreV1().Services("arbitrary-ns").Get(ctx, "istio-labeled-gw", metav1.GetOptions{}); err == nil {
		// update
		if _, err := c.client.Kube().CoreV1().Services("arbitrary-ns").Update(context.TODO(), svc, metav1.UpdateOptions{}); err != nil {
			t.Fatal(err)
		}
	} else if errors.IsNotFound(err) {
		// create
		if _, err := c.client.Kube().CoreV1().Services("arbitrary-ns").Create(context.TODO(), svc, metav1.CreateOptions{}); err != nil {
			t.Fatal(err)
		}
	} else {
		t.Fatal(err)
	}
}

func removeLabeledServiceGateway(t *testing.T, c *FakeController) {
	err := c.client.Kube().CoreV1().Services("arbitrary-ns").Delete(context.TODO(), "istio-labeled-gw", metav1.DeleteOptions{})
	if err != nil {
		t.Fatal(err)
	}
}

func addMeshNetworksFromRegistryGateway(t *testing.T, c *FakeController, watcher mesh.NetworksWatcher) {
	_, err := c.client.Kube().CoreV1().Services("istio-system").Create(context.TODO(), &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: "istio-meshnetworks-gw", Namespace: "istio-system"},
		Spec: corev1.ServiceSpec{
			Type:  corev1.ServiceTypeLoadBalancer,
			Ports: []corev1.ServicePort{{Port: 15443, Protocol: corev1.ProtocolTCP}},
		},
		Status: corev1.ServiceStatus{LoadBalancer: corev1.LoadBalancerStatus{Ingress: []corev1.LoadBalancerIngress{{
			IP:    "1.2.3.4",
			Ports: []corev1.PortStatus{{Port: 15443, Protocol: corev1.ProtocolTCP}},
		}}}},
	}, metav1.CreateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	watcher.SetNetworks(&meshconfig.MeshNetworks{Networks: map[string]*meshconfig.Network{
		"nw0": {
			Endpoints: []*meshconfig.Network_NetworkEndpoints{{
				Ne: &meshconfig.Network_NetworkEndpoints_FromRegistry{FromRegistry: "Kubernetes"},
			}},
			Gateways: []*meshconfig.Network_IstioNetworkGateway{{
				Port: 15443,
				Gw:   &meshconfig.Network_IstioNetworkGateway_RegistryServiceName{RegistryServiceName: "istio-meshnetworks-gw.istio-system.svc.cluster.local"},
			}},
		},
		"nw1": {
			Endpoints: []*meshconfig.Network_NetworkEndpoints{{
				Ne: &meshconfig.Network_NetworkEndpoints_FromRegistry{FromRegistry: "Kubernetes"},
			}},
			Gateways: []*meshconfig.Network_IstioNetworkGateway{{
				Port: 15443,
				Gw:   &meshconfig.Network_IstioNetworkGateway_RegistryServiceName{RegistryServiceName: "istio-meshnetworks-gw.istio-system.svc.cluster.local"},
			}},
		},
	}})
}
