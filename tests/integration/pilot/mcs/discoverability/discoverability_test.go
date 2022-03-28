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
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	envoy_admin_v3 "github.com/envoyproxy/go-control-plane/envoy/admin/v3"
	"golang.org/x/sync/errgroup"
	kubeCore "k8s.io/api/core/v1"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	kubeMeta "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	mcsapi "sigs.k8s.io/mcs-api/pkg/apis/v1alpha1"
	"sigs.k8s.io/yaml"

	"istio.io/api/annotation"
	kube "istio.io/istio/pilot/pkg/serviceregistry/kube/controller"
	"istio.io/istio/pkg/kube/mcs"
	echoClient "istio.io/istio/pkg/test/echo"
	"istio.io/istio/pkg/test/echo/check"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/cluster"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/echotest"
	"istio.io/istio/pkg/test/framework/components/echo/match"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/label"
	"istio.io/istio/pkg/test/framework/resource"
	"istio.io/istio/pkg/test/scopes"
	"istio.io/istio/pkg/test/util/retry"
	"istio.io/istio/tests/integration/pilot/mcs/common"
)

type hostType string

func (ht hostType) String() string {
	return string(ht)
}

const (
	hostTypeClusterLocal    hostType = "cluster.local"
	hostTypeClusterSetLocal hostType = "clusterset.local"

	requestCountMultiplier = 20
)

var (
	i     istio.Instance
	echos common.EchoDeployment

	retryTimeout = retry.Timeout(1 * time.Minute)
	retryDelay   = retry.Delay(500 * time.Millisecond)

	hostTypes = []hostType{hostTypeClusterSetLocal, hostTypeClusterLocal}

	serviceA = match.ServiceName(echo.NamespacedName{Name: common.ServiceA, Namespace: echos.Namespace})
	serviceB = match.ServiceName(echo.NamespacedName{Name: common.ServiceB, Namespace: echos.Namespace})
)

func TestMain(m *testing.M) {
	framework.
		NewSuite(m).
		Label(label.CustomSetup).
		RequireMinVersion(17).
		RequireMinClusters(2).
		Setup(common.InstallMCSCRDs).
		Setup(istio.Setup(&i, enableMCSServiceDiscovery)).
		Setup(common.DeployEchosFunc("mcs", &echos)).
		Run()
}

func TestClusterLocal(t *testing.T) {
	framework.NewTest(t).
		Features("traffic.mcs.servicediscovery").
		RequireIstioVersion("1.11").
		Run(func(t framework.TestContext) {
			// Don't export service B in any cluster. All requests should stay in-cluster.

			for _, ht := range hostTypes {
				t.NewSubTest(ht.String()).Run(func(t framework.TestContext) {
					runForAllClusterCombinations(t, func(t framework.TestContext, from echo.Instance, to echo.Target) {
						var checker check.Checker
						if ht == hostTypeClusterLocal {
							// For calls to cluster.local, ensure that all requests stay in the same cluster
							expectedClusters := cluster.Clusters{from.Config().Cluster}
							checker = checkClustersReached(expectedClusters)
						} else {
							// For calls to clusterset.local, we should fail DNS lookup. The clusterset.local host
							// is only available for a service when it is exported in at least one cluster.
							checker = checkDNSLookupFailed()
						}
						callAndValidate(t, ht, from, to, checker)
					})
				})
			}
		})
}

func TestMeshWide(t *testing.T) {
	framework.NewTest(t).
		Features("traffic.mcs.servicediscovery").
		Run(func(t framework.TestContext) {
			// Export service B in all clusters.
			createAndCleanupServiceExport(t, common.ServiceB, t.Clusters())

			for _, ht := range hostTypes {
				t.NewSubTest(ht.String()).Run(func(t framework.TestContext) {
					runForAllClusterCombinations(t, func(t framework.TestContext, from echo.Instance, to echo.Target) {
						var expectedClusters cluster.Clusters
						if ht == hostTypeClusterLocal {
							// Ensure that all requests to cluster.local stay in the same cluster
							expectedClusters = cluster.Clusters{from.Config().Cluster}
						} else {
							// Ensure that requests to clusterset.local reach all destination clusters.
							expectedClusters = to.Clusters()
						}
						callAndValidate(t, ht, from, to, checkClustersReached(expectedClusters))
					})
				})
			}
		})
}

