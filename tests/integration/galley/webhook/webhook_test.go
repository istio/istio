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
	"fmt"
	"strings"
	"testing"
	"time"

	k8serrors "k8s.io/apimachinery/pkg/api/errors"

	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/environment"
	"istio.io/istio/pkg/test/framework/components/environment/kube"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/label"
	"istio.io/istio/pkg/test/util/retry"
)

const (
	webhookControllerApp = "pilot"
	deployName           = "istiod"
	clusterRolePrefix    = "istiod"
	vwcName              = "istiod-istio-system"
)

var (
	sleepDelay = 10 * time.Second // How long to wait to give the reconcile loop an opportunity to act
	i          istio.Instance
)

// Using subtests to enforce ordering
func TestWebhook(t *testing.T) {
	framework.NewTest(t).
		// Limit to Kube environment as we're testing integration of webhook with K8s.
		RequiresEnvironment(environment.Kube).
		Run(func(ctx framework.TestContext) {
			env := ctx.Environment().(*kube.Environment)

			istioNs := i.Settings().IstioNamespace

			// Verify the controller re-creates the webhook when it is deleted.
			ctx.NewSubTest("recreateOnDelete").
				Run(func(t framework.TestContext) {
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
				Run(func(t framework.TestContext) {
					startGen := getVwcGeneration(t, env)

					// Scale up
					scaleDeployment(istioNs, deployName, 2, t, env)
					// Wait a bit to give the ValidatingWebhookConfiguration reconcile loop an opportunity to act
					time.Sleep(sleepDelay)
					gen := getVwcGeneration(t, env)
					if gen != startGen {
						t.Fatalf("ValidatingWebhookConfiguration was updated unexpectedly on scale up to 2")
					}

					// Scale down to zero
					scaleDeployment(istioNs, deployName, 0, t, env)
					// Wait a bit to give the ValidatingWebhookConfiguration reconcile loop an opportunity to act
					time.Sleep(sleepDelay)
					gen = getVwcGeneration(t, env)
					if gen != startGen {
						t.Fatalf("ValidatingWebhookConfiguration was updated unexpectedly on scale down to zero")
					}

					// Scale back to 1
					scaleDeployment(istioNs, deployName, 1, t, env)
					// Wait a bit to give the ValidatingWebhookConfiguration reconcile loop an opportunity to act
					time.Sleep(sleepDelay)
					gen = getVwcGeneration(t, env)
					if gen != startGen {
						t.Fatalf("ValidatingWebhookConfiguration was updated unexpectedly on scale up back to 1")
					}
				})

			// NOTE: Keep this as the last test! It deletes the webhook's clusterrole. All subsequent kube tests will fail.
			// Verify that removing clusterrole results in the webhook configuration being removed.
			ctx.NewSubTest("webhookUninstall").
				Run(func(t framework.TestContext) {
					if err := env.DeleteClusterRole(fmt.Sprintf("%v-%v", clusterRolePrefix, istioNs)); err != nil {
						t.Fatal(err)
					}

					// Verify webhook config is deleted
					if err := env.WaitForValidatingWebhookDeletion(vwcName); err != nil {
						t.Fatal(err)
					}
				})
		})
}

func scaleDeployment(namespace, deployment string, replicas int, t test.Failer, env *kube.Environment) {
	if err := env.ScaleDeployment(namespace, deployment, replicas); err != nil {
		t.Fatalf("Error scaling deployment %s to %d: %v", deployment, replicas, err)
	}
	if err := env.WaitUntilDeploymentIsReady(namespace, deployment); err != nil {
		t.Fatalf("Error waiting for deployment %s to be ready: %v", deployment, err)
	}

	// verify no pods are still terminating
	if replicas == 0 {
		fetchFunc := env.Accessor.NewSinglePodFetch(namespace, fmt.Sprintf("app=%v", webhookControllerApp))
		retry.UntilSuccessOrFail(t, func() error {
			pods, err := fetchFunc()
			if err != nil {
				if k8serrors.IsNotFound(err) || strings.Contains(err.Error(), "no matching pod found for selectors") {
					return nil
				}
				return err
			}
			if len(pods) > 0 {
				return fmt.Errorf("%v pods remaining", len(pods))
			}
			return nil
		}, retry.Timeout(5*time.Minute))
	}
}

func getVwcGeneration(t test.Failer, env *kube.Environment) int64 {
	t.Helper()

	vwc, err := env.GetValidatingWebhookConfiguration(vwcName)
	if err != nil {
		t.Fatalf("Could not get validating webhook webhook config %s: %v", vwcName, err)
	}
	return vwc.GetGeneration()
}

func getVwcResourceVersion(vwcName string, t test.Failer, env *kube.Environment) string {
	t.Helper()

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
