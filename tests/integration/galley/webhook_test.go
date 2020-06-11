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

package galley

import (
	"context"
	"errors"
	"fmt"
	"testing"

	kubeApiAdmission "k8s.io/api/admissionregistration/v1beta1"
	kubeApiMeta "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/environment/kube"
	"istio.io/istio/pkg/test/util/retry"
)

const (
	vwcName = "istiod-istio-system"
)

func TestWebhook(t *testing.T) {
	framework.NewTest(t).
		// Limit to Kube environment as we're testing integration of webhook with K8s.

		Run(func(ctx framework.TestContext) {
			env := ctx.Environment().(*kube.Environment)

			// clear the updated fields and verify istiod updates them

			retry.UntilSuccessOrFail(t, func() error {
				got, err := getValidatingWebhookConfiguration(env.KubeClusters[0], vwcName)
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

				if _, err := env.KubeClusters[0].AdmissionregistrationV1beta1().ValidatingWebhookConfigurations().Update(context.TODO(),
					updated, kubeApiMeta.UpdateOptions{}); err != nil {
					return fmt.Errorf("could not update validating webhook config: %s", updated.Name)
				}
				return nil
			})

			retry.UntilSuccessOrFail(t, func() error {
				got, err := getValidatingWebhookConfiguration(env.KubeClusters[0], vwcName)
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

func getValidatingWebhookConfiguration(client kubernetes.Interface, name string) (*kubeApiAdmission.ValidatingWebhookConfiguration, error) {
	whc, err := client.AdmissionregistrationV1beta1().ValidatingWebhookConfigurations().Get(context.TODO(),
		name, kubeApiMeta.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("could not get validating webhook config: %s", name)
	}
	return whc, nil
}

func verifyValidatingWebhookConfiguration(c *kubeApiAdmission.ValidatingWebhookConfiguration) error {
	if len(c.Webhooks) == 0 {
		return errors.New("no webhook entries found")
	}
	for i, wh := range c.Webhooks {
		if *wh.FailurePolicy != kubeApiAdmission.Fail {
			return fmt.Errorf("webhook #%v: wrong failure policy. c %v wanted %v",
				i, *wh.FailurePolicy, kubeApiAdmission.Fail)
		}
		if len(wh.ClientConfig.CABundle) == 0 {
			return fmt.Errorf("webhook #%v: caBundle not matched", i)
		}
	}
	return nil
}
