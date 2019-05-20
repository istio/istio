//  Copyright 2019 Istio Authors
//
//  Licensed under the Apache License, Version 2.0 (the "License");
//  you may not use this file except in compliance with the License.
//  You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
//  Unless required by applicable law or agreed to in writing, software
//  distributed under the License is distributed on an "AS IS" BASIS,
//  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//  See the License for the specific language governing permissions and
//  limitations under the License.

// Package healthcheck contains a test to support kubernetes app health check when mTLS is turned on.
// https://github.com/istio/istio/issues/9150.
package healthcheck

import (
	"testing"

	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/echoboot"
	"istio.io/istio/pkg/test/framework/components/environment"
	"istio.io/istio/pkg/test/framework/components/namespace"
)

// TestMtlsHealthCheck verifies Kubernetes HTTP health check can work when mTLS
// is enabled.
// Currently this test can only pass on Prow with a real GKE cluster, and fail
// on CircleCI with a Minikube. For more details, see https://github.com/istio/istio/issues/12754.
func TestMtlsHealthCheck(t *testing.T) {
	framework.NewTest(t).
		RequiresEnvironment(environment.Kube).
		Run(func(ctx framework.TestContext) {

			ns := namespace.ClaimOrFail(t, ctx, "default")

			// Apply the policy.
			policyYAML := `apiVersion: "authentication.istio.io/v1alpha1"
kind: "Policy"
metadata:
  name: "mtls-strict-for-healthcheck"
spec:
  targets:
  - name: "healthcheck"
  peers:
    - mtls:
        mode: STRICT
`
			g.ApplyConfigOrFail(t, ns, policyYAML)
			defer g.DeleteConfigOrFail(t, ns, policyYAML)

			var healthcheck echo.Instance
			echoboot.NewBuilderOrFail(t, ctx).
				With(&healthcheck, echo.Config{
					Service: "healthcheck",
					Pilot:   p,
					Galley:  g,
				}).
				BuildOrFail(t)

			// TODO(incfly): add a negative test once we have a per deployment annotation support for this feature.
		})
}
