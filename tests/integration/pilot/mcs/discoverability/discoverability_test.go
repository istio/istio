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
	"io/ioutil"
	"sync"
	"testing"
	"time"

	"golang.org/x/sync/errgroup"
	v12 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/mcs-api/pkg/apis/v1alpha1"

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
		Setup(installServiceExportCRD).
		Setup(istio.Setup(&i, enableMCSServiceDiscovery)).
		Setup(deployEchos).
		Run()
}

func TestClusterLocal(t *testing.T) {
	framework.NewTest(t).
		Features("traffic.mcs.servicediscovery").
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

func installServiceExportCRD(t resource.Context) error {
	crd, err := ioutil.ReadFile("../../testdata/mcs-serviceexport-crd.yaml")
	if err != nil {
		return err
	}
	return t.Config().ApplyYAMLNoCleanup("", string(crd))
}

func enableMCSServiceDiscovery(_ resource.Context, cfg *istio.Config) {
	cfg.ControlPlaneValues = `
values:
  pilot:
    env:
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

func newServiceExport(service string) *v1alpha1.ServiceExport {
	return &v1alpha1.ServiceExport{
		TypeMeta: v12.TypeMeta{
			Kind:       "ServiceExport",
			APIVersion: "multicluster.x-k8s.io/v1alpha1",
		},
		ObjectMeta: v12.ObjectMeta{
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

	src.CallWithRetryOrFail(t, echo.CallOptions{
		Address:   address,
		Target:    dest,
		Count:     50,
		PortName:  "http",
		Validator: echo.And(echo.ExpectOK(), echo.ExpectReachedClusters(clusters)),
	}, retryTimeout)
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
				serviceExport, v12.CreateOptions{})
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
					serviceExport.Name, v12.DeleteOptions{})
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
