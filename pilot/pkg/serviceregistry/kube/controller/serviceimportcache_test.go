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
	"reflect"
	"testing"
	"time"

	envoyCore "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	. "github.com/onsi/gomega"
	kubeMeta "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	mcsapi "sigs.k8s.io/mcs-api/pkg/apis/v1alpha1"

	"istio.io/api/label"
	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/serviceregistry/kube"
	"istio.io/istio/pkg/config/host"
	"istio.io/istio/pkg/kube/mcs"
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/util/retry"
)

const (
	serviceImportName      = "test-svc"
	serviceImportNamespace = "test-ns"
	serviceImportPodIP     = "128.0.0.2"
	serviceImportCluster   = "test-cluster"
)

var (
	serviceImportNamespacedName = types.NamespacedName{
		Namespace: serviceImportNamespace,
		Name:      serviceImportName,
	}
	serviceImportClusterSetHost = serviceClusterSetLocalHostname(serviceImportNamespacedName)
	serviceImportVIPs           = []string{"1.1.1.1"}
	serviceImportTimeout        = retry.Timeout(2 * time.Second)
)

func TestServiceNotImported(t *testing.T) {
	for _, mode := range []EndpointMode{EndpointsOnly, EndpointSliceOnly} {
		t.Run(mode.String(), func(t *testing.T) {
			c, ic := newTestServiceImportCache(t, mode)

			ic.createKubeService(t, c)

			// Check that the service does not have ClusterSet IPs.
			ic.checkServiceInstances(t)
		})
	}
}

func TestServiceImportedAfterCreated(t *testing.T) {
	for _, mode := range []EndpointMode{EndpointsOnly, EndpointSliceOnly} {
		t.Run(mode.String(), func(t *testing.T) {
			c, ic := newTestServiceImportCache(t, mode)

			ic.createKubeService(t, c)
			ic.createServiceImport(t, mcsapi.ClusterSetIP, serviceImportVIPs)

			// Check that the service has been assigned ClusterSet IPs.
			ic.checkServiceInstances(t)
		})
	}
}

func TestServiceCreatedAfterImported(t *testing.T) {
	for _, mode := range []EndpointMode{EndpointsOnly, EndpointSliceOnly} {
		t.Run(mode.String(), func(t *testing.T) {
			c, ic := newTestServiceImportCache(t, mode)

			ic.createServiceImport(t, mcsapi.ClusterSetIP, serviceImportVIPs)
			ic.createKubeService(t, c)

			// Check that the service has been assigned ClusterSet IPs.
			ic.checkServiceInstances(t)
		})
	}
}

func TestUpdateImportedService(t *testing.T) {
	for _, mode := range []EndpointMode{EndpointsOnly, EndpointSliceOnly} {
		t.Run(mode.String(), func(t *testing.T) {
			c, ic := newTestServiceImportCache(t, mode)

			ic.createKubeService(t, c)
			ic.createServiceImport(t, mcsapi.ClusterSetIP, serviceImportVIPs)
			ic.checkServiceInstances(t)

			// Update the k8s service and verify that both services are updated.
			ic.updateKubeService(t)
		})
	}
}

func TestHeadlessServiceImported(t *testing.T) {
	for _, mode := range []EndpointMode{EndpointsOnly, EndpointSliceOnly} {
		t.Run(mode.String(), func(t *testing.T) {
			// Create and run the controller.
			c, ic := newTestServiceImportCache(t, mode)

			ic.createKubeService(t, c)
			ic.createServiceImport(t, mcsapi.Headless, nil)

			// Verify that we did not generate the synthetic service for the headless service.
			ic.checkServiceInstances(t)
		})
	}
}

