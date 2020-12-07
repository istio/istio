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

package extauthzcustomaction

import (
	"testing"

	"istio.io/istio/pkg/test/echo/common/scheme"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/echoboot"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/kube"
	"istio.io/istio/pkg/test/util/file"
	"istio.io/istio/pkg/test/util/tmpl"
	"istio.io/istio/tests/integration/security/util"
	"istio.io/istio/tests/integration/security/util/connection"
	rbacUtil "istio.io/istio/tests/integration/security/util/rbac_util"
)

var (
	// The extAuthzServiceNamespace namespace is used to deploy the sample ext-authz server.
	extAuthzServiceNamespace    namespace.Instance
	extAuthzServiceNamespaceErr error
)

// TestAuthorization_Custom tests that the CUSTOM action with the sample ext_authz server.
func TestAuthorization_Custom(t *testing.T) {
	framework.NewTest(t).
		Features("security.authorization.custom").
		Run(func(ctx framework.TestContext) {
			ns := namespace.NewOrFail(t, ctx, namespace.Config{
				Prefix: "v1beta1-custom",
				Inject: true,
			})
			args := map[string]string{"Namespace": ns.Name()}
			applyYAML := func(filename string, ns namespace.Instance) []string {
				policy := tmpl.EvaluateAllOrFail(t, args, file.AsStringOrFail(t, filename))
				ctx.Config().ApplyYAMLOrFail(t, ns.Name(), policy...)
				return policy
			}

			// Deploy and wait for the ext-authz server to be ready.
			if extAuthzServiceNamespace == nil {
				ctx.Fatalf("Failed to create namespace for ext-authz server: %v", extAuthzServiceNamespaceErr)
			}
			extAuthzServer := applyYAML("../../../../samples/extauthz/ext-authz.yaml", extAuthzServiceNamespace)
			defer ctx.Config().DeleteYAMLOrFail(t, extAuthzServiceNamespace.Name(), extAuthzServer...)
			if _, _, err := kube.WaitUntilServiceEndpointsAreReady(ctx.Clusters().Default(), extAuthzServiceNamespace.Name(), "ext-authz"); err != nil {
				ctx.Fatalf("Wait for ext-authz server failed: %v", err)
			}

			policy := applyYAML("../testdata/authz/v1beta1-custom.yaml.tmpl", ns)
			defer ctx.Config().DeleteYAMLOrFail(t, ns.Name(), policy...)

			var a, b, c, d, e echo.Instance
			echoConfig := func(name string, includeExtAuthz bool) echo.Config {
				cfg := util.EchoConfig(name, ns, false, nil, nil)
				cfg.IncludeExtAuthz = includeExtAuthz
				return cfg
			}
			echoboot.NewBuilder(ctx).
				With(&a, echoConfig("a", false)).
				With(&b, echoConfig("b", false)).
				With(&c, echoConfig("c", false)).
				With(&d, echoConfig("d", true)).
				With(&e, echoConfig("e", true)).
				BuildOrFail(t)

			newTestCase := func(target echo.Instance, path string, header string, expectAllowed bool) rbacUtil.TestCase {
				return rbacUtil.TestCase{
					Request: connection.Checker{
						From: a,
						Options: echo.CallOptions{
							Target:   target,
							PortName: "http",
							Scheme:   scheme.HTTP,
							Path:     path,
						},
					},
					Headers:       map[string]string{"x-ext-authz": header},
					ExpectAllowed: expectAllowed,
				}
			}
			// Path "/custom" is protected by ext-authz service and is accessible with the header `x-ext-authz: allow`.
			// Path "/health" is not protected and is accessible to public.
			cases := []rbacUtil.TestCase{
				// workload b is using an ext-authz service in its own pod of HTTP API.
				newTestCase(b, "/custom", "allow", true),
				newTestCase(b, "/custom", "deny", false),
				newTestCase(b, "/health", "allow", true),
				newTestCase(b, "/health", "deny", true),

				// workload c is using an ext-authz service in its own pod of gRPC API.
				newTestCase(c, "/custom", "allow", true),
				newTestCase(c, "/custom", "deny", false),
				newTestCase(c, "/health", "allow", true),
				newTestCase(c, "/health", "deny", true),

				// workload d is using an local ext-authz service in the same pod as the application of HTTP API.
				newTestCase(d, "/custom", "allow", true),
				newTestCase(d, "/custom", "deny", false),
				newTestCase(d, "/health", "allow", true),
				newTestCase(d, "/health", "deny", true),

				// workload e is using an local ext-authz service in the same pod as the application of gRPC API.
				newTestCase(e, "/custom", "allow", true),
				newTestCase(e, "/custom", "deny", false),
				newTestCase(e, "/health", "allow", true),
				newTestCase(e, "/health", "deny", true),
			}

			rbacUtil.RunRBACTest(ctx, cases)
		})
}
