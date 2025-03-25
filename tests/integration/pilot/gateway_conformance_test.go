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

package pilot

import (
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	k8ssets "k8s.io/apimachinery/pkg/util/sets" //nolint: depguard
	"sigs.k8s.io/controller-runtime/pkg/client"
	v1 "sigs.k8s.io/gateway-api/apis/v1"
	"sigs.k8s.io/gateway-api/conformance"
	confv1 "sigs.k8s.io/gateway-api/conformance/apis/v1"
	"sigs.k8s.io/gateway-api/conformance/tests"
	"sigs.k8s.io/gateway-api/conformance/utils/suite"
	"sigs.k8s.io/gateway-api/pkg/features"
	"sigs.k8s.io/yaml"

	"istio.io/istio/pilot/pkg/config/kube/gateway"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/maps"
	"istio.io/istio/pkg/test/env"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/crd"
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
}

var gatewayConformanceInputs GatewayConformanceInputs

// defined in sigs.k8s.io/gateway-api/conformance/base/manifests.yaml
var conformanceNamespaces = []string{
	"gateway-conformance-infra",
	"gateway-conformance-mesh",
	"gateway-conformance-mesh-consumer",
	"gateway-conformance-app-backend",
	"gateway-conformance-web-backend",
}

var skippedTests = map[string]string{}

func TestGatewayConformance(t *testing.T) {
	framework.
		NewTest(t).
		Run(func(ctx framework.TestContext) {
			crd.DeployGatewayAPIOrSkip(ctx)

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

			supportedFeatures := gateway.SupportedFeatures.Clone().
				Delete(features.MeshClusterIPMatchingFeature) // https://github.com/istio/istio/issues/44702
			if ctx.Settings().GatewayConformanceStandardOnly {
				for f := range supportedFeatures {
					if f.Channel != features.FeatureChannelStandard {
						supportedFeatures.Delete(f)
					}
				}
			}
			hostnameType := v1.AddressType("Hostname")
			istioVersion, _ := env.ReadVersion()
			opts := suite.ConformanceOptions{
				Client:                   c,
				Clientset:                gatewayConformanceInputs.Client.Kube(),
				ClientOptions:            clientOptions,
				RestConfig:               gatewayConformanceInputs.Client.RESTConfig(),
				GatewayClassName:         "istio",
				Debug:                    scopes.Framework.DebugEnabled(),
				CleanupBaseResources:     gatewayConformanceInputs.Cleanup,
				ManifestFS:               []fs.FS{&conformance.Manifests},
				SupportedFeatures:        features.SetsToNamesSet(supportedFeatures),
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
				TimeoutConfig: ctx.Settings().GatewayConformanceTimeoutConfig,
			}
			if rev := ctx.Settings().Revisions.Default(); rev != "" {
				opts.NamespaceLabels = map[string]string{
					"istio.io/rev": rev,
				}
			} else {
				opts.NamespaceLabels = map[string]string{
					"istio-injection": "enabled",
				}
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
