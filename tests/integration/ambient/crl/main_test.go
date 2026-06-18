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

package crl

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
	ambientControlPlaneValues = `
values:
  pilot:
    env:
      ENABLE_CA_CRL: "true"
  cni:
    repair:
      enabled: false
  ztunnel:
    terminationGracePeriodSeconds: 5
    logLevel: debug
    env:
      SECRET_TTL: 5m
    peerCaCrl:
      enabled: true
`
)

var (
	certBundle    *crlutil.RootBundle
	clientNS      namespace.Instance
	serverNS      namespace.Instance
	client        echo.Instance
	server        echo.Instance
	clientCluster cluster.Cluster
	serverCluster cluster.Cluster
)

func TestMain(m *testing.M) {
	framework.
		NewSuite(m).
		Label(testlabel.CustomSetup).
		SkipIf("test suite requires at least two clusters", func(ctx resource.Context) bool { return len(ctx.AllClusters()) < 2 }).
		Setup(func(ctx resource.Context) error {
			var err error
			// generate shared root CA and one intermediate bundle per cluster, installed via cacerts secret
			certBundle, err = crlutil.GenerateCaCerts(ctx)
			if err != nil {
				return err
			}
			// register cleanup of k8s resources (derived from GenerateCaCerts) that istiod distributes to every ns so we don't pollute other test suites
			ctx.Cleanup(func() {
				log.Info("cleaning up istio-ca-crl and istio-ca-root-cert ConfigMaps")
				for _, cl := range ctx.Clusters() {
					nsList, err := cl.Kube().CoreV1().Namespaces().List(context.TODO(), metav1.ListOptions{})
					if err != nil {
						return
					}
					for _, ns := range nsList.Items {
						_ = cl.Kube().CoreV1().ConfigMaps(ns.Name).Delete(context.TODO(), features.CRLConfigMapName, metav1.DeleteOptions{})
						_ = cl.Kube().CoreV1().ConfigMaps(ns.Name).Delete(context.TODO(), features.CACertConfigMapName, metav1.DeleteOptions{})
					}
				}
			})
			return nil
		}).
		Setup(istio.Setup(nil, func(ctx resource.Context, cfg *istio.Config) {
			ctx.Settings().Ambient = true
			cfg.EnableCNI = true
			cfg.DeployEastWestGW = true
			cfg.DeployGatewayAPI = true
			cfg.SkipDeployCrossClusterSecrets = false
			cfg.ControlPlaneValues = ambientControlPlaneValues

			if ctx.Settings().AmbientMultiNetwork {
				cfg.Values["pilot.env.AMBIENT_ENABLE_MULTI_NETWORK"] = "true"
				cfg.Values["pilot.env.AMBIENT_ENABLE_MULTI_NETWORK_INGRESS"] = "true"
				cfg.Values["pilot.env.AMBIENT_ENABLE_BAGGAGE"] = "true"
			}
		}, nil)).
		SetupParallel(
			namespace.Setup(&clientNS, namespace.Config{
				Prefix: "ambient-client",
				Inject: false,
				Labels: map[string]string{
					label.IoIstioDataplaneMode.Name: "ambient",
				},
			}),
			namespace.Setup(&serverNS, namespace.Config{
				Prefix: "ambient-server",
				Inject: false,
				Labels: map[string]string{
					label.IoIstioDataplaneMode.Name: "ambient",
				},
			}),
		).
		Setup(deployApps).
		Run()
}

func deployApps(ctx resource.Context) error {
	clusters := ctx.AllClusters()
	clientCluster = clusters[0]
	serverCluster = clusters[1]

	_, err := deployment.New(ctx).
		With(&client, echo.Config{
			Service:        "ambient-client",
			Namespace:      clientNS,
			Cluster:        clientCluster,
			ServiceAccount: true,
			ServiceLabels:  map[string]string{"istio.io/global": "true"},
			Ports: []echo.Port{
				{
					Name:         "http",
					Protocol:     protocol.HTTP,
					WorkloadPort: 8080,
				},
			},
		}).
		With(&server, echo.Config{
			Service:        "ambient-server",
			Namespace:      serverNS,
			Cluster:        serverCluster,
			ServiceAccount: true,
			ServiceLabels:  map[string]string{"istio.io/global": "true"},
			Ports: []echo.Port{
				{
					Name:         "http",
					Protocol:     protocol.HTTP,
					WorkloadPort: 8080,
				},
			},
		}).
		Build()
	if err != nil {
		return fmt.Errorf("failed to deploy echo apps: %v", err)
	}
	return nil
}
