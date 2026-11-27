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
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	mcsapi "sigs.k8s.io/mcs-api/pkg/apis/v1alpha1"

	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/serviceregistry/kube"
	"istio.io/istio/pilot/pkg/serviceregistry/util/xdsfake"
	"istio.io/istio/pkg/config/host"
	"istio.io/istio/pkg/kube/mcs"
	"istio.io/istio/pkg/maps"
	"istio.io/istio/pkg/ptr"
	"istio.io/istio/pkg/slices"
	istiotest "istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/util/assert"
	"istio.io/istio/pkg/test/util/retry"
)

const (
	serviceExportName      = "test-svc"
	serviceExportNamespace = "test-ns"
	serviceExportPodIP     = "128.0.0.2"
	testCluster            = "test-cluster"
)

var serviceExportNamespacedName = types.NamespacedName{
	Namespace: serviceExportNamespace,
	Name:      serviceExportName,
}

type ClusterLocalMode string

func (m ClusterLocalMode) String() string {
	return string(m)
}

const (
	alwaysClusterLocal ClusterLocalMode = "always cluster local"
	meshWide           ClusterLocalMode = "mesh wide"
)

var ClusterLocalModes = []ClusterLocalMode{alwaysClusterLocal, meshWide}

func TestServiceNotExported(t *testing.T) {
	for _, clusterLocalMode := range ClusterLocalModes {
		t.Run(clusterLocalMode.String(), func(t *testing.T) {
			// Create and run the controller.
			ec, endpoints := newTestServiceExportCache(t, clusterLocalMode)
			// Check that the endpoint is cluster-local
			ec.checkServiceInstancesOrFail(t, false, endpoints)
		})
	}
}

func TestServiceExported(t *testing.T) {
	for _, clusterLocalMode := range ClusterLocalModes {
		t.Run(clusterLocalMode.String(), func(t *testing.T) {
			// Create and run the controller.
			ec, endpoints := newTestServiceExportCache(t, clusterLocalMode)
			// Export the service.
			ec.export(t)

			// Check that the endpoint is mesh-wide
			ec.checkServiceInstancesOrFail(t, true, endpoints)
		})
	}
}

func TestServiceUnexported(t *testing.T) {
	for _, clusterLocalMode := range ClusterLocalModes {
		t.Run(clusterLocalMode.String(), func(t *testing.T) {
			// Create and run the controller.
			ec, endpoints := newTestServiceExportCache(t, clusterLocalMode)
			// Export the service and then unexport it immediately.
			ec.export(t)
			ec.unExport(t)

			// Check that the endpoint is cluster-local
			ec.checkServiceInstancesOrFail(t, false, endpoints)
		})
	}
}

