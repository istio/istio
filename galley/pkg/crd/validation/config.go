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

package validation

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"reflect"

	"github.com/ghodss/yaml"
	"k8s.io/api/admissionregistration/v1beta1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	admissionregistration "k8s.io/client-go/kubernetes/typed/admissionregistration/v1beta1"

	"istio.io/istio/pkg/log"
)

func (wh *Webhook) reconcileWebhookConfiguration() {
	client := wh.clientset.AdmissionregistrationV1beta1().ValidatingWebhookConfigurations()
	updated, err := reconcileWebhookConfigurationHelper(client, wh.webhookConfiguration)
	if err != nil {
		log.Errorf("%v validatingwebhookconfiguration update failed: %v",
			wh.webhookConfiguration.Name, err)
		reportValidationConfigUpdateError(err)
	} else if updated {
		log.Infof("%v validatingwebhookconfiguration updated", wh.webhookConfiguration.Name)
		reportValidationConfigUpdate()
	}
}

func reconcileWebhookConfigurationHelper(
	client admissionregistration.ValidatingWebhookConfigurationInterface,
	webhookConfiguration *v1beta1.ValidatingWebhookConfiguration,
) (bool, error) {
	current, err := client.Get(webhookConfiguration.Name, metav1.GetOptions{})
	if err != nil {
		if k8serrors.IsNotFound(err) {
			if _, createErr := client.Create(webhookConfiguration); createErr != nil {
				return false, createErr
			}
			return true, nil
		}
		return false, err
	}

	updated := current.DeepCopyObject().(*v1beta1.ValidatingWebhookConfiguration)
	updated.Webhooks = webhookConfiguration.Webhooks
	updated.OwnerReferences = webhookConfiguration.OwnerReferences

	if !reflect.DeepEqual(updated, current) {
		_, err := client.Update(updated)
		return true, err
	}
	return false, nil
}

func (wh *Webhook) rebuildWebhookConfiguration() error {
	webhookConfig, err := rebuildWebhookConfigurationHelper(
		wh.caFile,
		wh.webhookConfigFile,
		wh.ownerRefs)
	if err != nil {
		reportValidationConfigLoadError(err)
		log.Errorf("%v validatingwebhookconfiguration (re)load failed: %v",
			wh.webhookConfiguration.Name, err)
		return err
	}
	wh.webhookConfiguration = webhookConfig

	var webhookYAML string
	if b, err := yaml.Marshal(wh.webhookConfiguration); err == nil {
		webhookYAML = string(b)
	}

	reportValidationConfigLoad()
	log.Infof("%v validatingwebhookconfiguration (re)loaded: \n%v",
		wh.webhookConfiguration.Name, webhookYAML)
	return nil
}

func loadCaCertPem(in io.Reader) ([]byte, error) {
	caCertPemBytes, err := ioutil.ReadAll(in)
	if err != nil {
		return nil, err
	}
	block, _ := pem.Decode(caCertPemBytes)
	if block == nil {
		return nil, errors.New("could not decode pem")
	}
	if block.Type != "CERTIFICATE" {
		return nil, fmt.Errorf("ca bundle contains wrong pem type: %q", block.Type)
	}
	if _, err := x509.ParseCertificate(block.Bytes); err != nil {
		return nil, fmt.Errorf("ca bundle contains invalid x509 certiticate: %v", err)
	}
	return caCertPemBytes, nil
}

func rebuildWebhookConfigurationHelper(
	caFile, webhookConfigFile string,
	ownerRefs []metav1.OwnerReference,
) (*v1beta1.ValidatingWebhookConfiguration, error) {
	// load and validate configuration
	webhookConfigData, err := ioutil.ReadFile(webhookConfigFile)
	if err != nil {
		return nil, err
	}
	var webhookConfig v1beta1.ValidatingWebhookConfiguration
	if err := yaml.Unmarshal(webhookConfigData, &webhookConfig); err != nil {
		return nil, fmt.Errorf("could not decode validatingwebhookconfiguration from %v: %v",
			webhookConfigFile, err)
	}

	// fill in missing defaults
	for i := 0; i < len(webhookConfig.Webhooks); i++ {
		if webhookConfig.Webhooks[i].FailurePolicy == nil {
			failurePolicy := v1beta1.Fail
			webhookConfig.Webhooks[i].FailurePolicy = &failurePolicy
		}
		if webhookConfig.Webhooks[i].NamespaceSelector == nil {
			webhookConfig.Webhooks[i].NamespaceSelector = &metav1.LabelSelector{}
		}
	}

	// update ownerRefs so configuration is cleaned up when the validation deployment is deleted.
	webhookConfig.OwnerReferences = ownerRefs

	in, err := os.Open(caFile)
	if err != nil {
		return nil, fmt.Errorf("failed to read ca bundle from %v: %v", caFile, err)
	}
	defer in.Close() // nolint: errcheck

	caPem, err := loadCaCertPem(in)
	if err != nil {
		return nil, err
	}

	// patch the ca-cert into the user provided configuration
	for i := range webhookConfig.Webhooks {
		webhookConfig.Webhooks[i].ClientConfig.CABundle = caPem
	}

	return &webhookConfig, nil
}

func (wh *Webhook) reloadKeyCert() {
	pair, err := tls.LoadX509KeyPair(wh.certFile, wh.keyFile)
	if err != nil {
		reportValidationCertKeyUpdateError(err)
		log.Errorf("Cert/Key reload error: %v", err)
		return
	}
	wh.mu.Lock()
	wh.cert = &pair
	wh.mu.Unlock()

	reportValidationCertKeyUpdate()
	log.Info("Cert and Key reloaded")
}