func TestServiceExportedInOneCluster(t *testing.T) {
	framework.NewTest(t).
		Features("traffic.mcs.servicediscovery").
		Run(func(t framework.TestContext) {
			t.Skip("https://github.com/istio/istio/issues/34051")
			// Get all the clusters where service B resides.
			bClusters := serviceB.GetMatches(echos.Instances).Clusters()

			// Test exporting service B exclusively in each cluster.
			for _, exportCluster := range bClusters {
				exportCluster := exportCluster
				t.NewSubTestf("b exported in %s", exportCluster.StableName()).
					Run(func(t framework.TestContext) {
						// Export service B in the export cluster.
						createAndCleanupServiceExport(t, common.ServiceB, cluster.Clusters{exportCluster})

						for _, ht := range hostTypes {
							t.NewSubTest(ht.String()).Run(func(t framework.TestContext) {
								runForAllClusterCombinations(t, func(t framework.TestContext, from echo.Instance, to echo.Target) {
									var expectedClusters cluster.Clusters
									if ht == hostTypeClusterLocal {
										// Ensure that all requests to cluster.local stay in the same cluster
										expectedClusters = cluster.Clusters{from.Config().Cluster}
									} else {
										// Since we're exporting only the endpoints in the exportCluster, depending
										// on where we call service B from, we'll reach a different set of endpoints.
										// If we're calling from exportCluster, it will be the same as cluster-local
										// (i.e. we'll only reach endpoints in exportCluster). From all other clusters,
										// we should reach endpoints in that cluster AND exportCluster.
										expectedClusters = cluster.Clusters{exportCluster}
										if from.Config().Cluster.Name() != exportCluster.Name() {
											expectedClusters = append(expectedClusters, from.Config().Cluster)
										}
									}
									callAndValidate(t, ht, from, to, checkClustersReached(expectedClusters))
								})
							})
						}
					})
			}
		})
}

func enableMCSServiceDiscovery(t resource.Context, cfg *istio.Config) {
	cfg.ControlPlaneValues = fmt.Sprintf(`
values:
  pilot:
    env:
      PILOT_USE_ENDPOINT_SLICE: "true"
      ENABLE_MCS_SERVICE_DISCOVERY: "true"
      ENABLE_MCS_HOST: "true"
      ENABLE_MCS_CLUSTER_LOCAL: "true"
      MCS_API_GROUP: %s
      MCS_API_VERSION: %s`,
		common.KubeSettings(t).MCSAPIGroup,
		common.KubeSettings(t).MCSAPIVersion)
}

func runForAllClusterCombinations(
	t framework.TestContext,
	fn func(t framework.TestContext, from echo.Instance, to echo.Target)) {
	t.Helper()
	echotest.New(t, echos.Instances).
		WithDefaultFilters().
		FromMatch(serviceA).
		ToMatch(serviceB).
		Run(fn)
}

func newServiceExport(service string, serviceExportGVR schema.GroupVersionResource) *mcsapi.ServiceExport {
	return &mcsapi.ServiceExport{
		TypeMeta: kubeMeta.TypeMeta{
			Kind:       "ServiceExport",
			APIVersion: serviceExportGVR.GroupVersion().String(),
		},
		ObjectMeta: kubeMeta.ObjectMeta{
			Name:      service,
			Namespace: echos.Namespace.Name(),
		},
	}
}

func checkClustersReached(clusters cluster.Clusters) check.Checker {
	return check.And(
		check.OK(),
		check.ReachedClusters(clusters))
}

func checkDNSLookupFailed() check.Checker {
	return check.And(
		check.Error(),
		func(_ echoClient.Responses, err error) error {
			if strings.Contains(err.Error(), "no such host") {
				return nil
			}
			return err
		})
}

func callAndValidate(t framework.TestContext, ht hostType, from echo.Instance, to echo.Target, checker check.Checker) {
	t.Helper()

	var address string
	if ht == hostTypeClusterSetLocal {
		// Call the service using the MCS ClusterSet host.
		address = to.Config().ClusterSetLocalFQDN()
	} else {
		address = to.Config().ClusterLocalFQDN()
	}

	_, err := from.Call(echo.CallOptions{
		Address: address,
		To:      to,
		Count:   requestCountMultiplier * to.WorkloadsOrFail(t).Len(),
		Port: echo.Port{
			Name: "http",
		},
		Check: checker,
		Retry: echo.Retry{
			Options: []retry.Option{retryDelay, retryTimeout},
		},
	})
	if err != nil {
		t.Fatalf("failed calling host %s: %v\nCluster Details:\n%s", address, err,
			getClusterDetailsYAML(t, address, from, to))
	}
}