func TestDeleteImportedService(t *testing.T) {
	for _, mode := range []EndpointMode{EndpointsOnly, EndpointSliceOnly} {
		t.Run(mode.String(), func(t *testing.T) {
			// Create and run the controller.
			c1, ic := newTestServiceImportCache(t, mode)

			// Create and run another controller.
			c2, _ := NewFakeControllerWithOptions(t, FakeControllerOptions{
				ClusterID: "test-cluster2",
				Mode:      mode,
			})

			c1.opts.MeshServiceController.AddRegistryAndRun(c2, c2.stop)

			ic.createKubeService(t, c1)
			ic.createServiceImport(t, mcsapi.ClusterSetIP, serviceImportVIPs)
			ic.checkServiceInstances(t)

			// create the same service in cluster2
			createService(c2, serviceImportName, serviceImportNamespace, map[string]string{},
				[]int32{8080}, map[string]string{"app": "prod-app"}, t)

			// Delete the k8s service and verify that all internal services are removed.
			ic.deleteKubeService(t, c2)
		})
	}
}

func TestUnimportService(t *testing.T) {
	for _, mode := range []EndpointMode{EndpointsOnly, EndpointSliceOnly} {
		t.Run(mode.String(), func(t *testing.T) {
			// Create and run the controller.
			c, ic := newTestServiceImportCache(t, mode)

			ic.createKubeService(t, c)
			ic.createServiceImport(t, mcsapi.ClusterSetIP, serviceImportVIPs)
			ic.checkServiceInstances(t)

			ic.unimportService(t)
		})
	}
}

func TestAddServiceImportVIPs(t *testing.T) {
	for _, mode := range []EndpointMode{EndpointsOnly, EndpointSliceOnly} {
		t.Run(mode.String(), func(t *testing.T) {
			// Create and run the controller.
			c, ic := newTestServiceImportCache(t, mode)

			ic.createKubeService(t, c)
			ic.createServiceImport(t, mcsapi.ClusterSetIP, nil)
			ic.checkServiceInstances(t)

			ic.setServiceImportVIPs(t, serviceImportVIPs)
		})
	}
}

func TestUpdateServiceImportVIPs(t *testing.T) {
	for _, mode := range []EndpointMode{EndpointsOnly, EndpointSliceOnly} {
		t.Run(mode.String(), func(t *testing.T) {
			// Create and run the controller.
			c, ic := newTestServiceImportCache(t, mode)

			ic.createKubeService(t, c)
			ic.createServiceImport(t, mcsapi.ClusterSetIP, serviceImportVIPs)
			ic.checkServiceInstances(t)

			updatedVIPs := []string{"1.1.1.1", "1.1.1.2"}
			ic.setServiceImportVIPs(t, updatedVIPs)
		})
	}
}

func newTestServiceImportCache(t test.Failer, mode EndpointMode) (c *FakeController, ic *serviceImportCacheImpl) {
	test.SetBoolForTest(t, &features.EnableMCSHost, true)

	c, _ = NewFakeControllerWithOptions(t, FakeControllerOptions{
		ClusterID: serviceImportCluster,
		Mode:      mode,
	})

	ic = c.imports.(*serviceImportCacheImpl)
	return
}

