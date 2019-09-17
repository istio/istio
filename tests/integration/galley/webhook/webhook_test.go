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

package galley

import (
	"testing"
	"time"

	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/environment"
	"istio.io/istio/pkg/test/framework/components/environment/kube"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/label"
)

var (
	vwcName    = "istio-galley"
	deployName = "istio-galley"
	sleepDelay = 10 * time.Second // How long to wait to give the reconcile loop an opportunity to act
)

var i istio.Instance

// Using subtests to enforce ordering
func TestWebhook(t *testing.T) {
	framework.NewTest(t).
		// Limit to Kube environment as we're testing integration of webhook with K8s.
		RequiresEnvironment(environment.Kube).
		Run(func(ctx framework.TestContext) {
			env := ctx.Environment().(*kube.Environment)

			istioNs := i.Settings().IstioNamespace

			// Verify galley re-creates the webhook when it is deleted.
			ctx.NewSubTest("recreateOnDelete").
				Run(func(ctx framework.TestContext) {
					startVersion := getVwcResourceVersion(vwcName, t, env)

					if err := env.DeleteValidatingWebhook(vwcName); err != nil {
						t.Fatalf("Error deleting validation webhook: %v", err)
					}

					time.Sleep(sleepDelay)
					version := getVwcResourceVersion(vwcName, t, env)
					if version == startVersion {
						t.Fatalf("ValidatingWebhookConfiguration was not re-created after deletion")
					}
				})

			// Verify that scaling up/down doesn't modify webhook configuration
			ctx.NewSubTest("scaling").
				Run(func(ctx framework.TestContext) {
					startGen := getVwcGeneration(vwcName, t, env)

					// Scale up
					scaleDeployment(istioNs, deployName, 2, t, env)
					// Wait a bit to give the ValidatingWebhookConfiguration reconcile loop an opportunity to act
					time.Sleep(sleepDelay)
					gen := getVwcGeneration(vwcName, t, env)
					if gen != startGen {
						t.Fatalf("ValidatingWebhookConfiguration was updated unexpectedly on scale up to 2")
					}

					// Scale down to zero
					scaleDeployment(istioNs, deployName, 0, t, env)
					// Wait a bit to give the ValidatingWebhookConfiguration reconcile loop an opportunity to act
					time.Sleep(sleepDelay)
					gen = getVwcGeneration(vwcName, t, env)
					if gen != startGen {
						t.Fatalf("ValidatingWebhookConfiguration was updated unexpectedly on scale down to zero")
					}

					// Scale back to 1
					scaleDeployment(istioNs, deployName, 1, t, env)
					// Wait a bit to give the ValidatingWebhookConfiguration reconcile loop an opportunity to act
					time.Sleep(sleepDelay)
					gen = getVwcGeneration(vwcName, t, env)
					if gen != startGen {
						t.Fatalf("ValidatingWebhookConfiguration was updated unexpectedly on scale up back to 1")
					}
				})

			// Verify that scaling up/down doesn't modify webhook configuration
			ctx.NewSubTest("scaling").
				Run(func(ctx framework.TestContext) {
					startGen := getVwcGeneration(vwcName, t, env)

					// Scale up
					scaleDeployment(istioNs, deployName, 2, t, env)
					// Wait a bit to give the ValidatingWebhookConfiguration reconcile loop an opportunity to act
					time.Sleep(sleepDelay)
					gen := getVwcGeneration(vwcName, t, env)
					if gen != startGen {
						t.Fatalf("ValidatingWebhookConfiguration was updated unexpectedly on scale up to 2")
					}

					// Scale down to zero
					scaleDeployment(istioNs, deployName, 0, t, env)
					// Wait a bit to give the ValidatingWebhookConfiguration reconcile loop an opportunity to act
					time.Sleep(sleepDelay)
					gen = getVwcGeneration(vwcName, t, env)
					if gen != startGen {
						t.Fatalf("ValidatingWebhookConfiguration was updated unexpectedly on scale down to zero")
					}

					// Scale back to 1
					scaleDeployment(istioNs, deployName, 1, t, env)
					// Wait a bit to give the ValidatingWebhookConfiguration reconcile loop an opportunity to act
					time.Sleep(sleepDelay)
					gen = getVwcGeneration(vwcName, t, env)
					if gen != startGen {
						t.Fatalf("ValidatingWebhookConfiguration was updated unexpectedly on scale up back to 1")
					}
				})

			// NOTE: Keep this as the last test! It deletes the istio-system namespaces. All subsequent kube tests will fail.
			// Verify that removing galley's namespace results in the webhook configuration being removed
			ctx.NewSubTest("webhookUninstall").
				Run(func(ctx framework.TestContext) {
					// Remove galley's namespace
					env.DeleteNamespace(istioNs)

					// Verify webhook config is deleted
					env.WaitForValidatingWebhookDeletion(vwcName)
				})
		})
}

func scaleDeployment(namespace, deployment string, replicas int, t *testing.T, env *kube.Environment) {
	env.ScaleDeployment(namespace, deployment, replicas)
	if err := env.ScaleDeployment(namespace, deployment, replicas); err != nil {
		t.Fatalf("Error scaling deployment %s to %d: %v", deployment, replicas, err)
	}
	if err := env.WaitUntilDeploymentIsReady(namespace, deployment); err != nil {
		t.Fatalf("Error waiting for deployment %s to be ready: %v", deployment, err)
	}
}

func getVwcGeneration(vwcName string, t *testing.T, env *kube.Environment) int64 {
	vwc, err := env.GetValidatingWebhookConfiguration(vwcName)
	if err != nil {
		t.Fatalf("Could not get validating webhook webhook config %s: %v", vwcName, err)
	}
	return vwc.GetGeneration()
}

func getVwcResourceVersion(vwcName string, t *testing.T, env *kube.Environment) string {
	vwc, err := env.GetValidatingWebhookConfiguration(vwcName)
	if err != nil {
		t.Fatalf("Could not get validating webhook webhook config %s: %v", vwcName, err)
	}
	return vwc.GetResourceVersion()
}

func TestMain(m *testing.M) {
	framework.
		NewSuite("webhook_test", m).
		Label(label.CustomSetup).
		SetupOnEnv(environment.Kube, istio.Setup(&i, nil)).
		Run()
}
