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
	"fmt"
	"strings"
	"testing"

	apiextensions "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/gateway-api/conformance/tests"
	"sigs.k8s.io/gateway-api/conformance/utils/suite"

	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/framework/resource/config/apply"
	"istio.io/istio/pkg/test/scopes"
	"istio.io/istio/pkg/test/util/retry"
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
	"gateway-conformance-app-backend",
	"gateway-conformance-web-backend",
}

var skippedTests = map[string]string{}

func TestGatewayConformance(t *testing.T) {
	// nolint: staticcheck
	framework.
		NewTest(t).
		RequiresSingleCluster().
		Features("traffic.gateway").
		Run(func(ctx framework.TestContext) {
			DeployGatewayAPICRD(ctx)

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

			opts := suite.Options{
				Client:               c,
				GatewayClassName:     "istio",
				Debug:                scopes.Framework.DebugEnabled(),
				CleanupBaseResources: gatewayConformanceInputs.Cleanup,
				SupportedFeatures:    []suite.SupportedFeature{suite.SupportReferenceGrant},
			}
			if rev := ctx.Settings().Revisions.Default(); rev != "" {
				opts.NamespaceLabels = map[string]string{
					"istio.io/rev": rev,
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

			for _, ct := range tests.ConformanceTests {
				t.Run(ct.ShortName, func(t *testing.T) {
					if reason, f := skippedTests[ct.ShortName]; f {
						t.Skip(reason)
					}
					ct.Run(t, csuite)
				})
			}
		})
}

func DeployGatewayAPICRD(ctx framework.TestContext) {
	if !supportsGatewayAPI(ctx) {
		ctx.Skip("Not supported; requires CRDv1 support.")
	}
	if err := ctx.ConfigIstio().
		File("", "testdata/gateway-api-crd.yaml").
		Apply(apply.NoCleanup); err != nil {
		ctx.Fatal(err)
	}
	// Wait until our GatewayClass is ready
	retry.UntilSuccessOrFail(ctx, func() error {
		for _, c := range ctx.Clusters().Configs() {
			_, err := c.GatewayAPI().GatewayV1alpha2().GatewayClasses().Get(context.Background(), "istio", metav1.GetOptions{})
			if err != nil {
				return err
			}
			crdl, err := c.Ext().ApiextensionsV1().CustomResourceDefinitions().List(context.Background(), metav1.ListOptions{})
			if err != nil {
				return err
			}
			for _, crd := range crdl.Items {
				if !strings.HasSuffix(crd.Name, "gateway.networking.k8s.io") {
					continue
				}
				found := false
				for _, c := range crd.Status.Conditions {
					if c.Type == apiextensions.Established && c.Status == apiextensions.ConditionTrue {
						found = true
					}
				}
				if !found {
					return fmt.Errorf("crd %v not ready: %+v", crd.Name, crd.Status)
				}
			}
		}
		return nil
	})
}