func (ic *serviceImportCacheImpl) createKubeService(t *testing.T, c *FakeController) {
	t.Helper()

	// Create the test service and endpoints.
	createService(c, serviceImportName, serviceImportNamespace, map[string]string{},
		[]int32{8080}, map[string]string{"app": "prod-app"}, t)
	createEndpoints(t, c, serviceImportName, serviceImportNamespace, []string{"tcp-port"}, []string{serviceImportPodIP}, nil, nil)

	isImported := ic.isImported(serviceImportNamespacedName)

	// Wait for the resources to be processed by the controller.
	retry.UntilSuccessOrFail(t, func() error {
		clusterLocalHost := ic.clusterLocalHost()
		if svc := c.GetService(clusterLocalHost); svc == nil {
			return fmt.Errorf("failed looking up service for host %s", clusterLocalHost)
		}

		var expectedHosts map[host.Name]struct{}
		if isImported {
			expectedHosts = map[host.Name]struct{}{
				clusterLocalHost:            {},
				serviceImportClusterSetHost: {},
			}
		} else {
			expectedHosts = map[host.Name]struct{}{
				clusterLocalHost: {},
			}
		}

		instances := ic.getProxyServiceInstances()
		if len(instances) != len(expectedHosts) {
			return fmt.Errorf("expected 1 service instance, found %d", len(instances))
		}
		for _, si := range instances {
			if si.Service == nil {
				return fmt.Errorf("proxy ServiceInstance has nil service")
			}
			if si.Endpoint == nil {
				return fmt.Errorf("proxy ServiceInstance has nil endpoint")
			}
			if _, found := expectedHosts[si.Service.Hostname]; !found {
				return fmt.Errorf("found proxy ServiceInstance for unexpected host: %s", si.Service.Hostname)
			}
			delete(expectedHosts, si.Service.Hostname)
		}

		if len(expectedHosts) > 0 {
			return fmt.Errorf("failed to find proxy ServiceInstances for hosts: %v", expectedHosts)
		}

		return nil
	}, serviceImportTimeout)
}

func (ic *serviceImportCacheImpl) updateKubeService(t *testing.T) {
	t.Helper()
	svc, _ := ic.client.Kube().CoreV1().Services(serviceImportNamespace).Get(context.TODO(), serviceImportName, kubeMeta.GetOptions{})
	if svc == nil {
		t.Fatalf("failed to find k8s service: %s/%s", serviceImportNamespace, serviceImportName)
	}

	// Just add a new label.
	svc.Labels = map[string]string{
		"foo": "bar",
	}
	if _, err := ic.client.Kube().CoreV1().Services(serviceImportNamespace).Update(context.TODO(), svc, kubeMeta.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}

	hostNames := []host.Name{
		ic.clusterLocalHost(),
		serviceImportClusterSetHost,
	}

	// Wait for the services to pick up the label.
	retry.UntilSuccessOrFail(t, func() error {
		for _, hostName := range hostNames {
			svc := ic.GetService(hostName)
			if svc == nil {
				return fmt.Errorf("failed to find service for host %s", hostName)
			}
			if svc.Attributes.Labels["foo"] != "bar" {
				return fmt.Errorf("service not updated for %s", hostName)
			}
		}

		return nil
	}, serviceImportTimeout)
}

func (ic *serviceImportCacheImpl) deleteKubeService(t *testing.T, anotherCluster *FakeController) {
	t.Helper()

	if err := anotherCluster.client.Kube().
		CoreV1().Services(serviceImportNamespace).Delete(context.TODO(), serviceImportName, kubeMeta.DeleteOptions{}); err != nil {
		t.Fatal(err)
	}
	// Wait for the resources to be processed by the controller.
	if err := ic.client.Kube().CoreV1().Services(serviceImportNamespace).Delete(context.TODO(), serviceImportName, kubeMeta.DeleteOptions{}); err != nil {
		t.Fatal(err)
	}

	// Wait for the resources to be processed by the controller.
	retry.UntilSuccessOrFail(t, func() error {
		if svc := ic.GetService(ic.clusterLocalHost()); svc != nil {
			return fmt.Errorf("found deleted service for host %s", ic.clusterLocalHost())
		}
		if svc := ic.GetService(serviceImportClusterSetHost); svc != nil {
			return fmt.Errorf("found deleted service for host %s", serviceImportClusterSetHost)
		}

		instances := ic.getProxyServiceInstances()
		if len(instances) != 0 {
			return fmt.Errorf("expected 0 service instance, found %d", len(instances))
		}

		return nil
	}, serviceImportTimeout)
}