func TestServiceExportDiscoverabilityEventOrder(t *testing.T) {
	orders := [][]string{
		{"export", "endpoints", "service"},
		{"endpoints", "export", "service"},
		{"service", "export", "endpoints"},
		{"service", "endpoints", "export"},
		{"endpoints", "service", "export"},
		{"export", "service", "endpoints"},
	}
	for _, clusterLocalMode := range ClusterLocalModes {
		for _, exported := range []bool{true, false} {
			for _, order := range orders {
				t.Run(fmt.Sprintf("%s/exported=%t/%s", clusterLocalMode, exported, strings.Join(order, "-")), func(t *testing.T) {
					istiotest.SetForTest(t, &features.EnableMCSServiceDiscovery, true)
					istiotest.SetForTest(t, &features.EnableMCSClusterLocal, clusterLocalMode == alwaysClusterLocal)
					istiotest.SetForTest(t, &features.EnableMCSHost, false)
					c, _ := NewFakeControllerWithOptions(t, FakeControllerOptions{
						ClusterID: testCluster,
						CRDs:      []schema.GroupVersionResource{mcs.ServiceExportGVR},
						SkipRun:   true,
					})
					ec := c.exports.(*serviceExportCacheImpl)
					svc := &corev1.Service{
						ObjectMeta: metav1.ObjectMeta{Name: serviceExportName, Namespace: serviceExportNamespace},
						Spec: corev1.ServiceSpec{
							ClusterIP: "10.0.0.1",
							Ports:     []corev1.ServicePort{{Name: "tcp-port", Port: 8080}},
						},
					}
					pod := generatePod([]string{serviceExportPodIP}, "pod", serviceExportNamespace, "sa", "node", nil, nil)
					epSlice := &discoveryv1.EndpointSlice{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "test-slice",
							Namespace: serviceExportNamespace,
							Labels:    map[string]string{discoveryv1.LabelServiceName: serviceExportName},
						},
						AddressType: discoveryv1.AddressTypeIPv4,
						Ports: []discoveryv1.EndpointPort{{
							Name: ptr.Of("tcp-port"), Port: ptr.Of(int32(8080)),
						}},
						Endpoints: []discoveryv1.Endpoint{{
							Addresses:  []string{serviceExportPodIP},
							Conditions: discoveryv1.EndpointConditions{Ready: ptr.Of(true)},
							TargetRef:  &corev1.ObjectReference{Kind: "Pod", Name: pod.Name, Namespace: pod.Namespace},
						}},
					}
					ctx := context.Background()
					_, err := c.client.Kube().CoreV1().Namespaces().Create(ctx,
						&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: serviceExportNamespace}}, metav1.CreateOptions{})
					assert.NoError(t, err)
					_, err = c.client.Kube().CoreV1().Services(serviceExportNamespace).Create(ctx, svc, metav1.CreateOptions{})
					assert.NoError(t, err)
					_, err = c.client.Kube().CoreV1().Pods(serviceExportNamespace).Create(ctx, pod, metav1.CreateOptions{})
					assert.NoError(t, err)
					_, err = c.client.Kube().DiscoveryV1().EndpointSlices(serviceExportNamespace).Create(ctx, epSlice, metav1.CreateOptions{})
					assert.NoError(t, err)
					export := newServiceExport()
					if exported {
						_, err = c.client.Dynamic().Resource(mcs.ServiceExportGVR).Namespace(serviceExportNamespace).Create(ctx, export, metav1.CreateOptions{})
						assert.NoError(t, err)
					}

					// Informers have all objects, but the controller queue has not processed them yet.
					retry.UntilOrFail(t, func() bool {
						return c.services.Get(svc.Name, svc.Namespace) != nil &&
							c.pods.pods.Get(pod.Name, pod.Namespace) != nil &&
							c.endpoints.slices.Get(epSlice.Name, epSlice.Namespace) != nil &&
							ec.isExported(serviceExportNamespacedName) == exported
					})
					assert.NoError(t, c.pods.onEvent(nil, pod, model.EventAdd))
					for _, event := range order {
						switch event {
						case "export":
							if exported {
								assert.NoError(t, ec.onServiceExportEvent(nil, export, model.EventAdd))
							}
						case "endpoints":
							c.endpoints.onEventInternal(nil, epSlice, model.EventAdd)
						case "service":
							assert.NoError(t, c.onServiceEvent(nil, svc, model.EventAdd))
						}
					}

					// Both the slice cache and the shard used for EDS must have the final policy.
					for name, endpoints := range map[string][]*model.IstioEndpoint{
						"EndpointSlice cache": c.endpoints.endpointCache.Get(ec.serviceHostname()),
						"EDS shard":           GetEndpoints(c.GetService(ec.serviceHostname()), c.Endpoints),
					} {
						assert.Equal(t, len(endpoints), 1, name)
						if err := ec.checkEndpoint(exported, endpoints[0]); err != nil {
							t.Errorf("%s: %v", name, err)
						}
					}
				})
			}
		}
	}
}

func newServiceExport() *unstructured.Unstructured {
	se := &mcsapi.ServiceExport{
		TypeMeta: metav1.TypeMeta{
			Kind:       "ServiceExport",
			APIVersion: mcs.MCSSchemeGroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      serviceExportName,
			Namespace: serviceExportNamespace,
		},
	}
	return toUnstructured(se)
}

func newTestServiceExportCache(t *testing.T, clusterLocalMode ClusterLocalMode) (*serviceExportCacheImpl, *model.EndpointIndex) {
	t.Helper()

	istiotest.SetForTest(t, &features.EnableMCSServiceDiscovery, true)
	istiotest.SetForTest(t, &features.EnableMCSClusterLocal, clusterLocalMode == alwaysClusterLocal)

	c, _ := NewFakeControllerWithOptions(t, FakeControllerOptions{
		ClusterID: testCluster,
		CRDs:      []schema.GroupVersionResource{mcs.ServiceExportGVR},
	})

	// Create the test service and endpoints.
	createService(c, serviceExportName, serviceExportNamespace, []string{"10.0.0.1"}, map[string]string{}, map[string]string{},
		[]int32{8080}, map[string]string{"app": "prod-app"}, t)
	createEndpoints(t, c, serviceExportName, serviceExportNamespace, []string{"tcp-port"}, []string{serviceExportPodIP}, nil, nil)

	ec := c.exports.(*serviceExportCacheImpl)
	// Wait for the resources to be processed by the controller.
	retry.UntilOrFail(t, func() bool {
		if svc := ec.GetService(ec.serviceHostname()); svc == nil {
			return false
		}
		inst := ec.getEndpoint(c.Endpoints)
		return inst != nil
	}, serviceExportTimeout)
	return ec, c.Endpoints
}

func (ec *serviceExportCacheImpl) serviceHostname() host.Name {
	return kube.ServiceHostname(serviceExportName, serviceExportNamespace, ec.opts.DomainSuffix)
}

