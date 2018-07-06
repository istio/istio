// Copyright 2018 Istio Authors
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

package util

import (
	"encoding/json"
	"fmt"

	admissionregistrationv1beta1 "k8s.io/api/admissionregistration/v1beta1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/strategicpatch"
	admissionregistrationv1beta1client "k8s.io/client-go/kubernetes/typed/admissionregistration/v1beta1"
)

// PatchMutatingWebhookConfig patches a CA bundle into the specified webhook config.
func PatchMutatingWebhookConfig(client admissionregistrationv1beta1client.MutatingWebhookConfigurationInterface,
	webhookConfigName, webhookName string, caBundle []byte) error {
	config, err := client.Get(webhookConfigName, metav1.GetOptions{})
	if err != nil {
		return err
	}
	prev, err := json.Marshal(config)
	if err != nil {
		return err
	}
	found := false
	for i, w := range config.Webhooks {
		if w.Name == webhookName {
			config.Webhooks[i].ClientConfig.CABundle = caBundle[:]
			found = true
			break
		}
	}
	if !found {
		return apierrors.NewInternalError(fmt.Errorf(
			"webhook entry %q not found in config %q", webhookName, webhookConfigName))
	}
	curr, err := json.Marshal(config)
	if err != nil {
		return err
	}
	patch, err := strategicpatch.CreateTwoWayMergePatch(prev, curr, admissionregistrationv1beta1.MutatingWebhookConfiguration{})
	if err != nil {
		return err
	}

	if string(patch) != "{}" {
		_, err = client.Patch(webhookConfigName, types.StrategicMergePatchType, patch)
	}
	return err
}

// PatchValidatingWebhookConfig patches a CA bundle into the specified webhook config.
func PatchValidatingWebhookConfig(client admissionregistrationv1beta1client.ValidatingWebhookConfigurationInterface,
	webhookConfigName string, webhookNames []string, caBundle []byte) error {
	config, err := client.Get(webhookConfigName, metav1.GetOptions{})
	if err != nil {
		return err
	}
	prev, err := json.Marshal(config)
	if err != nil {
		return err
	}
	var found int
	names := map[string]bool{}
	for _, name := range webhookNames {
		names[name] = true
	}
	for i, w := range config.Webhooks {
		if _, ok := names[w.Name]; ok {
			config.Webhooks[i].ClientConfig.CABundle = caBundle[:]
			found++
			if found == len(webhookNames) {
				break
			}
		}
	}
	if found < len(webhookNames) {
		return apierrors.NewInternalError(fmt.Errorf(
			"webhook entries %q not found in config %q", webhookNames, webhookConfigName))
	}
	curr, err := json.Marshal(config)
	if err != nil {
		return err
	}
	patch, err := strategicpatch.CreateTwoWayMergePatch(prev, curr, admissionregistrationv1beta1.ValidatingWebhookConfiguration{})
	if err != nil {
		return err
	}

	if string(patch) != "{}" {
		_, err = client.Patch(webhookConfigName, types.StrategicMergePatchType, patch)
	}
	return err
}
