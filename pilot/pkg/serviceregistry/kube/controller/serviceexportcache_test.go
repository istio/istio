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
	"errors"
	"fmt"
	"testing"
	"time"

	core "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	v12 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/mcs-api/pkg/apis/v1alpha1"

	"istio.io/api/label"
	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/serviceregistry/kube"
	"istio.io/istio/pkg/config/host"
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

func TestServiceNotExported(t *testing.T) {
	for _, mode := range []EndpointMode{EndpointsOnly, EndpointSliceOnly} {
		t.Run(mode.String(), func(t *testing.T) {
			// Create and run the controller.
			ec, cleanup := newTestServiceExportCache(t, mode)
			defer cleanup()

			// Check that the endpoint is cluster-local
			ec.checkServiceInstances(t, false)
		})
	}
}

func TestServiceExported(t *testing.T) {
	for _, mode := range []EndpointMode{EndpointsOnly, EndpointSliceOnly} {
		t.Run(mode.String(), func(t *testing.T) {
			// Create and run the controller.
			ec, cleanup := newTestServiceExportCache(t, mode)
			defer cleanup()

			// Export the service.
			ec.export(t)

			// Check that the endpoint is mesh-wide
			ec.checkServiceInstances(t, true)
		})
	}
}

func TestServiceUnexported(t *testing.T) {
	for _, mode := range []EndpointMode{EndpointsOnly, EndpointSliceOnly} {
		t.Run(mode.String(), func(t *testing.T) {
			// Create and run the controller.
			ec, cleanup := newTestServiceExportCache(t, mode)
			defer cleanup()

			// Export the service and then unexport it immediately.
			ec.export(t)
			ec.unExport(t)

			// Check that the endpoint is cluster-local
			ec.checkServiceInstances(t, false)
		})
	}
}

func newServiceExport() *v1alpha1.ServiceExport {
	return &v1alpha1.ServiceExport{
		TypeMeta: v12.TypeMeta{
			Kind:       "ServiceExport",
			APIVersion: "multicluster.x-k8s.io/v1alpha1",
		},
		ObjectMeta: v12.ObjectMeta{
			Name:      serviceExportName,
			Namespace: serviceExportNamespace,
		},
	}
}

func newTestServiceExportCache(t *testing.T, mode EndpointMode) (ec *serviceExportCacheImpl, cleanup func()) {
	t.Helper()

	stopCh := make(chan struct{})
	prevValue := features.EnableMCSServiceDiscovery
	features.EnableMCSServiceDiscovery = true
	cleanup = func() {
		close(stopCh)
		features.EnableMCSServiceDiscovery = prevValue
	}

	c, _ := NewFakeControllerWithOptions(FakeControllerOptions{
		Stop:      stopCh,
		ClusterID: testCluster,
		Mode:      mode,
	})

	// Create the test service and endpoints.
	createService(c, serviceExportName, serviceExportNamespace, map[string]string{},
		[]int32{8080}, map[string]string{"app": "prod-app"}, t)
	createEndpoints(t, c, serviceExportName, serviceExportNamespace, []string{"tcp-port"}, []string{serviceExportPodIP}, nil, nil)

	ec = c.exports.(*serviceExportCacheImpl)

	// Wait for the resources to be processed by the controller.
	retry.UntilOrFail(t, func() bool {
		if svc := ec.GetService(ec.serviceHostname()); svc == nil {
			return false
		}
		inst := ec.getProxyServiceInstances()
		return len(inst) == 1 && inst[0].Service != nil && inst[0].Endpoint != nil
	}, retry.Timeout(2*time.Second))
	return
}

func (ec *serviceExportCacheImpl) serviceHostname() host.Name {
	return kube.ServiceHostname(serviceExportName, serviceExportNamespace, ec.opts.DomainSuffix)
}

func (ec *serviceExportCacheImpl) export(t *testing.T) {
	t.Helper()

	_, _ = ec.client.MCSApis().MulticlusterV1alpha1().ServiceExports(serviceExportNamespace).Create(
		context.TODO(),
		newServiceExport(),
		v12.CreateOptions{})

	// Wait for the export to be processed by the controller.
	retry.UntilOrFail(t, func() bool {
		return ec.isExported(serviceExportNamespacedName)
	}, retry.Timeout(2*time.Second))

	// Wait for the XDS event.
	ec.waitForXDS(t, true)
}

