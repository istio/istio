//go:build integ

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

package trustdomain

import (
	"context"
	"fmt"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"istio.io/api/label"
	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/log"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/cluster"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/deployment"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/components/namespace"
	testlabel "istio.io/istio/pkg/test/framework/label"
	"istio.io/istio/pkg/test/framework/resource"
	crlutil "istio.io/istio/tests/integration/security/crl/util"
)

const (
	clientTrustDomain = "domain1.local"
	serverTrustDomain = "domain2.local"
)

var (
	testNamespace namespace.Instance
	clientCluster cluster.Cluster
	serverCluster cluster.Cluster
	client        echo.Instance
	servers       echo.Instances
)

func TestMain(m *testing.M) {
	framework.
		NewSuite(m).
		Label(testlabel.CustomSetup).
		Label(testlabel.Full).
		SkipIf("test suite requires at least two clusters on different networks", func(ctx resource.Context) bool {
			_, remote := crossNetworkClusters(ctx.AllClusters())
			return remote == nil
		}).
		Setup(func(ctx resource.Context) error {
			if _, err := crlutil.GenerateCaCerts(ctx); err != nil {
				return err
			}
			ctx.Cleanup(func() {
				log.Info("cleaning up trust domain and CA ConfigMaps")
				for _, cl := range ctx.Clusters() {
					nsList, err := cl.Kube().CoreV1().Namespaces().List(context.TODO(), metav1.ListOptions{})
					if err != nil {
						return
					}
					for _, ns := range nsList.Items {
						_ = cl.Kube().CoreV1().ConfigMaps(ns.Name).Delete(context.TODO(), features.TrustDomainsConfigMapName, metav1.DeleteOptions{})
						_ = cl.Kube().CoreV1().ConfigMaps(ns.Name).Delete(context.TODO(), features.CACertConfigMapName, metav1.DeleteOptions{})
						_ = cl.Kube().CoreV1().ConfigMaps(ns.Name).Delete(context.TODO(), features.CRLConfigMapName, metav1.DeleteOptions{})
					}
				}
			})
			return nil
		}).
		Setup(istio.Setup(nil, func(ctx resource.Context, cfg *istio.Config) {
			ctx.Settings().Ambient = true
			ctx.Settings().AmbientMultiNetwork = true
			cfg.EnableCNI = true
			cfg.DeployEastWestGW = true
			cfg.DeployGatewayAPI = true
			cfg.SkipDeployCrossClusterSecrets = false
			cfg.ControlPlaneValues = controlPlaneValues(clientTrustDomain, serverTrustDomain)
			cfg.RemoteClusterValues = controlPlaneValues(serverTrustDomain, clientTrustDomain)
			cfg.Values["pilot.env.AMBIENT_ENABLE_MULTI_NETWORK"] = "true"
			cfg.Values["pilot.env.AMBIENT_ENABLE_MULTI_NETWORK_INGRESS"] = "true"
			cfg.Values["pilot.env.AMBIENT_ENABLE_BAGGAGE"] = "true"
		}, nil)).
		Setup(namespace.Setup(&testNamespace, namespace.Config{
			Prefix: "ambient-trust-domain",
			Inject: false,
			Labels: map[string]string{
				label.IoIstioDataplaneMode.Name: "ambient",
			},
		})).
		Setup(deployApps).
		Run()
}

func controlPlaneValues(trustDomain, alias string) string {
	return fmt.Sprintf(`
meshConfig:
  trustDomain: %s
  trustDomainAliases: [%s]
values:
  cni:
    repair:
      enabled: false
  ztunnel:
    terminationGracePeriodSeconds: 5
    env:
      SECRET_TTL: 5m
`, trustDomain, alias)
}

func crossNetworkClusters(clusters cluster.Clusters) (cluster.Cluster, cluster.Cluster) {
	if len(clusters) < 2 {
		return nil, nil
	}
	for _, remote := range clusters[1:] {
		if clusters[0].NetworkName() != remote.NetworkName() {
			return clusters[0], remote
		}
	}
	return nil, nil
}

func deployApps(ctx resource.Context) error {
	clientCluster, serverCluster = crossNetworkClusters(ctx.AllClusters())

	instances, err := deployment.New(ctx).
		WithConfig(echo.Config{
			Service:        "client",
			Namespace:      testNamespace,
			Cluster:        clientCluster,
			ServiceAccount: true,
			Ports:          []echo.Port{{Name: "http", Protocol: protocol.HTTP, WorkloadPort: 8080}},
		}).
		WithConfig(echo.Config{
			Service:        "server",
			Namespace:      testNamespace,
			Cluster:        clientCluster,
			ServiceAccount: true,
			ServiceLabels:  map[string]string{"istio.io/global": "true"},
			Ports:          []echo.Port{{Name: "http", Protocol: protocol.HTTP, WorkloadPort: 8080}},
			Subsets:        []echo.SubsetConfig{{Version: "v1", Replicas: 0}},
		}).
		WithConfig(echo.Config{
			Service:        "server",
			Namespace:      testNamespace,
			Cluster:        serverCluster,
			ServiceAccount: true,
			ServiceLabels:  map[string]string{"istio.io/global": "true"},
			Ports:          []echo.Port{{Name: "http", Protocol: protocol.HTTP, WorkloadPort: 8080}},
		}).
		Build()
	if err != nil {
		return fmt.Errorf("failed to deploy echo apps: %w", err)
	}
	client = instances[0]
	servers = instances[1:]
	return nil
}
