//go:build integ

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
	"errors"
	"fmt"
	"testing"

	kubeApiAdmission "k8s.io/api/admissionregistration/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"istio.io/api/label"
	networking "istio.io/api/networking/v1alpha3"
	clientnetworking "istio.io/client-go/pkg/apis/networking/v1"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/util/retry"
)

func TestWebhook(t *testing.T) {
	// nolint: staticcheck
	framework.NewTest(t).
		RequiresSingleCluster().
		Run(func(t framework.TestContext) {
			vwcName := "istio-validator"
			if t.Settings().Revisions.Default() != "" {
				vwcName = fmt.Sprintf("%s-%s", vwcName, t.Settings().Revisions.Default())
			}
			vwcName += "-istio-system"
			webhooks := []string{vwcName, "istiod-default-validator"}

			// clear the updated fields and verify istiod updates them
			cluster := t.Clusters().Default()
			for _, vwcName := range webhooks {
				retry.UntilSuccessOrFail(t, func() error {
					got, err := getValidatingWebhookConfiguration(cluster.Kube(), vwcName)
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

					if _, err := cluster.Kube().AdmissionregistrationV1().ValidatingWebhookConfigurations().Update(context.TODO(),
						updated, metav1.UpdateOptions{}); err != nil {
						return fmt.Errorf("could not update validating webhook config %q: %v", updated.Name, err)
					}
					return nil
				})

				retry.UntilSuccessOrFail(t, func() error {
					got, err := getValidatingWebhookConfiguration(cluster.Kube(), vwcName)
					if err != nil {
						t.Fatalf("error getting initial webhook: %v", err)
					}
					if err := verifyValidatingWebhookConfiguration(got); err != nil {
						return fmt.Errorf("validatingwebhookconfiguration not updated yet: %v", err)
					}
					return nil
				})
			}

			revision := "default"
			if t.Settings().Revisions.Default() != "" {
				revision = t.Settings().Revisions.Default()
			}
			verifyRejectsInvalidConfig(t, revision, true)
			verifyRejectsInvalidConfig(t, "", true)
		})
}

func getValidatingWebhookConfiguration(client kubernetes.Interface, name string) (*kubeApiAdmission.ValidatingWebhookConfiguration, error) {
	whc, err := client.AdmissionregistrationV1().ValidatingWebhookConfigurations().Get(context.TODO(),
		name, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("could not get validating webhook config %q: %v", name, err)
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
			return fmt.Errorf("webhook #%v: caBundle not patched", i)
		}
	}
	return nil
}

func verifyRejectsInvalidConfig(t framework.TestContext, configRevision string, shouldReject bool) {
	t.Helper()
	const istioNamespace = "istio-system"
	revLabel := map[string]string{}
	if configRevision != "" {
		revLabel[label.IoIstioRev.Name] = configRevision
	}
	invalidGateway := &clientnetworking.Gateway{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "invalid-istio-gateway",
			Namespace: istioNamespace,
			Labels:    revLabel,
		},
		Spec: networking.Gateway{},
	}

	createOptions := metav1.CreateOptions{DryRun: []string{metav1.DryRunAll}}
	istioClient := t.Clusters().Default().Istio().NetworkingV1()
	_, err := istioClient.Gateways(istioNamespace).Create(context.TODO(), invalidGateway, createOptions)
	rejected := err != nil
	if rejected != shouldReject {
		t.Errorf("Config rejected: %t, expected config rejected: %t", rejected, shouldReject)
	}
}
