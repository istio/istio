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

package mixer

import (
	"testing"

	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/environment"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/label"
)

func TestK8sDeployment(t *testing.T) {
	// Test to ensure that deployment in K8s environment completes. We can extend this suite in the future
	// to include more Mixer/K8s integration tests. In that case, it may make sense to move the deployment
	// logic into TestMain.
	framework.NewTest(t).Run(func(ctx framework.TestContext) {
		cfg := istio.DefaultConfigOrFail(ctx, ctx)
		cfg.Values["global.useMCP"] = "false"
		cfg.Values["galley.enabled"] = "false"
		cfg.SkipWaitForValidationWebhook = true

		_, err := istio.Deploy(ctx, &cfg)
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
	})
}

func TestMain(m *testing.M) {
	framework.NewSuite("mixer_k8s_test", m).
		RequireEnvironment(environment.Kube).
		Label(label.CustomSetup). // This test deploys without Galley & MCP.
		Run()
}
