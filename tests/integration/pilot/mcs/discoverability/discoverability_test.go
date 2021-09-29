//go:build integ
// +build integ

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

package discoverability

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ghodss/yaml"
	"golang.org/x/sync/errgroup"
	kubeCore "k8s.io/api/core/v1"
	kubeMeta "k8s.io/apimachinery/pkg/apis/meta/v1"
	mcs "sigs.k8s.io/mcs-api/pkg/apis/v1alpha1"

	"istio.io/api/annotation"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/cluster"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/common"
	"istio.io/istio/pkg/test/framework/components/echo/echoboot"
	"istio.io/istio/pkg/test/framework/components/echo/echotest"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/framework/label"
	"istio.io/istio/pkg/test/framework/resource"
	"istio.io/istio/pkg/test/scopes"
	"istio.io/istio/pkg/test/util/retry"
)

const (
	serviceA = "svc-a"
	serviceB = "svc-b"
)

var (
	i      istio.Instance
	testNS string
	echos  echo.Instances

	retryTimeout = retry.Timeout(1 * time.Minute)
)

func TestMain(m *testing.M) {
	framework.
		NewSuite(m).
		Label(label.CustomSetup).
		RequireMinVersion(17).
		RequireMinClusters(2).
		Setup(installMCSCRDs).
		Setup(istio.Setup(&i, enableMCSServiceDiscovery)).
		Setup(deployEchos).
		Setup(importServiceInAllClusters).
		Run()
}

func TestClusterLocal(t *testing.T) {
	framework.NewTest(t).
		Features("traffic.mcs.servicediscovery").
		RequireIstioVersion("1.11").
		Run(func(t framework.TestContext) {
			// Don't export service B in any cluster. All requests should stay in-cluster.

			sendTrafficBetweenAllClusters(t, func(t framework.TestContext, src echo.Instance, dst echo.Instances) {
				// Ensure that all requests stay in the same cluster
				expectedClusters := cluster.Clusters{src.Config().Cluster}
				checkClustersReached(t, src, dst[0], expectedClusters)
			})
		})
}

func TestMeshWide(t *testing.T) {
	framework.NewTest(t).
		Features("traffic.mcs.servicediscovery").
		Run(func(t framework.TestContext) {
			// Export service B in all clusters.
			createAndCleanupServiceExport(t, serviceB, t.Clusters())

			sendTrafficBetweenAllClusters(t, func(t framework.TestContext, src echo.Instance, dst echo.Instances) {
				// Ensure that all destination clusters are reached.
				expectedClusters := dst.Clusters()
				checkClustersReached(t, src, dst[0], expectedClusters)
			})
		})
}

func TestServiceExportedInOneCluster(t *testing.T) {
	framework.NewTest(t).
		Features("traffic.mcs.servicediscovery").
		Run(func(t framework.TestContext) {
			// Get all the clusters where service B resides.
			bClusters := echos.Match(echo.Service(serviceB)).Clusters()

			// Test exporting service B exclusively in each cluster.
			for _, exportCluster := range bClusters {
				exportCluster := exportCluster
				t.NewSubTestf("b exported in %s", exportCluster.StableName()).
					Run(func(t framework.TestContext) {
						// Export service B in the export cluster.
						createAndCleanupServiceExport(t, serviceB, cluster.Clusters{exportCluster})

						sendTrafficBetweenAllClusters(t, func(t framework.TestContext, src echo.Instance, dst echo.Instances) {
							// Since we're exporting only the endpoints in the exportCluster, depending
							// on where we call service B from, we'll reach a different set of endpoints.
							// If we're calling from exportCluster, it will be the same as cluster-local
							// (i.e. we'll only reach endpoints in exportCluster). From all other clusters,
							// we should reach endpoints in that cluster AND exportCluster.
							expectedClusters := cluster.Clusters{exportCluster}
							if src.Config().Cluster.Name() != exportCluster.Name() {
								expectedClusters = append(expectedClusters, src.Config().Cluster)
							}
							checkClustersReached(t, src, dst[0], expectedClusters)
						})
					})
			}
		})
}

func installMCSCRDs(t resource.Context) error {
	for _, f := range []string{"mcs-serviceexport-crd.yaml", "mcs-serviceimport-crd.yaml"} {
		crd, err := os.ReadFile("../../testdata/" + f)
		if err != nil {
			return err
		}
		if t.Settings().NoCleanup {
			if err := t.Config().ApplyYAMLNoCleanup("", string(crd)); err != nil {
				return err
			}
		} else {
			if err := t.Config().ApplyYAML("", string(crd)); err != nil {
				return err
			}
		}
	}
	return nil
}

func enableMCSServiceDiscovery(_ resource.Context, cfg *istio.Config) {
	cfg.ControlPlaneValues = `
values:
  pilot:
    env:
      PILOT_USE_ENDPOINT_SLICE: "true"
      ENABLE_MCS_SERVICE_DISCOVERY: "true"
      ENABLE_MCS_HOST: "true"`
}