func (ic *serviceImportCacheImpl) getProxyServiceInstances() []*model.ServiceInstance {
	return ic.GetProxyServiceInstances(&model.Proxy{
		Type:            model.SidecarProxy,
		IPAddresses:     []string{serviceImportPodIP},
		Locality:        &envoyCore.Locality{Region: "r", Zone: "z"},
		ConfigNamespace: serviceImportNamespace,
		Labels: map[string]string{
			"app":                      "prod-app",
			label.SecurityTlsMode.Name: "mutual",
		},
		Metadata: &model.NodeMetadata{
			ServiceAccount: "account",
			ClusterID:      ic.Cluster(),
			Labels: map[string]string{
				"app":                      "prod-app",
				label.SecurityTlsMode.Name: "mutual",
			},
		},
	})
}

func (ic *serviceImportCacheImpl) getServiceImport(t *testing.T) *mcsapi.ServiceImport {
	t.Helper()

	// Get the ServiceImport as unstructured
	u, err := ic.client.Dynamic().Resource(mcs.ServiceImportGVR).Namespace(serviceImportNamespace).Get(
		context.TODO(), serviceImportName, kubeMeta.GetOptions{})
	if err != nil {
		return nil
	}

	// Convert to ServiceImport
	si := &mcsapi.ServiceImport{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(u.Object, si); err != nil {
		t.Fatal(err)
	}
	return si
}

func (ic *serviceImportCacheImpl) checkServiceInstances(t *testing.T) {
	t.Helper()
	g := NewWithT(t)

	si := ic.getServiceImport(t)

	var expectedIPs []string
	expectedServiceCount := 1
	expectMCSService := false
	if si != nil && si.Spec.Type == mcsapi.ClusterSetIP && len(si.Spec.IPs) > 0 {
		expectedIPs = si.Spec.IPs
		expectedServiceCount = 2
		expectMCSService = true
	}

	instances := ic.getProxyServiceInstances()
	g.Expect(instances).To(HaveLen(expectedServiceCount))

	for _, inst := range instances {
		svc := inst.Service
		if svc.Hostname == serviceImportClusterSetHost {
			if !expectMCSService {
				t.Fatalf("found ServiceInstance for unimported service %s", serviceImportClusterSetHost)
			}
			// Check the ClusterSet IPs.
			g.Expect(svc.ClusterVIPs.GetAddressesFor(ic.Cluster())).To(Equal(expectedIPs))
			return
		}
	}

	if expectMCSService {
		t.Fatalf("failed finding ServiceInstance for %s", serviceImportClusterSetHost)
	}
}

func (ic *serviceImportCacheImpl) createServiceImport(t *testing.T, importType mcsapi.ServiceImportType, vips []string) {
	t.Helper()

	// Create the ServiceImport resource in the cluster.
	_, err := ic.client.Dynamic().Resource(mcs.ServiceImportGVR).Namespace(serviceImportNamespace).Create(context.TODO(),
		newServiceImport(importType, vips),
		kubeMeta.CreateOptions{})
	if err != nil {
		t.Fatal(err)
	}

	shouldCreateMCSService := importType == mcsapi.ClusterSetIP && len(vips) > 0 &&
		ic.GetService(ic.clusterLocalHost()) != nil

	// Wait for the import to be processed by the controller.
	retry.UntilSuccessOrFail(t, func() error {
		if !ic.isImported(serviceImportNamespacedName) {
			return fmt.Errorf("serviceImport not found for %s", serviceImportClusterSetHost)
		}
		if shouldCreateMCSService && ic.GetService(serviceImportClusterSetHost) == nil {
			return fmt.Errorf("failed to find service for %s", serviceImportClusterSetHost)
		}
		return nil
	}, serviceImportTimeout)

	if shouldCreateMCSService {
		// Wait for the XDS event.
		ic.waitForXDS(t)
	}
}

func (ic *serviceImportCacheImpl) setServiceImportVIPs(t *testing.T, vips []string) {
	t.Helper()

	// Get the ServiceImport
	si := ic.getServiceImport(t)

	// Apply the ClusterSet IPs.
	si.Spec.IPs = vips
	if _, err := ic.client.Dynamic().Resource(mcs.ServiceImportGVR).Namespace(serviceImportNamespace).Update(
		context.TODO(), toUnstructured(si), kubeMeta.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}

	if len(vips) > 0 {
		// Wait for the import to be processed by the controller.
		retry.UntilSuccessOrFail(t, func() error {
			svc := ic.GetService(serviceImportClusterSetHost)
			if svc == nil {
				return fmt.Errorf("failed to find service for %s", serviceImportClusterSetHost)
			}

			actualVIPs := svc.ClusterVIPs.GetAddressesFor(ic.Cluster())
			if !reflect.DeepEqual(vips, actualVIPs) {
				return fmt.Errorf("expected ClusterSet VIPs %v, but found %v", vips, actualVIPs)
			}
			return nil
		}, serviceImportTimeout)

		// Wait for the XDS event.
		ic.waitForXDS(t)
	} else {
		// Wait for the import to be processed by the controller.
		retry.UntilSuccessOrFail(t, func() error {
			if svc := ic.GetService(serviceImportClusterSetHost); svc != nil {
				return fmt.Errorf("found unexpected service for %s", serviceImportClusterSetHost)
			}
			return nil
		}, serviceImportTimeout)
	}
}

func (ic *serviceImportCacheImpl) unimportService(t *testing.T) {
	t.Helper()

	if err := ic.client.Dynamic().Resource(mcs.ServiceImportGVR).Namespace(serviceImportNamespace).Delete(
		context.TODO(), serviceImportName, kubeMeta.DeleteOptions{}); err != nil {
		t.Fatal(err)
	}

	// Wait for the import to be processed by the controller.
	retry.UntilSuccessOrFail(t, func() error {
		if ic.isImported(serviceImportNamespacedName) {
			return fmt.Errorf("serviceImport found for %s", serviceImportClusterSetHost)
		}
		if ic.GetService(serviceImportClusterSetHost) != nil {
			return fmt.Errorf("found MCS service for unimported service %s", serviceImportClusterSetHost)
		}
		return nil
	}, serviceImportTimeout)
}

func (ic *serviceImportCacheImpl) isImported(name types.NamespacedName) bool {
	_, err := ic.lister.ByNamespace(name.Namespace).Get(name.Name)
	return err == nil
}

func (ic *serviceImportCacheImpl) waitForXDS(t *testing.T) {
	t.Helper()

	retry.UntilSuccessOrFail(t, func() error {
		return ic.checkXDS()
	}, serviceImportTimeout)
}

func (ic *serviceImportCacheImpl) checkXDS() error {
	event := ic.opts.XDSUpdater.(*FakeXdsUpdater).Wait("service")
	if event == nil {
		return errors.New("failed waiting for XDS event")
	}

	// The name of the event will be the cluster-local hostname.
	eventID := serviceImportClusterSetHost.String()
	if event.ID != eventID {
		return fmt.Errorf("waitForXDS failed: expected event id=%s, but found %s", eventID, event.ID)
	}
	return nil
}

func (ic *serviceImportCacheImpl) clusterLocalHost() host.Name {
	return kube.ServiceHostname(serviceImportName, serviceImportNamespace, ic.opts.DomainSuffix)
}

func newServiceImport(importType mcsapi.ServiceImportType, vips []string) *unstructured.Unstructured {
	si := &mcsapi.ServiceImport{
		TypeMeta: kubeMeta.TypeMeta{
			Kind:       "ServiceImport",
			APIVersion: "multicluster.x-k8s.io/v1alpha1",
		},
		ObjectMeta: kubeMeta.ObjectMeta{
			Name:      serviceImportName,
			Namespace: serviceImportNamespace,
		},
		Spec: mcsapi.ServiceImportSpec{
			Type: importType,
			IPs:  vips,
		},
	}
	return toUnstructured(si)
}

func toUnstructured(o any) *unstructured.Unstructured {
	u, err := runtime.DefaultUnstructuredConverter.ToUnstructured(o)
	if err != nil {
		panic(err)
	}
	return &unstructured.Unstructured{Object: u}
}
