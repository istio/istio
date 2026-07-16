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

package agentgateway

import (
	"io/fs"
	"os"
	"path/filepath"
	"testing"

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
// To workaround this, we set up the inputs in TestMain.
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

var skippedTests = map[string]string{
	// The following tests were added in v1.5.0
	"BackendTLSPolicyObservedGenerationBump": "TODO",

	"GatewayBackendClientCertificateFeature":                     "TODO",
	"GatewayFrontendInvalidDefaultClientCertificateValidation":   "TODO",
	"GatewayInvalidTLSBackendConfiguration":                      "TODO",
	"GatewayTLSBackendClientCertificate":                         "TODO",
	"GatewayFrontendClientCertificateValidationInsecureFallback": "TODO",
	"GatewayFrontendClientCertificateValidation":                 "TODO",
	"GatewayInvalidFrontendClientCertificateValidation":          "TODO",

	"HTTPRouteHTTPSListenerDetectMisdirectedRequests": "TODO",

	// The following tests were modified between v1.4.0 && v1.5.0
	"BackendTLSPolicy": "TODO",

	// The following tests were added in v1.5.0
	"TLSRouteTerminateSimpleSameNamespace":  "TODO",
	"TLSRouteMixedTerminationSameNamespace": "TODO",

	// The following tests were added in v1.6.0
	"GatewayInvalidParametersRef":        "TODO",
	"GatewayListenerUnsupportedProtocol": "TODO",
	"TCPRouteWeightedRouting":            "TODO: flaky in dual-stack and multicluster environments",

	// agentgateway does not yet route TCPRoute data-plane traffic, so the
	// traffic-based TCPRoute conformance tests fail. The status/validation-only
	// TCPRouteInvalid* tests still run and pass, so they are not skipped.
	"TCPRouteMultipleRoutesAttachment":    "TODO: agentgateway does not yet support TCPRoute traffic",
	"TCPRouteParentRefAttachAll":          "TODO: agentgateway does not yet support TCPRoute traffic",
	"TCPRouteParentRefPortAndSectionName": "TODO: agentgateway does not yet support TCPRoute traffic",
	"TCPRouteReferenceGrant":              "TODO: agentgateway does not yet support TCPRoute traffic",
}

func TestGatewayConformanceAgentgateway(t *testing.T) {
	framework.
		NewTest(t).
		Run(func(ctx framework.TestContext) {
			if !ctx.Settings().Agentgateway {
				ctx.Skip("Only run agentgateway conformance tests when explicitly enabled")
			}
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

			clientOptions := client.Options{
				Scheme: kube.IstioScheme,
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
				ConfigurableOptions: suite.ConfigurableOptions{
					GatewayClassName:         "istio-agentgateway",
					Debug:                    scopes.Framework.DebugEnabled(),
					CleanupBaseResources:     gatewayConformanceInputs.Cleanup,
					CleanupTestResources:     gatewayConformanceInputs.Cleanup,
					SupportedFeatures:        features.SetsToNamesSet(supportedFeatures).UnsortedList(),
					SkipTests:                maps.Keys(skippedTests),
					UsableNetworkAddresses:   []v1.GatewaySpecAddress{{Value: "infra-backend-v1.gateway-conformance-infra.svc.cluster.local", Type: &hostnameType}},
					UnusableNetworkAddresses: []v1.GatewaySpecAddress{{Value: "foo", Type: &hostnameType}},
					ConformanceProfiles: []suite.ConformanceProfileName{
						suite.GatewayHTTPConformanceProfileName,
						suite.GatewayTLSConformanceProfileName,
						suite.GatewayGRPCConformanceProfileName,
						suite.GatewayTCPConformanceProfileName,
						suite.MeshHTTPConformanceProfileName,
					},
					Implementation: confv1.Implementation{
						Organization: "istio",
						Project:      "istio",
						URL:          "https://istio.io/",
						Version:      istioVersion,
						Contact:      []string{"@istio/maintainers"},
					},
					TimeoutConfig: ctx.Settings().GatewayConformanceTimeoutConfig,
				},
				Client:        c,
				Clientset:     gatewayConformanceInputs.Client.Kube(),
				ClientOptions: clientOptions,
				RestConfig:    gatewayConformanceInputs.Client.RESTConfig(),
				ManifestFS:    []fs.FS{&conformance.Manifests},
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
			if ctx.Settings().GatewayConformanceAllowCRDsMismatch {
				opts.AllowCRDsMismatch = true
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
			fp := filepath.Join(ctx.Settings().BaseDir, "istio-agentgateway-conformance.yaml")
			t.Logf("writing conformance test to %v (%v)", fp, prow.ArtifactsURL(fp))
			assert.NoError(t, os.WriteFile(fp, reportb, 0o644))
		})
}
