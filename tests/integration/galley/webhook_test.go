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
	"testing"
	"time"

	kubeApiAdmission "k8s.io/api/admissionregistration/v1beta1"

	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/environment"
	"istio.io/istio/pkg/test/framework/components/environment/kube"
)

const (
	vwcName = "istiod-istio-system"
)

func TestWebhook(t *testing.T) {
	framework.NewTest(t).
		// Limit to Kube environment as we're testing integration of webhook with K8s.
		RequiresEnvironment(environment.Kube).
		Run(func(ctx framework.TestContext) {
			env := ctx.Environment().(*kube.Environment)

			got, err := env.GetValidatingWebhookConfiguration(vwcName)
			if err != nil {
				t.Fatalf("error getting initial webhook: %v", err)
			}
			if err := verifyValidatingWebhookConfiguration(got); err != nil {
				t.Fatal(err)
			}

			// clear the updated fields and verify istiod updates them
			updated := got.DeepCopyObject().(*kubeApiAdmission.ValidatingWebhookConfiguration)
			updated.Webhooks[0].ClientConfig.CABundle = nil
			updated.Webhooks[0].FailurePolicy = &([]kubeApiAdmission.FailurePolicyType{kubeApiAdmission.Ignore}[0])

			if err := env.UpdateValidatingWebhookConfiguration(updated); err != nil {
				t.Fatalf("could not clear reconciled fields in config: %v", err)
			}

			timeout := time.After(30 * time.Second)
			for {
				select {
				case <-timeout:
					t.Fatal("timed out waiting for istiod to update validatingwebhookconfiguration")
				default:
					got, err = env.GetValidatingWebhookConfiguration(vwcName)
					if err != nil {
						t.Fatalf("error getting initial webhook: %v", err)
					}
					if err := verifyValidatingWebhookConfiguration(got); err == nil {
						return
					}
				}
			}
		})
}

func verifyValidatingWebhookConfiguration(c *kubeApiAdmission.ValidatingWebhookConfiguration) error {
	if len(c.Webhooks) != 1 {
		return fmt.Errorf("only one webhook expected. Found %v", len(c.Webhooks))
	}
	wh := c.Webhooks[0]
	if *wh.FailurePolicy != kubeApiAdmission.Fail {
		return fmt.Errorf("wrong failure policy. c %v wanted %v", *wh.FailurePolicy, kubeApiAdmission.Fail)
	}
	if len(wh.ClientConfig.CABundle) == 0 {
		return fmt.Errorf("caBundle not matched")
	}
	return nil
}
