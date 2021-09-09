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
	. "github.com/onsi/gomega"
	v12 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/mcs-api/pkg/apis/v1alpha1"

	"istio.io/api/label"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/serviceregistry/kube"
	"istio.io/istio/pkg/config/host"
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
	serviceImportClusterSetHost = kube.ServiceClusterSetLocalHostname(serviceImportNamespacedName)
	serviceImportClusterSetIPs  = []string{"1.1.1.1"}
)

func TestServiceNotImported(t *testing.T) {
	stopCh := make(chan struct{})
	defer close(stopCh)

	// Create and run the controller.
	ec := newTestServiceImportCache(t, stopCh)

	// Check that the service does not have ClusterSet IPs.
	ec.checkServiceInstances(t, false)
}

func TestServiceImported(t *testing.T) {
	stopCh := make(chan struct{})
	defer close(stopCh)

	// Create and run the controller.
	ec := newTestServiceImportCache(t, stopCh)

	// Import the service.
	ec.importService(t, v1alpha1.ClusterSetIP)

	// Check that the service has been assigned ClusterSet IPs.
	ec.checkServiceInstances(t, true)
}

func TestHeadlessServiceImported(t *testing.T) {
	stopCh := make(chan struct{})
	defer close(stopCh)

	// Create and run the controller.
	ec := newTestServiceImportCache(t, stopCh)

	// Import the service.
	ec.importService(t, v1alpha1.Headless)

	// Check that the headless services do not have ClusterSet IPs.
	ec.checkServiceInstances(t, false)
}

func newTestServiceImportCache(t *testing.T, stopCh chan struct{}) *serviceImportCacheImpl {
	t.Helper()
	c, _ := NewFakeControllerWithOptions(FakeControllerOptions{
		EnableMCSServiceDiscovery: true,
		Stop:                      stopCh,
		ClusterID:                 serviceImportCluster,
	})

	// Create the test service and endpoints.
	createService(c, serviceImportName, serviceImportNamespace, map[string]string{},
		[]int32{8080}, map[string]string{"app": "prod-app"}, t)
	createEndpoints(t, c, serviceImportName, serviceImportNamespace, []string{"tcp-port"}, []string{serviceImportPodIP}, nil, nil)

	ic := c.imports.(*serviceImportCacheImpl)

	// Wait for the resources to be processed by the controller.
	retry.UntilSuccessOrFail(t, func() error {
		if svc, _ := ic.GetService(ic.serviceHostname()); svc == nil {
			return fmt.Errorf("failed looking up service for host %s", ic.serviceHostname())
		}
		inst := ic.getProxyServiceInstances()
		if len(inst) != 1 {
			return fmt.Errorf("expected 1 service instance, found %d", len(inst))
		}
		if inst[0].Service == nil {
			return fmt.Errorf("proxy service instance has nil service")
		}
		if inst[0].Endpoint == nil {
			return fmt.Errorf("proxy service instance has nil endpoint")
		}
		return nil
	}, retry.Timeout(2*time.Second))

	return ic
}

func (ic *serviceImportCacheImpl) getProxyServiceInstances() []*model.ServiceInstance {
	return ic.GetProxyServiceInstances(&model.Proxy{
		Type:            model.SidecarProxy,
		IPAddresses:     []string{serviceImportPodIP},
		Locality:        &core.Locality{Region: "r", Zone: "z"},
		ConfigNamespace: serviceImportNamespace,
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

func (ic *serviceImportCacheImpl) checkServiceInstances(t *testing.T, imported bool) {
	t.Helper()
	instances := ic.getProxyServiceInstances()
	if len(instances) != 1 {
		t.Fatalf("expected 1 ServiceInstance, found %d", len(instances))
	}
	inst := instances[0]
	svc := inst.Service

	// Check the ClusterSet host.
	g := NewWithT(t)
	g.Expect(svc.ClusterSetLocal.Hostname).To(Equal(serviceImportClusterSetHost))

	// Check the ClusterSet IPs.
	var expectedClusterSetIPs []string
	if imported {
		expectedClusterSetIPs = serviceImportClusterSetIPs
	}
	g.Expect(svc.ClusterSetLocal.ClusterVIPs.GetAddressesFor(ic.Cluster())).To(Equal(expectedClusterSetIPs))
}

func (ic *serviceImportCacheImpl) importService(t *testing.T, importType v1alpha1.ServiceImportType) {
	t.Helper()

	if _, err := ic.client.MCSApis().MulticlusterV1alpha1().ServiceImports(serviceImportNamespace).Create(
		context.TODO(),
		newServiceImport(importType),
		v12.CreateOptions{}); err != nil {
		t.Fatal(err)
	}

	// Wait for the export to be processed by the controller.
	retry.UntilOrFail(t, func() bool {
		return ic.isImported(serviceImportNamespacedName)
	}, retry.Timeout(2*time.Second))

	// Wait for the XDS event.
	ic.waitForXDS(t)
}

func (ic *serviceImportCacheImpl) isImported(name types.NamespacedName) bool {
	_, err := ic.lister.ServiceImports(name.Namespace).Get(name.Name)
	return err == nil
}

func (ic *serviceImportCacheImpl) serviceHostname() host.Name {
	return kube.ServiceHostname(serviceImportName, serviceImportNamespace, ic.opts.DomainSuffix)
}

func (ic *serviceImportCacheImpl) waitForXDS(t *testing.T) {
	t.Helper()

	retry.UntilSuccessOrFail(t, func() error {
		return ic.checkXDS()
	}, retry.Timeout(2*time.Second))
}

func (ic *serviceImportCacheImpl) checkXDS() error {
	event := ic.opts.XDSUpdater.(*FakeXdsUpdater).Wait("xds")
	if event == nil {
		return errors.New("failed waiting for XDS event")
	}

	// The name of the event will be the cluster-local hostname.
	eventID := string(ic.serviceHostname())
	if event.ID != eventID {
		return fmt.Errorf("waitForXDS failed: expected event id=%s, but found %s", eventID, event.ID)
	}
	return nil
}

func newServiceImport(importType v1alpha1.ServiceImportType) *v1alpha1.ServiceImport {
	var ips []string
	if importType == v1alpha1.ClusterSetIP {
		ips = serviceImportClusterSetIPs
	}
	return &v1alpha1.ServiceImport{
		TypeMeta: v12.TypeMeta{
			Kind:       "ServiceImport",
			APIVersion: "multicluster.x-k8s.io/v1alpha1",
		},
		ObjectMeta: v12.ObjectMeta{
			Name:      serviceExportName,
			Namespace: serviceExportNamespace,
		},
		Spec: v1alpha1.ServiceImportSpec{
			Type: importType,
			IPs:  ips,
		},
	}
}