func deployEchos(t resource.Context) error {
	// Create a new namespace in each cluster.
	ns, err := namespace.New(t, namespace.Config{
		Prefix: "mcs",
		Inject: true,
	})
	if err != nil {
		return err
	}
	testNS = ns.Name()

	// Create echo instances in each cluster.
	echos, err = echoboot.NewBuilder(t).
		WithClusters(t.Clusters()...).
		WithConfig(echo.Config{
			Service:   serviceA,
			Namespace: ns,
			Ports:     common.EchoPorts,
		}).
		WithConfig(echo.Config{
			Service:   serviceB,
			Namespace: ns,
			Ports:     common.EchoPorts,
		}).Build()
	return err
}

func importServiceInAllClusters(resource.Context) error {
	clusters := echos.Match(echo.Service(serviceB)).Clusters()
	grp := errgroup.Group{}
	for _, c := range clusters {
		c := c
		grp.Go(func() error {
			// Generate a dummy service in the cluster to reserve the ClusterSet VIP.
			clusterSetIPSvc, err := genClusterSetIPService(c)
			if err != nil {
				return err
			}

			// Create a ServiceImport in the cluster with the ClusterSet VIP.
			return createServiceImport(c, clusterSetIPSvc.Spec.ClusterIP)
		})
	}

	return grp.Wait()
}

func sendTrafficBetweenAllClusters(
	t framework.TestContext,
	fn func(t framework.TestContext, src echo.Instance, dst echo.Instances)) {
	t.Helper()
	echotest.New(t, echos).
		WithDefaultFilters().
		From(echotest.FilterMatch(echo.Service(serviceA))).
		To(echotest.FilterMatch(echo.Service(serviceB))).
		Run(fn)
}

func newServiceExport(service string) *mcs.ServiceExport {
	return &mcs.ServiceExport{
		TypeMeta: kubeMeta.TypeMeta{
			Kind:       "ServiceExport",
			APIVersion: "multicluster.x-k8s.io/v1alpha1",
		},
		ObjectMeta: kubeMeta.ObjectMeta{
			Name:      service,
			Namespace: testNS,
		},
	}
}

func checkClustersReached(t framework.TestContext, src, dest echo.Instance, clusters cluster.Clusters) {
	t.Helper()

	// Call the service using the MCS clusterset host.
	address := fmt.Sprintf("%s.%s.svc.clusterset.local",
		dest.Config().Service,
		dest.Config().Namespace.Name())

	_, err := src.CallWithRetry(echo.CallOptions{
		Address:   address,
		Target:    dest,
		Count:     50,
		PortName:  "http",
		Validator: echo.And(echo.ExpectOK(), echo.ExpectReachedClusters(clusters)),
	}, retryTimeout)
	if err != nil {
		t.Fatal(fmt.Errorf("%v\nCluster Details:\n%s", err, getClusterDetailsYAML(t, src, dest)))
	}
}

func getClusterDetailsYAML(t framework.TestContext, src, dest echo.Instance) string {
	// Add details about the configuration to the error message.
	type ClusterInfo struct {
		Cluster            string   `json:"cluster"`
		DestinationPodIPs  []string `json:"destinationPodIPs,omitempty"`
		EastWestGatewayIPs []string `json:"eastWestGatewayIPs,omitempty"`
	}

	type Details struct {
		SourceOutboundIPs []string      `json:"sourceOutboundIPs,omitempty"`
		Clusters          []ClusterInfo `json:"clusters,omitempty"`
	}
	details := Details{}

	destName := dest.Config().Service
	destNS := dest.Config().Namespace.Name()
	istioNS := istio.GetOrFail(t, t).Settings().SystemNamespace

	for _, c := range t.Clusters() {
		info := ClusterInfo{
			Cluster: c.StableName(),
		}

		// Get pod IPs for service B.
		pods, err := c.PodsForSelector(context.TODO(), destNS, "app="+destName)
		if err == nil {
			for _, destPod := range pods.Items {
				info.DestinationPodIPs = append(info.DestinationPodIPs, destPod.Status.PodIP)
			}
			sort.Strings(info.DestinationPodIPs)
		}

		// Get the East-West Gateway IP
		svc, err := c.Kube().CoreV1().Services(istioNS).Get(context.TODO(), "istio-eastwestgateway", kubeMeta.GetOptions{})
		if err == nil {
			info.EastWestGatewayIPs = append(info.EastWestGatewayIPs, svc.Spec.ExternalIPs...)
		}

		details.Clusters = append(details.Clusters, info)
	}

	// Populate the source Envoy's outbound clusters to the dest service.
	srcWorkload := src.WorkloadsOrFail(t)[0]
	envoyClusters, err := srcWorkload.Sidecar().Clusters()
	if err == nil {
		ipSet := make(map[string]struct{})
		for _, status := range envoyClusters.GetClusterStatuses() {
			if strings.Contains(status.Name, destName+"."+destNS) {
				for _, hostStatus := range status.GetHostStatuses() {
					ipSet[hostStatus.Address.GetSocketAddress().GetAddress()] = struct{}{}
				}
			}
		}
		for ip := range ipSet {
			details.SourceOutboundIPs = append(details.SourceOutboundIPs, ip)
		}
		sort.Strings(details.SourceOutboundIPs)
	}

	detailsYAML, err := yaml.Marshal(&details)
	if err != nil {
		return fmt.Sprintf("failed writing cluster details: %v", err)
	}

	return string(detailsYAML)
}