func getClusterDetailsYAML(t framework.TestContext, address string, from echo.Instance, to echo.Target) string {
	// Add details about the configuration to the error message.
	type IPs struct {
		Cluster   string   `json:"cluster"`
		TargetPod []string `json:"targetPod"`
		Gateway   []string `json:"gateway"`
	}

	type Outbound struct {
		ClusterName string                         `json:"clusterName"`
		IP          string                         `json:"ip"`
		Stats       []*envoy_admin_v3.SimpleMetric `json:"stats"`
	}

	type Details struct {
		From     string     `json:"from"`
		To       string     `json:"to"`
		Outbound []Outbound `json:"outbound"`
		IPs      []IPs      `json:"ips"`
	}
	details := Details{
		From: from.Config().Cluster.Name(),
		To:   address,
	}

	destName := to.Config().Service
	destNS := to.Config().Namespace.Name()
	istioNS := istio.GetOrFail(t, t).Settings().SystemNamespace

	for _, c := range t.Clusters() {
		info := IPs{
			Cluster: c.StableName(),
		}

		// Get pod IPs for service B.
		pods, err := c.PodsForSelector(context.TODO(), destNS, "app="+destName)
		if err == nil {
			for _, destPod := range pods.Items {
				info.TargetPod = append(info.TargetPod, destPod.Status.PodIP)
			}
			sort.Strings(info.TargetPod)
		}

		// Get the East-West Gateway IP
		svc, err := c.Kube().CoreV1().Services(istioNS).Get(context.TODO(), "istio-eastwestgateway", kubeMeta.GetOptions{})
		if err == nil {
			var ips []string
			for _, ingress := range svc.Status.LoadBalancer.Ingress {
				ips = append(ips, ingress.IP)
			}
			info.Gateway = append(info.Gateway, ips...)
		}

		details.IPs = append(details.IPs, info)
	}

	// Populate the source Envoy's outbound clusters to the dest service.
	srcWorkload := from.WorkloadsOrFail(t)[0]
	envoyClusters, err := srcWorkload.Sidecar().Clusters()
	if err == nil {
		for _, hostName := range []string{to.Config().ClusterLocalFQDN(), to.Config().ClusterSetLocalFQDN()} {
			clusterName := fmt.Sprintf("outbound|80||%s", hostName)

			for _, status := range envoyClusters.GetClusterStatuses() {
				if status.Name == clusterName {
					for _, hostStatus := range status.GetHostStatuses() {
						details.Outbound = append(details.Outbound, Outbound{
							ClusterName: clusterName,
							IP:          hostStatus.Address.GetSocketAddress().GetAddress(),
							Stats:       hostStatus.Stats,
						})
					}
				}
			}
		}
	}

	detailsYAML, err := yaml.Marshal(&details)
	if err != nil {
		return fmt.Sprintf("failed writing cluster details: %v", err)
	}

	return string(detailsYAML)
}

