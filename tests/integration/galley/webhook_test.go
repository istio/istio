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

	kubeApiAdmission "k8s.io/api/admissionregistration/v1beta1"

	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/environment"
	"istio.io/istio/pkg/test/framework/components/environment/kube"
	"istio.io/istio/pkg/test/util/retry"
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

			// clear the updated fields and verify istiod updates them

			retry.UntilSuccessOrFail(t, func() error {
				got, err := env.GetValidatingWebhookConfiguration(vwcName)
				if err != nil {
					return fmt.Errorf("error getting initial webhook: %v", err)
				}
				if err := verifyValidatingWebhookConfiguration(got); err != nil {
					return err
				}

				updated := got.DeepCopyObject().(*kubeApiAdmission.ValidatingWebhookConfiguration)
				updated.Webhooks[0].ClientConfig.CABundle = nil
				ignore := kubeApiAdmission.Ignore // can't take the address of a constant
				updated.Webhooks[0].FailurePolicy = &ignore

				return env.UpdateValidatingWebhookConfiguration(updated)
			})

			retry.UntilSuccessOrFail(t, func() error {
				got, err := env.GetValidatingWebhookConfiguration(vwcName)
				if err != nil {
					t.Fatalf("error getting initial webhook: %v", err)
				}
				if err := verifyValidatingWebhookConfiguration(got); err != nil {
					return fmt.Errorf("validatingwebhookconfiguration not updated yet: %v", err)
				}
				return nil
			})
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
