package validation

import (
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"io/ioutil"
	"reflect"

	"github.com/ghodss/yaml"
	"istio.io/istio/pkg/log"
	"k8s.io/api/admissionregistration/v1beta1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	admissionregistration "k8s.io/client-go/kubernetes/typed/admissionregistration/v1beta1"
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
		if errors.IsNotFound(err) {
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
	webhookConfig, caCertPem, err := rebuildWebhookConfigurationHelper(
		wh.caFile,
		wh.webhookConfigFile,
		wh.ownerRefs,
		wh.caCertPem)
	if err != nil {
		reportValidationConfigLoadError(err)
		log.Errorf("%v validatingwebhookconfiguration (re)load failed: %v",
			wh.webhookConfiguration.Name, err)
		return err
	}
	wh.webhookConfiguration = webhookConfig
	wh.caCertPem = caCertPem

	log.Infof("%v validatingwebhookconfiguration reloaded: %#v", wh.webhookConfiguration.Name, webhookConfig)
	return nil
}

func rebuildWebhookConfigurationHelper(
	caFile, webhookConfigFile string,
	ownerRefs []metav1.OwnerReference,
	prevCAPem []byte,
) (*v1beta1.ValidatingWebhookConfiguration, []byte, error) {
	// load and validate configuration
	webhookConfigData, err := ioutil.ReadFile(webhookConfigFile)
	if err != nil {
		return nil, nil, err
	}
	var webhookConfig v1beta1.ValidatingWebhookConfiguration
	if err := yaml.Unmarshal(webhookConfigData, &webhookConfig); err != nil {
		return nil, nil, fmt.Errorf("could not decode validatingwebhookconfiguration from %v: %v",
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

	caPem := prevCAPem

	// load and validate ca-cert
	caCertPemBytes, err := ioutil.ReadFile(caFile)
	if err != nil {
		return nil, nil, err
	}
	if block, _ := pem.Decode(caCertPemBytes); block != nil {
		if block.Type != "CERTIFICATE" {
			return nil, nil, fmt.Errorf("%s contains wrong pem type: %q", caFile, block.Type)
		}
		if _, err := x509.ParseCertificate(block.Bytes); err != nil {
			return nil, nil, fmt.Errorf("%s contains invalid x509 certiticate: %v", caFile, err)
		}
		caPem = caCertPemBytes
	}
	if len(caPem) == 0 {
		return nil, nil, fmt.Errorf("%s doesn't contain a valid pem encoded cert", caFile)
	}

	// patch the ca-cert into the user provided configuration
	for i := range webhookConfig.Webhooks {
		webhookConfig.Webhooks[i].ClientConfig.CABundle = caPem
	}

	return &webhookConfig, nil, nil
}