func (ec *serviceExportCacheImpl) unExport(t *testing.T) {
	t.Helper()

	_ = ec.client.MCSApis().MulticlusterV1alpha1().ServiceExports(serviceExportNamespace).Delete(
		context.TODO(),
		serviceExportName,
		v12.DeleteOptions{})

	// Wait for the delete to be processed by the controller.
	retry.UntilOrFail(t, func() bool {
		return !ec.isExported(serviceExportNamespacedName)
	}, retry.Timeout(2*time.Second))

	// Wait for the XDS event.
	ec.waitForXDS(t, false)
}

func (ec *serviceExportCacheImpl) waitForXDS(t *testing.T, expectMeshWide bool) {
	t.Helper()
	retry.UntilSuccessOrFail(t, func() error {
		event := ec.opts.XDSUpdater.(*FakeXdsUpdater).Wait("eds")
		if event == nil {
			return errors.New("failed waiting for XDS event")
		}
		if len(event.Endpoints) != 1 {
			return fmt.Errorf("waitForXDS failed: expected 1 endpoint, but found %v", event.Endpoints)
		}
		ep := event.Endpoints[0]
		return ec.expectDiscoverable(ep, expectMeshWide)
	}, retry.Timeout(2*time.Second))
}

func (ec *serviceExportCacheImpl) expectNotExported(t *testing.T) {
	t.Helper()
	if ec.isExported(serviceExportNamespacedName) {
		t.Fatalf("endpoint was unexpectedly discoverable from proxy")
	}
}

func (ec *serviceExportCacheImpl) expectExported(t *testing.T) {
	t.Helper()
	if !ec.isExported(serviceExportNamespacedName) {
		t.Fatalf("endpoint was not discoverable from proxy")
	}
}

func (ec *serviceExportCacheImpl) getProxyServiceInstances() []*model.ServiceInstance {
	return ec.GetProxyServiceInstances(&model.Proxy{
		Type:            model.SidecarProxy,
		IPAddresses:     []string{serviceExportPodIP},
		Locality:        &core.Locality{Region: "r", Zone: "z"},
		ConfigNamespace: serviceExportNamespace,
		Metadata: &model.NodeMetadata{
			ServiceAccount: "account",
			ClusterID:      ec.Cluster(),
			Labels: map[string]string{
				"app":                      "prod-app",
				label.SecurityTlsMode.Name: "mutual",
			},
		},
	})
}

func (ec *serviceExportCacheImpl) checkServiceInstances(t *testing.T, meshWide bool) {
	t.Helper()
	inst := ec.getProxyServiceInstances()
	if len(inst) != 1 {
		t.Fatalf("expected 1 ServiceInstance, found %d", len(inst))
	}
	ep := inst[0].Endpoint
	if err := ec.expectDiscoverable(ep, meshWide); err != nil {
		t.Fatal(err)
	}
}

func (ec *serviceExportCacheImpl) expectDiscoverable(ep *model.IstioEndpoint, expectMeshWide bool) error {
	// All endpoints should be discoverable from within the same cluster.
	if !ep.IsDiscoverableFromProxy(&model.Proxy{
		Metadata: &model.NodeMetadata{
			ClusterID: ec.Cluster(),
		},
	}) {
		return fmt.Errorf("endpoint was not discoverable in the same cluster")
	}

	// Check if this endpoint is discoverable from another cluster.
	meshWide := ep.IsDiscoverableFromProxy(&model.Proxy{
		Metadata: &model.NodeMetadata{
			ClusterID: "some-other-cluster",
		},
	})

	if expectMeshWide {
		if !meshWide {
			return fmt.Errorf("endpoint was not discoverable mesh-wide")
		}
	} else {
		if meshWide {
			return fmt.Errorf("endpoint was discoverable mesh-wide")
		}
	}
	return nil
}
