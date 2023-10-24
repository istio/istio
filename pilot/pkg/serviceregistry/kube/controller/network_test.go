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
	"fmt"
	"sync"
	"testing"
	"time"

	"go.uber.org/atomic"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	k8sv1 "sigs.k8s.io/gateway-api/apis/v1"
	"sigs.k8s.io/gateway-api/apis/v1beta1"

	"istio.io/api/label"
	meshconfig "istio.io/api/mesh/v1alpha1"
	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/config/mesh"
	"istio.io/istio/pkg/config/schema/gvr"
	"istio.io/istio/pkg/kube/kclient"
	"istio.io/istio/pkg/kube/kclient/clienttest"
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/util/retry"
)

func TestNetworkUpdateTriggers(t *testing.T) {
	test.SetForTest(t, &features.MultiNetworkGatewayAPI, true)
	meshNetworks := mesh.NewFixedNetworksWatcher(nil)
	c, _ := NewFakeControllerWithOptions(t, FakeControllerOptions{
		ClusterID:       "Kubernetes",
		NetworksWatcher: meshNetworks,
		DomainSuffix:    "cluster.local",
		CRDs:            []schema.GroupVersionResource{gvr.KubernetesGateway},
	})

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
	t.Run("add kubernetes gateway", func(t *testing.T) {
		addOrUpdateGatewayResource(t, c, 35443)
		expectGateways(t, 7)
	})
	t.Run("update kubernetes gateway", func(t *testing.T) {
		addOrUpdateGatewayResource(t, c, 45443)
		expectGateways(t, 7)
	})
	t.Run("remove kubernetes gateway", func(t *testing.T) {
		removeGatewayResource(t, c)
		expectGateways(t, 3)
	})
	t.Run("remove labeled service", func(t *testing.T) {
		removeLabeledServiceGateway(t, c)
		expectGateways(t, 2)
	})
	// gateways are created even with out service
	t.Run("add kubernetes gateway", func(t *testing.T) {
		addOrUpdateGatewayResource(t, c, 35443)
		expectGateways(t, 6)
	})
	t.Run("remove kubernetes gateway", func(t *testing.T) {
		removeGatewayResource(t, c)
		expectGateways(t, 2)
	})
	t.Run("remove meshnetworks", func(t *testing.T) {
		meshNetworks.SetNetworks(nil)
		expectGateways(t, 0)
	})
}

func addLabeledServiceGateway(t *testing.T, c *FakeController, nw string) {
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
	clienttest.Wrap(t, c.services).CreateOrUpdate(svc)
}

func removeLabeledServiceGateway(t *testing.T, c *FakeController) {
	clienttest.Wrap(t, c.services).Delete("istio-labeled-gw", "arbitrary-ns")
}

// creates a gateway that exposes 2 ports that are valid auto-passthrough ports
// and it does so on an IP and a hostname
func addOrUpdateGatewayResource(t *testing.T, c *FakeController, customPort int) {
	passthroughMode := k8sv1.TLSModePassthrough
	ipType := v1beta1.IPAddressType
	hostnameType := v1beta1.HostnameAddressType
	clienttest.Wrap(t, kclient.New[*v1beta1.Gateway](c.client)).CreateOrUpdate(&v1beta1.Gateway{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "eastwest-gwapi",
			Namespace: "istio-system",
			Labels:    map[string]string{label.TopologyNetwork.Name: "nw2"},
		},
		Spec: v1beta1.GatewaySpec{
			GatewayClassName: "istio",
			Addresses: []v1beta1.GatewayAddress{
				{Type: &ipType, Value: "1.2.3.4"},
				{Type: &hostnameType, Value: "some hostname"},
			},
			Listeners: []v1beta1.Listener{
				{
					Name: "detected-by-options",
					TLS: &v1beta1.GatewayTLSConfig{
						Mode: &passthroughMode,
						Options: map[v1beta1.AnnotationKey]v1beta1.AnnotationValue{
							constants.ListenerModeOption: constants.ListenerModeAutoPassthrough,
						},
					},
					Port: v1beta1.PortNumber(customPort),
				},
				{
					Name: "detected-by-number",
					TLS:  &v1beta1.GatewayTLSConfig{Mode: &passthroughMode},
					Port: 15443,
				},
			},
		},
		Status: v1beta1.GatewayStatus{},
	})
}

func removeGatewayResource(t *testing.T, c *FakeController) {
	clienttest.Wrap(t, kclient.New[*v1beta1.Gateway](c.client)).Delete("eastwest-gwapi", "istio-system")
}

func addMeshNetworksFromRegistryGateway(t *testing.T, c *FakeController, watcher mesh.NetworksWatcher) {
	clienttest.Wrap(t, c.services).Create(&corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: "istio-meshnetworks-gw", Namespace: "istio-system"},
		Spec: corev1.ServiceSpec{
			Type:  corev1.ServiceTypeLoadBalancer,
			Ports: []corev1.ServicePort{{Port: 15443, Protocol: corev1.ProtocolTCP}},
		},
		Status: corev1.ServiceStatus{LoadBalancer: corev1.LoadBalancerStatus{Ingress: []corev1.LoadBalancerIngress{{
			IP:    "1.2.3.4",
			Ports: []corev1.PortStatus{{Port: 15443, Protocol: corev1.ProtocolTCP}},
		}}}},
	})
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