func createAndCleanupServiceExport(t framework.TestContext, service string, exportClusters cluster.Clusters) {
	t.Helper()

	start := time.Now()
	scopes.Framework.Infof("=== BEGIN: Create ServiceExport%v ===", exportClusters.Names())

	serviceExportGVR := common.KubeSettings(t).ServiceExportGVR()
	serviceImportGVR := common.KubeSettings(t).ServiceImportGVR()

	serviceExport := newServiceExport(service, serviceExportGVR)

	u, err := runtime.DefaultUnstructuredConverter.ToUnstructured(serviceExport)
	if err != nil {
		t.Fatal(err)
	}

	// Create the ServiceExports in each cluster concurrently.
	g := errgroup.Group{}
	for _, c := range exportClusters {
		c := c
		g.Go(func() error {
			_, err := c.Dynamic().Resource(serviceExportGVR).Namespace(echos.Namespace.Name()).Create(context.TODO(),
				&unstructured.Unstructured{Object: u}, kubeMeta.CreateOptions{})
			if err != nil {
				return fmt.Errorf("failed creating %s with name %s/%s in cluster %s: %v",
					serviceExportGVR.String(), echos.Namespace, common.ServiceB, c.Name(), err)
			}

			return nil
		})
	}

	// Now wait for ServiceImport to be created
	importClusters := serviceA.GetMatches(echos.Instances).Clusters()
	if common.IsMCSControllerEnabled(t) {
		scopes.Framework.Infof("Waiting for the MCS Controller to create ServiceImport in each cluster")
		for _, c := range importClusters {
			c := c
			serviceImports := c.Dynamic().Resource(serviceImportGVR).Namespace(echos.Namespace.Name())

			g.Go(func() error {
				return retry.UntilSuccess(func() error {
					si, err := serviceImports.Get(context.TODO(), common.ServiceB, kubeMeta.GetOptions{})
					if err != nil {
						return fmt.Errorf("failed waiting for ServiceImport %s/%s in cluster %s: %v",
							echos.Namespace, common.ServiceB, c.Name(), err)
					}

					ips := kube.GetServiceImportIPs(si)
					if len(ips) == 0 {
						return fmt.Errorf("no ClusterSet IP for ServiceImport %s/%s in cluster %s",
							echos.Namespace, common.ServiceB, c.Name())
					}
					return nil
				}, retry.Timeout(5*time.Minute)) // GKE has a significant delay in creating ServiceImport.
			})
		}
	} else {
		scopes.Framework.Infof("No MCS Controller running. Manually creating ServiceImport in each cluster")
		for _, c := range importClusters {
			c := c
			g.Go(func() error {
				// Generate a dummy service in the cluster to reserve the ClusterSet VIP.
				clusterSetIPSvc, err := genClusterSetIPService(c)
				if err != nil {
					return err
				}

				// Create a ServiceImport in the cluster with the ClusterSet VIP.
				return createServiceImport(c, clusterSetIPSvc.Spec.ClusterIP, serviceImportGVR)
			})
		}
	}

	err = g.Wait()
	status := "success"
	if err != nil {
		status = "failed"
	}

	end := time.Now()
	scopes.Framework.Infof("=== DONE (%s): Create ServiceExport%v (%v) ===", status, exportClusters.Names(), end.Sub(start))
	if err != nil {
		t.Fatal(err)
	}

	// Add a cleanup that will delete the ServiceExports in each cluster concurrently.
	t.Cleanup(func() {
		wg := sync.WaitGroup{}
		for _, c := range exportClusters {
			c := c
			wg.Add(1)
			go func() {
				defer wg.Done()

				err := c.Dynamic().Resource(mcs.ServiceExportGVR).Namespace(echos.Namespace.Name()).Delete(context.TODO(),
					serviceExport.Name, kubeMeta.DeleteOptions{})
				if err != nil && !kerrors.IsAlreadyExists(err) {
					scopes.Framework.Warnf("failed deleting ServiceExport %s/%s in cluster %s: %v",
						echos.Namespace, common.ServiceB, c.Name(), err)
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
	svc, err := c.CoreV1().Services(echos.Namespace.Name()).Get(context.TODO(), common.ServiceB, kubeMeta.GetOptions{})
	if err != nil {
		return nil, err
	}

	dummySvcName := "clusterset-vip-" + common.ServiceB
	dummySvc := &kubeCore.Service{
		ObjectMeta: kubeMeta.ObjectMeta{
			Name:      dummySvcName,
			Namespace: echos.Namespace.Name(),
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

	ns := echos.Namespace.Name()
	if _, err := c.CoreV1().Services(ns).Create(context.TODO(), dummySvc, kubeMeta.CreateOptions{}); err != nil && !kerrors.IsAlreadyExists(err) {
		return nil, err
	}

	// Wait until a ClusterIP has been assigned.
	dummySvc = nil
	err = retry.UntilSuccess(func() error {
		var err error
		dummySvc, err = c.CoreV1().Services(echos.Namespace.Name()).Get(context.TODO(), dummySvcName, kubeMeta.GetOptions{})
		if err != nil {
			return err
		}
		if len(svc.Spec.ClusterIP) == 0 {
			return fmt.Errorf("clusterSet VIP not set for service %s/%s in cluster %s",
				echos.Namespace, dummySvcName, c.Name())
		}
		return nil
	}, retry.Timeout(10*time.Second))

	return dummySvc, err
}

func createServiceImport(c cluster.Cluster, vip string, serviceImportGVR schema.GroupVersionResource) error {
	// Get the definition for service B, so we can get the ports.
	svc, err := c.CoreV1().Services(echos.Namespace.Name()).Get(context.TODO(), common.ServiceB, kubeMeta.GetOptions{})
	if err != nil {
		return err
	}

	// Convert the ports for the ServiceImport.
	ports := make([]mcsapi.ServicePort, len(svc.Spec.Ports))
	for i, p := range svc.Spec.Ports {
		ports[i] = mcsapi.ServicePort{
			Name:        p.Name,
			Protocol:    p.Protocol,
			Port:        p.Port,
			AppProtocol: p.AppProtocol,
		}
	}

	serviceImport := &mcsapi.ServiceImport{
		TypeMeta: kubeMeta.TypeMeta{
			Kind:       "ServiceImport",
			APIVersion: serviceImportGVR.GroupVersion().String(),
		},
		ObjectMeta: kubeMeta.ObjectMeta{
			Namespace: echos.Namespace.Name(),
			Name:      common.ServiceB,
		},
		Spec: mcsapi.ServiceImportSpec{
			IPs:   []string{vip},
			Type:  mcsapi.ClusterSetIP,
			Ports: ports,
		},
	}

	u, err := runtime.DefaultUnstructuredConverter.ToUnstructured(serviceImport)
	if err != nil {
		panic(err)
	}

	// Create the ServiceImport.
	_, err = c.Dynamic().Resource(serviceImportGVR).Namespace(echos.Namespace.Name()).Create(
		context.TODO(), &unstructured.Unstructured{Object: u}, kubeMeta.CreateOptions{})
	if err != nil && !kerrors.IsAlreadyExists(err) {
		return err
	}
	return nil
}
