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
	"testing"

	k8ssets "k8s.io/apimachinery/pkg/util/sets" //nolint: depguard
	"sigs.k8s.io/controller-runtime/pkg/client"
	v1 "sigs.k8s.io/gateway-api/apis/v1"
	"sigs.k8s.io/gateway-api/conformance/tests"
	"sigs.k8s.io/gateway-api/conformance/utils/suite"

	"istio.io/istio/pilot/pkg/config/kube/gateway"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/maps"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/crd"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/scopes"
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

var skippedTests = map[string]string{
	"MeshFrontendHostname": "https://github.com/istio/istio/issues/44702",
}

func TestGatewayConformance(t *testing.T) {
	framework.
		NewTest(t).
		Features("traffic.gateway").
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
			c, err := client.New(gatewayConformanceInputs.Client.RESTConfig(), client.Options{
				Scheme: kube.IstioScheme,
				Mapper: mapper,
			})
			if err != nil {
				t.Fatal(err)
			}

			features := gateway.SupportedFeatures
			if ctx.Settings().GatewayConformanceStandardOnly {
				features = k8ssets.New[suite.SupportedFeature]().
					Insert(suite.GatewayExtendedFeatures.UnsortedList()...).
					Insert(suite.ReferenceGrantCoreFeatures.UnsortedList()...).
					Insert(suite.HTTPRouteCoreFeatures.UnsortedList()...).
					Insert(suite.HTTPRouteExtendedFeatures.UnsortedList()...).
					Insert(suite.MeshCoreFeatures.UnsortedList()...)
			}
			hostnameType := v1.AddressType("Hostname")
			opts := suite.Options{
				Client:                   c,
				Clientset:                gatewayConformanceInputs.Client.Kube(),
				RestConfig:               gatewayConformanceInputs.Client.RESTConfig(),
				GatewayClassName:         "istio",
				Debug:                    scopes.Framework.DebugEnabled(),
				CleanupBaseResources:     gatewayConformanceInputs.Cleanup,
				SupportedFeatures:        features,
				SkipTests:                maps.Keys(skippedTests),
				UsableNetworkAddresses:   []v1.GatewayAddress{{Value: "infra-backend-v1.gateway-conformance-infra.svc.cluster.local", Type: &hostnameType}},
				UnusableNetworkAddresses: []v1.GatewayAddress{{Value: "foo", Type: &hostnameType}},
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

			csuite := suite.New(opts)
			csuite.Setup(t)
			csuite.Run(t, tests.ConformanceTests)
		})
}
