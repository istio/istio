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
	"context"
	"io/fs"
	"path/filepath"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8ssets "k8s.io/apimachinery/pkg/util/sets" //nolint: depguard
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/gateway-api-inference-extension/conformance"
	inferenceconfig "sigs.k8s.io/gateway-api-inference-extension/conformance/utils/config"
	confv1 "sigs.k8s.io/gateway-api/conformance/apis/v1"
	confsuite "sigs.k8s.io/gateway-api/conformance/utils/suite"

	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/maps"
	"istio.io/istio/pkg/test/env"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/crd"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/scopes"
)

// GatewayInferenceConformanceInputs defines inputs to the gateway inference conformance test.
// The upstream build requires using `testing.T` types, which we cannot pass using our framework.
// To workaround this, we set up the inputs in TestMain.
type GatewayInferenceConformanceInputs struct {
	Client  kube.CLIClient
	Cleanup bool
}

var gatewayInferenceConformanceInputs GatewayInferenceConformanceInputs

// defined in sigs.k8s.io/gateway-api-inference-extension/conformance/resources/base.yaml
var inferenceConformanceNamespaces = []string{
	"gateway-conformance-infra",
	"gateway-conformance-app-backend",
}

var skippedInferenceTests = map[string]string{
	"EppUnAvailableFailOpen": "EppUnAvailableFailOpen is not supported in Istio/Envoy, see https://github.com/kubernetes-sigs/gateway-api-inference-extension/issues/1266",
}

func TestGatewayInferenceConformance(t *testing.T) {
	framework.
		NewTest(t).
		Run(func(ctx framework.TestContext) {

			crd.DeployGatewayAPIOrSkip(ctx)
			crd.DeployGatewayAPIInferenceExtensionOrSkip(ctx)

			// Precreate the Gateway Inference Conformance namespaces, and apply the Image Pull Secret to them.
			if ctx.Settings().Image.PullSecret != "" {
				for _, ns := range inferenceConformanceNamespaces {
					namespace.Claim(ctx, namespace.Config{
						Prefix: ns,
						Inject: false,
					})
				}
			}

			// Precreate the Gateway Inference Conformance namespaces so we can configure DestinationRules
			for _, ns := range inferenceConformanceNamespaces {
				ctx.Clusters().Default().Kube().CoreV1().Namespaces().Create(context.Background(), &corev1.Namespace{
					ObjectMeta: metav1.ObjectMeta{
						Name: ns,
					},
				}, metav1.CreateOptions{})
			}

			// Deploy istio specific resources required to run test suite, destionation rules for tls
			err := ctx.Clusters().Default().ApplyYAMLFiles("", filepath.Join(env.IstioSrc, "tests/integration/pilot/testdata/gateway-api-inference-extension-dr.yaml"))
			if err != nil {
				t.Fatalf("Failed to deploy inference extension conformance test istio specific resources: %v", err)
			}

			mapper, _ := gatewayInferenceConformanceInputs.Client.UtilFactory().ToRESTMapper()
			clientOptions := client.Options{
				Scheme: kube.IstioScheme,
				Mapper: mapper,
			}

			c, err := client.New(gatewayInferenceConformanceInputs.Client.RESTConfig(), clientOptions)
			if err != nil {
				t.Fatal(err)
			}

			istioVersion, _ := env.ReadVersion()
			opts := confsuite.ConformanceOptions{
				Client:               c,
				Clientset:            gatewayInferenceConformanceInputs.Client.Kube(),
				ClientOptions:        clientOptions,
				RestConfig:           gatewayInferenceConformanceInputs.Client.RESTConfig(),
				GatewayClassName:     "istio",
				BaseManifests:        "resources/base.yaml",
				SupportedFeatures:    k8ssets.New(conformance.GatewayLayerProfile.CoreFeatures.UnsortedList()...),
				ConformanceProfiles:  k8ssets.New(conformance.GatewayLayerProfileName),
				Debug:                scopes.Framework.DebugEnabled(),
				CleanupBaseResources: gatewayInferenceConformanceInputs.Cleanup,
				ManifestFS:           []fs.FS{&conformance.Manifests},
				SkipTests:            maps.Keys(skippedInferenceTests),
				ReportOutputPath:     filepath.Join(ctx.Settings().BaseDir, "inference-conformance.yaml"),
				Implementation: confv1.Implementation{
					Organization: "istio",
					Project:      "istio",
					URL:          "https://istio.io/",
					Version:      istioVersion,
					Contact:      []string{"@istio/maintainers"},
				},
				TimeoutConfig: inferenceconfig.DefaultInferenceExtensionTimeoutConfig().TimeoutConfig,
			}
			if rev := ctx.Settings().Revisions.Default(); rev != "" {
				opts.NamespaceLabels = map[string]string{
					"istio.io/rev": rev,
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
					for _, ns := range inferenceConformanceNamespaces {
						namespace.Dump(ctx, ns)
					}
				}
			})

			conformance.RunConformanceWithOptions(t, opts)
		})
}
