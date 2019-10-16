// Copyright 2019 Istio Authors
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

package istioio

import (
	"os/exec"
	"testing"

	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/istioio"
	"istio.io/istio/pkg/test/scopes"
)

const (
	ns = "default"
)

// TestAuthzHTTP simulates the task in https://www.istio.io/docs/tasks/security/authz-http/
func TestAuthzHTTP(t *testing.T) {
	framework.
		NewTest(t).
		Run(func(ctx framework.TestContext) {

			// Clean up everything after all the subtests run.
			defer func() {
				cmd := exec.Command("./scripts/authz/clean.sh")
				output, err := cmd.CombinedOutput()
				if err != nil {
					scopes.CI.Errorf("cleanup failed: %s", string(output))
				}
			}()

			ctx.NewSubTest("Setup").Run(istioio.NewBuilder().
				RunScript(istioio.File("scripts/authz/setup.sh"), nil).
				Apply(ns, istioio.IstioSrc("samples/bookinfo/platform/kube/bookinfo.yaml")).
				Apply(ns, istioio.IstioSrc("samples/bookinfo/networking/bookinfo-gateway.yaml")).
				Apply(ns, istioio.IstioSrc("samples/sleep/sleep.yaml")).
				RunScript(istioio.File("scripts/authz/wait.sh"), nil).
				Build())

			ctx.NewSubTest("Enabling Istio authorization").Run(istioio.NewBuilder().
				Apply("", istioio.IstioSrc("samples/bookinfo/platform/kube/rbac/rbac-config-ON.yaml")).
				RunScript(istioio.File("scripts/authz/verify-enablingIstioAuthorization.sh"), nil).
				Build())

			ctx.NewSubTest("Enforcing Namespace-level access control").Run(istioio.NewBuilder().
				Apply(ns, istioio.IstioSrc("samples/bookinfo/platform/kube/rbac/namespace-policy.yaml")).
				RunScript(istioio.File("scripts/authz/verify-enforcingNamespaceLevelAccessControl.sh"), nil).
				Delete(ns, istioio.IstioSrc("samples/bookinfo/platform/kube/rbac/namespace-policy.yaml")).
				RunScript(istioio.File("scripts/authz/verify-enablingIstioAuthorization.sh"), nil).
				Build())

			ctx.NewSubTest("Enforcing Service-level access control Step 1").Run(istioio.NewBuilder().
				Apply(ns, istioio.IstioSrc("samples/bookinfo/platform/kube/rbac/productpage-policy.yaml")).
				RunScript(istioio.File("scripts/authz/verify-enforcingServiceLevelAccessControlStep1.sh"), nil).
				Build())

			ctx.NewSubTest("Enforcing Service-level access control Step 2").Run(istioio.NewBuilder().
				Apply(ns, istioio.IstioSrc("samples/bookinfo/platform/kube/rbac/details-reviews-policy.yaml")).
				RunScript(istioio.File("scripts/authz/verify-enforcingServiceLevelAccessControlStep2.sh"), nil).
				Build())

			ctx.NewSubTest("Enforcing Service-level access control Step 3").Run(istioio.NewBuilder().
				Apply(ns, istioio.IstioSrc("samples/bookinfo/platform/kube/rbac/ratings-policy.yaml")).
				RunScript(istioio.File("scripts/authz/verify-enforcingServiceLevelAccessControlStep3.sh"), nil).
				Build())
		})
}