func (ec *serviceExportCacheImpl) export(t *testing.T) {
	t.Helper()

	_, err := ec.client.Dynamic().Resource(mcs.ServiceExportGVR).Namespace(serviceExportNamespace).Create(context.TODO(),
		newServiceExport(),
		metav1.CreateOptions{})
	if err != nil {
		t.Fatal(err)
	}

	// Wait for the export to be processed by the controller.
	retry.UntilOrFail(t, func() bool {
		return ec.isExported(serviceExportNamespacedName)
	}, serviceExportTimeout, retry.Message("expected to be exported"))

	// Wait for the XDS event.
	ec.waitForXDS(t, true)
}

func (ec *serviceExportCacheImpl) unExport(t *testing.T) {
	t.Helper()

	_ = ec.client.Dynamic().Resource(mcs.ServiceExportGVR).Namespace(serviceExportNamespace).Delete(
		context.TODO(),
		serviceExportName,
		metav1.DeleteOptions{})

	// Wait for the delete to be processed by the controller.
	retry.UntilOrFail(t, func() bool {
		return !ec.isExported(serviceExportNamespacedName)
	}, serviceExportTimeout)

	// Wait for the XDS event.
	ec.waitForXDS(t, false)
}

func (ec *serviceExportCacheImpl) waitForXDS(t *testing.T, exported bool) {
	t.Helper()
	retry.UntilSuccessOrFail(t, func() error {
		event := ec.opts.XDSUpdater.(*xdsfake.Updater).WaitOrFail(t, "eds")
		if len(event.Endpoints) != 1 {
			return fmt.Errorf("waitForXDS failed: expected 1 endpoint, found %d", len(event.Endpoints))
		}

		hostName := host.Name(event.ID)
		svc := ec.GetService(hostName)
		if svc == nil {
			return fmt.Errorf("unable to find service for host %s", hostName)
		}
		return ec.checkEndpoint(exported, event.Endpoints[0])
	}, serviceExportTimeout)
}

func (ec *serviceExportCacheImpl) getEndpoint(endpoints *model.EndpointIndex) *model.IstioEndpoint {
	svcs := ec.Services()
	for _, s := range svcs {
		ep := GetEndpoints(s, endpoints)
		if len(ep) > 0 {
			return ep[0]
		}
	}
	return nil
}

func GetEndpoints(s *model.Service, endpoints *model.EndpointIndex) []*model.IstioEndpoint {
	return GetEndpointsForPort(s, endpoints, 0)
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

func (ec *serviceExportCacheImpl) checkServiceInstancesOrFail(t *testing.T, exported bool, endpoints *model.EndpointIndex) {
	t.Helper()
	if err := ec.checkEndpoints(exported, endpoints); err != nil {
		t.Fatal(err)
	}
}

func (ec *serviceExportCacheImpl) checkEndpoints(exported bool, endpoints *model.EndpointIndex) error {
	ep := ec.getEndpoint(endpoints)
	if ep == nil {
		return fmt.Errorf("expected an endpoint, found none")
	}
	return ec.checkEndpoint(exported, ep)
}

func (ec *serviceExportCacheImpl) checkEndpoint(exported bool, ep *model.IstioEndpoint) error {
	// Should always be discoverable from the same cluster.
	if err := ec.checkDiscoverableFromSameCluster(ep); err != nil {
		return err
	}

	if exported && !features.EnableMCSClusterLocal {
		return ec.checkDiscoverableFromDifferentCluster(ep)
	}

	return ec.checkNotDiscoverableFromDifferentCluster(ep)
}

func (ec *serviceExportCacheImpl) checkDiscoverableFromSameCluster(ep *model.IstioEndpoint) error {
	if !ec.isDiscoverableFromSameCluster(ep) {
		return fmt.Errorf("endpoint was not discoverable from the same cluster")
	}
	return nil
}

func (ec *serviceExportCacheImpl) checkDiscoverableFromDifferentCluster(ep *model.IstioEndpoint) error {
	if !ec.isDiscoverableFromDifferentCluster(ep) {
		return fmt.Errorf("endpoint was not discoverable from a different cluster")
	}
	return nil
}

func (ec *serviceExportCacheImpl) checkNotDiscoverableFromDifferentCluster(ep *model.IstioEndpoint) error {
	if ec.isDiscoverableFromDifferentCluster(ep) {
		return fmt.Errorf("endpoint was discoverable from a different cluster")
	}
	return nil
}

func (ec *serviceExportCacheImpl) isDiscoverableFromSameCluster(ep *model.IstioEndpoint) bool {
	return ep.IsDiscoverableFromProxy(&model.Proxy{
		Metadata: &model.NodeMetadata{
			ClusterID: ec.Cluster(),
		},
	})
}

func (ec *serviceExportCacheImpl) isDiscoverableFromDifferentCluster(ep *model.IstioEndpoint) bool {
	return ep.IsDiscoverableFromProxy(&model.Proxy{
		Metadata: &model.NodeMetadata{
			ClusterID: "some-other-cluster",
		},
	})
}
