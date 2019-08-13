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

package chiron

import (
	"bytes"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"io/ioutil"
	"net"
	"reflect"
	"time"

	"k8s.io/api/admissionregistration/v1beta1"

	"github.com/ghodss/yaml"

	"istio.io/pkg/log"

	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Read CA certificate and check whether it is a valid certificate.
func readCACert(caCertPath string) ([]byte, error) {
	caCert, err := ioutil.ReadFile(caCertPath)
	if err != nil {
		log.Errorf("failed to read CA cert, cert. path: %v, error: %v", caCertPath, err)
		return nil, fmt.Errorf("failed to read CA cert, cert. path: %v, error: %v", caCertPath, err)
	}

	b, _ := pem.Decode(caCert)
	if b == nil {
		return nil, fmt.Errorf("could not decode pem")
	}
	if b.Type != "CERTIFICATE" {
		return nil, fmt.Errorf("ca certificate contains wrong type: %v", b.Type)
	}
	if _, err := x509.ParseCertificate(b.Bytes); err != nil {
		return nil, fmt.Errorf("ca certificate parsing returns an error: %v", err)
	}

	return caCert, nil
}

func isTCPReachable(host string, port int) bool {
	addr := fmt.Sprintf("%s:%d", host, port)
	conn, err := net.DialTimeout("tcp", addr, 1*time.Second)
	if err != nil {
		log.Debugf("DialTimeout() returns err: %v", err)
		// No connection yet, so no need to conn.Close()
		return false
	}
	defer conn.Close()
	return true
}

// Rebuild the desired mutatingwebhookconfiguration from the specified CA
// and webhook config files.
func rebuildMutatingWebhookConfigHelper(
	caCert []byte, webhookConfigFile, webhookConfigName string,
) (*v1beta1.MutatingWebhookConfiguration, error) {
	// load and validate configuration
	webhookConfigData, err := ioutil.ReadFile(webhookConfigFile)
	if err != nil {
		return nil, err
	}
	var webhookConfig v1beta1.MutatingWebhookConfiguration
	if err := yaml.Unmarshal(webhookConfigData, &webhookConfig); err != nil {
		return nil, fmt.Errorf("could not decode mutatingwebhookconfiguration from %v: %v",
			webhookConfigFile, err)
	}

	// the webhook name is fixed at startup time
	webhookConfig.Name = webhookConfigName

	// patch the ca-cert into the user provided configuration
	for i := range webhookConfig.Webhooks {
		webhookConfig.Webhooks[i].ClientConfig.CABundle = caCert
	}

	return &webhookConfig, nil
}

// Create or update the mutatingwebhookconfiguration based on the config from rebuildMutatingWebhookConfig().
func createOrUpdateMutatingWebhookConfig(wc *WebhookController) error {
	if wc == nil {
		return fmt.Errorf("webhook controller is nil")
	}
	if wc.mutatingWebhookConfig == nil {
		return fmt.Errorf("mutatingwebhookconfiguration is nil")
	}
	client := wc.admission.MutatingWebhookConfigurations()
	webhookConfig := wc.mutatingWebhookConfig

	current, err := client.Get(webhookConfig.Name, metav1.GetOptions{})
	if err != nil {
		// If the webhookconfiguration does not exist yet, create the config.
		if kerrors.IsNotFound(err) {
			log.Debugf("get webhookConfig %v: NotFound", webhookConfig.Name)
			// Create the webhookconfiguration
			_, createErr := client.Create(webhookConfig)
			return createErr
		}
		log.Errorf("get webhookConfig %v err: %v", webhookConfig.Name, err)
		// There is an error when getting the webhookconfiguration and the error is
		// not that the webhookconfiguration not found. In this case, still try the update.
	}
	// Update the configuration only if the webhooks in the current is different from those configured.
	// Only copy the relevant fields that we want reconciled and ignore everything else, e.g. labels, selectors.
	updated := current.DeepCopyObject().(*v1beta1.MutatingWebhookConfiguration)
	updated.Webhooks = webhookConfig.Webhooks

	if !reflect.DeepEqual(updated, current) {
		// Update mutatingwebhookconfiguration to based on current and the webhook configured.
		_, err := client.Update(updated)
		if err != nil {
			log.Errorf("update webhookconfiguration returns err: %v", err)
		}
		return err
	}
	return nil
}

// Create or update the validatingwebhookconfiguration based on the config from rebuildValidatingWebhookConfig().
func createOrUpdateValidatingWebhookConfig(wc *WebhookController) error {
	if wc == nil {
		return fmt.Errorf("webhook controller is nil")
	}
	if wc.validatingWebhookConfig == nil {
		return fmt.Errorf("validatingwebhookconfiguration is nil")
	}
	client := wc.admission.ValidatingWebhookConfigurations()
	webhookConfig := wc.validatingWebhookConfig

	current, err := client.Get(webhookConfig.Name, metav1.GetOptions{})
	if err != nil {
		// If the webhookconfiguration does not exist yet, create the config.
		if kerrors.IsNotFound(err) {
			log.Debugf("get webhookConfig %v: NotFound", webhookConfig.Name)
			// Create the webhookconfiguration
			_, createErr := client.Create(webhookConfig)
			return createErr
		}
		log.Errorf("get webhookConfig %v err: %v", webhookConfig.Name, err)
		// There is an error when getting the webhookconfiguration and the error is
		// not that the webhookconfiguration not found. In this case, still try the update.
	}
	// Update the configuration only if the webhooks in the current is different from those configured.
	// Only copy the relevant fields that we want reconciled and ignore everything else, e.g. labels, selectors.
	updated := current.DeepCopyObject().(*v1beta1.ValidatingWebhookConfiguration)
	updated.Webhooks = webhookConfig.Webhooks

	if !reflect.DeepEqual(updated, current) {
		// Update webhookconfiguration to based on current and the webhook configured.
		_, err := client.Update(updated)
		if err != nil {
			log.Errorf("update webhookconfiguration returns err: %v", err)
		}
		return err
	}
	return nil
}

// Reload CA cert from file and return whether CA cert is changed
func reloadCACert(wc *WebhookController) (bool, error) {
	certChanged := false
	wc.certMutex.Lock()
	defer wc.certMutex.Unlock()
	caCert, err := readCACert(wc.k8sCaCertFile)
	if err != nil {
		return certChanged, err
	}
	if !bytes.Equal(caCert, wc.CACert) {
		wc.CACert = append([]byte(nil), caCert...)
		certChanged = true
	}
	return certChanged, nil
}
