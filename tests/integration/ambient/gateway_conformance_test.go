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

package ambient

import (
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8ssets "k8s.io/apimachinery/pkg/util/sets" //nolint: depguard
	"sigs.k8s.io/controller-runtime/pkg/client"
	v1 "sigs.k8s.io/gateway-api/apis/v1"
	"sigs.k8s.io/gateway-api/conformance"
	confv1 "sigs.k8s.io/gateway-api/conformance/apis/v1"
	"sigs.k8s.io/gateway-api/conformance/tests"
	"sigs.k8s.io/gateway-api/conformance/utils/suite"
	gwfeatures "sigs.k8s.io/gateway-api/pkg/features"
	"sigs.k8s.io/yaml"

	"istio.io/api/label"
	"istio.io/istio/pilot/pkg/config/kube/gateway"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/maps"
	"istio.io/istio/pkg/test/env"
	"istio.io/istio/pkg/test/framework"
	ambientComponent "istio.io/istio/pkg/test/framework/components/ambient"
	"istio.io/istio/pkg/test/framework/components/cluster"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/prow"
	"istio.io/istio/pkg/test/scopes"
	"istio.io/istio/pkg/test/util/assert"
)

// GatewayConformanceInputs defines inputs to the gateway conformance test.
// The upstream build requires using `testing.T` types, which we cannot pass using our framework.
// To workaround this, we set up the inputs it TestMain.
type GatewayConformanceInputs struct {
	Client  kube.CLIClient
	Cleanup bool
	Cluster cluster.Cluster
}

var gatewayConformanceInputs GatewayConformanceInputs

// defined in sigs.k8s.io/gateway-api/conformance/base/manifests.yaml
var conformanceNamespaces = []string{
	"gateway-conformance-infra",
	"gateway-conformance-app-backend",
	"gateway-conformance-web-backend",
	"gateway-conformance-mesh",
}

var skippedTests = map[string]string{
	"BackendTLSPolicyConflictResolution": "https://github.com/istio/istio/issues/57817",
}

func TestGatewayConformance(t *testing.T) {
	framework.
		NewTest(t).
		Run(func(ctx framework.TestContext) {
			// Precreate the GatewayConformance namespaces, and apply the Image Pull Secret to them.
			if ctx.Settings().Image.PullSecret != "" {
				for _, ns := range conformanceNamespaces {
					namespace.Claim(ctx, namespace.Config{
						Prefix: ns,
						Inject: false,
					})
				}
			}

			mapper, _ := gatewayConformanceInputs.Client.UtilFactory().ToRESTMapper()
			clientOptions := client.Options{
				Scheme: kube.IstioScheme,
				Mapper: mapper,
			}
			c, err := client.New(gatewayConformanceInputs.Client.RESTConfig(), clientOptions)
			if err != nil {
				t.Fatal(err)
			}

			hostnameType := v1.AddressType("Hostname")
			istioVersion, _ := env.ReadVersion()
			supported := gateway.SupportedFeatures.Clone().Delete(gwfeatures.MeshConsumerRouteFeature)
			opts := suite.ConformanceOptions{
				Client:                   c,
				ClientOptions:            clientOptions,
				Clientset:                gatewayConformanceInputs.Client.Kube(),
				RestConfig:               gatewayConformanceInputs.Client.RESTConfig(),
				GatewayClassName:         "istio",
				Debug:                    scopes.Framework.DebugEnabled(),
				CleanupBaseResources:     gatewayConformanceInputs.Cleanup,
				ManifestFS:               []fs.FS{&conformance.Manifests},
				SupportedFeatures:        gwfeatures.SetsToNamesSet(supported),
				SkipTests:                maps.Keys(skippedTests),
				UsableNetworkAddresses:   []v1.GatewaySpecAddress{{Value: "infra-backend-v1.gateway-conformance-infra.svc.cluster.local", Type: &hostnameType}},
				UnusableNetworkAddresses: []v1.GatewaySpecAddress{{Value: "foo", Type: &hostnameType}},
				ConformanceProfiles: k8ssets.New(
					suite.GatewayHTTPConformanceProfileName,
					suite.GatewayTLSConformanceProfileName,
					suite.GatewayGRPCConformanceProfileName,
					suite.MeshHTTPConformanceProfileName,
				),
				Implementation: confv1.Implementation{
					Organization: "istio",
					Project:      "istio",
					URL:          "https://istio.io/",
					Version:      istioVersion,
					Contact:      []string{"@istio/maintainers"},
				},
				NamespaceLabels: map[string]string{
					label.IoIstioDataplaneMode.Name: "ambient",
				},
				TimeoutConfig: ctx.Settings().GatewayConformanceTimeoutConfig,
			}

			ctx.Cleanup(func() {
				if !ctx.Failed() {
					return
				}
				if ctx.Settings().CIMode {
					for _, ns := range conformanceNamespaces {
						namespace.Dump(ctx, ns)
					}
				}
			})
			csuite, err := suite.NewConformanceTestSuite(opts)
			assert.NoError(t, err)
			csuite.Setup(t, tests.ConformanceTests)

			// remove the dataplane mode label from the gateway-conformance-infra namespace
			// so that the ingress gateway doesn't get captured
			ns, err := namespace.Claim(ctx, namespace.Config{
				Prefix: "gateway-conformance-infra",
				Inject: false,
			})
			if err != nil {
				t.Fatal(err)
			}
			ns.RemoveLabel(label.IoIstioDataplaneMode.Name)

			// create a waypoint for mesh conformance
			meshNS := namespace.Static("gateway-conformance-mesh")
			cls := gatewayConformanceInputs.Cluster
			ambientComponent.NewWaypointProxyOrFailForCluster(ctx, meshNS, "namespace", cls)

			// TODO: Should we even run this in multiple clusters?
			concreteNS, err := cls.Kube().CoreV1().Namespaces().Get(ctx.Context(), meshNS.Name(), metav1.GetOptions{})
			if err != nil {
				t.Fatal(err)
			}
			labels := concreteNS.Labels
			if labels == nil {
				labels = make(map[string]string)
			}
			labels[label.IoIstioUseWaypoint.Name] = "namespace"
			concreteNS.Labels = labels
			cls.Kube().CoreV1().Namespaces().Update(ctx.Context(), concreteNS, metav1.UpdateOptions{})

			assert.NoError(t, csuite.Run(t, tests.ConformanceTests))
			report, err := csuite.Report()
			assert.NoError(t, err)
			reportb, err := yaml.Marshal(report)
			assert.NoError(t, err)
			fp := filepath.Join(ctx.Settings().BaseDir, "conformance.yaml")
			t.Logf("writing conformance test to %v (%v)", fp, prow.ArtifactsURL(fp))
			assert.NoError(t, os.WriteFile(fp, reportb, 0o644))
		})
}