func createAndCleanupServiceExport(t framework.TestContext, service string, clusters cluster.Clusters) {
	t.Helper()
	serviceExport := newServiceExport(service)

	// Create the ServiceExports in each cluster concurrently.
	g := errgroup.Group{}
	for _, c := range clusters {
		c := c
		g.Go(func() error {
			_, err := c.MCSApis().MulticlusterV1alpha1().ServiceExports(testNS).Create(context.TODO(),
				serviceExport, kubeMeta.CreateOptions{})
			if err != nil {
				return fmt.Errorf("failed creating ServiceExport %s/%s in cluster %s: %v",
					testNS, serviceB, c.Name(), err)
			}

			return nil
		})
	}

	if err := g.Wait(); err != nil {
		t.Fatal(err)
	}

	// Add a cleanup that will delete the ServiceExports in each cluster concurrently.
	t.Cleanup(func() {
		wg := sync.WaitGroup{}
		for _, c := range clusters {
			c := c
			wg.Add(1)
			go func() {
				defer wg.Done()

				err := c.MCSApis().MulticlusterV1alpha1().ServiceExports(testNS).Delete(context.TODO(),
					serviceExport.Name, kubeMeta.DeleteOptions{})
				if err != nil {
					scopes.Framework.Warnf("failed deleting ServiceExport %s/%s in cluster %s: %v",
						testNS, serviceB, c.Name(), err)
					return
				}
			}()
		}

		wg.Wait()
	})
}

// genClusterSetIPService Generates a dummy service in order to allocate ClusterSet VIPs for
// service B in the given cluster.
func genClusterSetIPService(c cluster.Cluster) (*kubeCore.Service, error) {
	// Get the definition for service B, so we can get the ports.
	svc, err := c.CoreV1().Services(testNS).Get(context.TODO(), serviceB, kubeMeta.GetOptions{})
	if err != nil {
		return nil, err
	}

	dummySvcName := "clusterset-vip-" + serviceB
	dummySvc := &kubeCore.Service{
		ObjectMeta: kubeMeta.ObjectMeta{
			Name:      dummySvcName,
			Namespace: testNS,
			Annotations: map[string]string{
				// Export the service nowhere, so that no proxy will receive it or its VIP.
				annotation.NetworkingExportTo.Name: "~",
			},
		},
		Spec: kubeCore.ServiceSpec{
			Type:  kubeCore.ServiceTypeClusterIP,
			Ports: svc.Spec.Ports,
		},
	}

	if _, err := c.CoreV1().Services(testNS).Create(context.TODO(), dummySvc, kubeMeta.CreateOptions{}); err != nil {
		return nil, err
	}

	// Wait until a ClusterIP has been assigned.
	dummySvc = nil
	err = retry.UntilSuccess(func() error {
		var err error
		dummySvc, err = c.CoreV1().Services(testNS).Get(context.TODO(), dummySvcName, kubeMeta.GetOptions{})
		if err != nil {
			return err
		}
		if len(svc.Spec.ClusterIP) == 0 {
			return fmt.Errorf("clusterSet VIP not set for service %s/%s in cluster %s",
				testNS, dummySvcName, c.Name())
		}
		return nil
	}, retry.Timeout(10*time.Second))

	return dummySvc, err
}

func createServiceImport(c cluster.Cluster, vip string) error {
	// Get the definition for service B, so we can get the ports.
	svc, err := c.CoreV1().Services(testNS).Get(context.TODO(), serviceB, kubeMeta.GetOptions{})
	if err != nil {
		return err
	}

	// Convert the ports for the ServiceImport.
	ports := make([]mcs.ServicePort, len(svc.Spec.Ports))
	for i, p := range svc.Spec.Ports {
		ports[i] = mcs.ServicePort{
			Name:        p.Name,
			Protocol:    p.Protocol,
			Port:        p.Port,
			AppProtocol: p.AppProtocol,
		}
	}

	// Create the ServiceImport.
	_, err = c.MCSApis().MulticlusterV1alpha1().ServiceImports(testNS).Create(context.TODO(), &mcs.ServiceImport{
		ObjectMeta: kubeMeta.ObjectMeta{
			Namespace: testNS,
			Name:      serviceB,
		},
		Spec: mcs.ServiceImportSpec{
			IPs:   []string{vip},
			Type:  mcs.ClusterSetIP,
			Ports: ports,
		},
	}, kubeMeta.CreateOptions{})
	return err
}
