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
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/gateway-api/conformance/tests"
	"sigs.k8s.io/gateway-api/conformance/utils/suite"

	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/resource/config/apply"
	"istio.io/istio/pkg/test/scopes"
	"istio.io/istio/pkg/test/util/retry"
)

// GatewayConformanceInputs defines inputs to the gateway conformance test.
// The upstream build requires using `testing.T` types, which we cannot pass using our framework.
// To workaround this, we set up the inputs it TestMain.
type GatewayConformanceInputs struct {
	Client  kube.ExtendedClient
	Cleanup bool
}

var gatewayConformanceInputs GatewayConformanceInputs

func TestGatewayConformance(t *testing.T) {
	// nolint: staticcheck
	framework.
		NewTest(t).
		RequiresSingleCluster().
		Features("traffic.gateway").
		Run(func(ctx framework.TestContext) {
			if !supportsCRDv1(ctx) {
				t.Skip("Not supported; requires CRDv1 support.")
			}
			if err := ctx.ConfigIstio().
				File("", "testdata/gateway-api-crd.yaml").
				Apply(apply.NoCleanup); err != nil {
				ctx.Fatal(err)
			}
			// Wait until our GatewayClass is ready
			retry.UntilSuccessOrFail(ctx, func() error {
				_, err := ctx.Clusters().Default().GatewayAPI().GatewayV1alpha2().GatewayClasses().Get(context.Background(), "istio", metav1.GetOptions{})
				return err
			})

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
			}
			if rev := ctx.Settings().Revisions.Default(); rev != "" {
				opts.NamespaceLabels = map[string]string{
					"istio.io/rev": rev,
				}
			}
			csuite := suite.New(opts)
			csuite.Setup(t)

			for _, ct := range tests.ConformanceTests {
				t.Run(ct.ShortName, func(t *testing.T) {
					ct.Run(t, csuite)
				})
			}
		})
}
