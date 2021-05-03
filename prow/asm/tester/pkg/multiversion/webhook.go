//  Copyright Istio Authors
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

package multiversion

import (
	"context"
	"fmt"
	"istio.io/istio/prow/asm/tester/pkg/asm/install/revision"
	"istio.io/istio/prow/asm/tester/pkg/exec"
	"istio.io/istio/prow/asm/tester/pkg/kube"
	admit_v1 "k8s.io/api/admissionregistration/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"log"
)

const (
	webhookPrefix = "istio-sidecar-injector"
)

// ReplaceWebhook creates a webhook with per-revision object selectors.
// this is useful when performing compat testing with older ASM versions.
func ReplaceWebhook(rev *revision.Config, contextName string) error {
	// If no version specified we are at master and the webhooks
	// have the desired behavior as is
	if rev.Version == "" {
		return nil
	}

	log.Printf("Generating webhook for revision %q: context: %s",
		rev.Name, contextName)

	cs, err := kube.NewClient(contextName)
	if err != nil {
		return fmt.Errorf("failed creating kube client: %w", err)
	}

	// Grab existing webhook for specified revision.
	webhookName := fmt.Sprintf("%s-%s",
		webhookPrefix, rev.Name)
	webhook, err := cs.
		AdmissionregistrationV1().
		MutatingWebhookConfigurations().
		Get(context.TODO(), webhookName, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("failed retrieving injection webhook %q: %w",
			webhookName, err)
	}

	// Extract CA bundle from webhook.
	if len(webhook.Webhooks) == 0 {
		return fmt.Errorf("could not extract CA bundle from webhook %q: %w",
			webhookName, err)
	}
	caBundle := webhook.Webhooks[0].ClientConfig.CABundle
	if len(caBundle) == 0 {
		return fmt.Errorf("empty CA bundle in webhook %q: %w",
			webhookName, err)
	}

	webhookCreateCmd := fmt.Sprintf("istioctl x revision tag generate %s --revision %s --context %s -y",
		rev.Name, rev.Name, contextName)
	generatedWebhook, err := exec.RunWithOutput(webhookCreateCmd)
	if err != nil {
		return fmt.Errorf("failed running tag generate command: %w", err)
	}

	// Deserialize the generated webhook so we can change name and CA bundle.
	scheme := runtime.NewScheme()
	codecFactory := serializer.NewCodecFactory(scheme)
	deserializer := codecFactory.UniversalDeserializer()

	whObject, _, err := deserializer.Decode([]byte(generatedWebhook), nil, &admit_v1.MutatingWebhookConfiguration{})
	if err != nil {
		return fmt.Errorf("could not decode generated webhook: %w", err)
	}
	decodedWh := whObject.(*admit_v1.MutatingWebhookConfiguration)

	// Since the Istio version may be too old to patch the new webhook format,
	// go through and manually patch in the CA bundles for the webhooks.
	for _, wh := range decodedWh.Webhooks {
		wh.ClientConfig.CABundle = caBundle
	}
	decodedWh.Name = webhookName

	// Remove the old webhook and create the new one.
	err = cs.
		AdmissionregistrationV1().
		MutatingWebhookConfigurations().
		Delete(context.TODO(), webhookName, metav1.DeleteOptions{})
	if err != nil {
		return fmt.Errorf("could not remove webhook %q: %w",
			webhookName, err)
	}
	_, err = cs.
		AdmissionregistrationV1().
		MutatingWebhookConfigurations().
		Create(context.TODO(), decodedWh, metav1.CreateOptions{})
	if err != nil {
		return fmt.Errorf("could not create webhook %q: %w",
			webhookName, err)
	}

	return nil
}
