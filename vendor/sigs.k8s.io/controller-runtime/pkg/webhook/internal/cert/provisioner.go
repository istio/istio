/*
Copyright 2018 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package cert

import (
	"bytes"
	"errors"
	"fmt"
	"net"
	"net/url"

	admissionregistrationv1beta1 "k8s.io/api/admissionregistration/v1beta1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/webhook/internal/cert/generator"
	"sigs.k8s.io/controller-runtime/pkg/webhook/internal/cert/writer"
)

// Provisioner provisions certificates for webhook configurations and writes them to an output
// destination - such as a Secret or local file. Provisioner can update the CA field of
// certain resources with the CA of the certs.
type Provisioner struct {
	// CertWriter knows how to persist the certificate.
	CertWriter writer.CertWriter
}

// Options are options for provisioning the certificate.
type Options struct {
	// ClientConfig is the WebhookClientCert that contains the information to generate
	// the certificate. The CA Certificate will be updated in the ClientConfig.
	// The updated ClientConfig will be used to inject into other runtime.Objects,
	// e.g. MutatingWebhookConfiguration and ValidatingWebhookConfiguration.
	ClientConfig *admissionregistrationv1beta1.WebhookClientConfig
	// Objects are the objects that will use the ClientConfig above.
	Objects []runtime.Object
}

// Provision provisions certificates for the WebhookClientConfig.
// It ensures the cert and CA are valid and not expiring.
// It updates the CABundle in the webhookClientConfig if necessary.
// It inject the WebhookClientConfig into options.Objects.
func (cp *Provisioner) Provision(options Options) (bool, error) {
	if cp.CertWriter == nil {
		return false, errors.New("CertWriter need to be set")
	}

	dnsName, err := dnsNameFromClientConfig(options.ClientConfig)
	if err != nil {
		return false, err
	}

	certs, changed, err := cp.CertWriter.EnsureCert(dnsName)
	if err != nil {
		return false, err
	}

	caBundle := options.ClientConfig.CABundle
	caCert := certs.CACert
	// TODO(mengqiy): limit the size of the CABundle by GC the old CA certificate
	// this is important since the max record size in etcd is 1MB (latest version is 1.5MB).
	if !bytes.Contains(caBundle, caCert) {
		// Ensure the CA bundle in the webhook configuration has the signing CA.
		options.ClientConfig.CABundle = append(caBundle, caCert...)
		changed = true
	}
	return changed, cp.inject(options.ClientConfig, options.Objects)
}

// Inject the ClientConfig to the objects.
// It supports MutatingWebhookConfiguration and ValidatingWebhookConfiguration.
func (cp *Provisioner) inject(cc *admissionregistrationv1beta1.WebhookClientConfig, objs []runtime.Object) error {
	if cc == nil {
		return nil
	}
	for i := range objs {
		switch typed := objs[i].(type) {
		case *admissionregistrationv1beta1.MutatingWebhookConfiguration:
			injectForEachWebhook(cc, typed.Webhooks)
		case *admissionregistrationv1beta1.ValidatingWebhookConfiguration:
			injectForEachWebhook(cc, typed.Webhooks)
		default:
			return fmt.Errorf("%#v is not supported for injecting a webhookClientConfig",
				objs[i].GetObjectKind().GroupVersionKind())
		}
	}
	return cp.CertWriter.Inject(objs...)
}

func injectForEachWebhook(
	cc *admissionregistrationv1beta1.WebhookClientConfig,
	webhooks []admissionregistrationv1beta1.Webhook) {
	for i := range webhooks {
		// only replacing the CA bundle to preserve the path in the WebhookClientConfig
		webhooks[i].ClientConfig.CABundle = cc.CABundle
	}
}

func dnsNameFromClientConfig(config *admissionregistrationv1beta1.WebhookClientConfig) (string, error) {
	if config == nil {
		return "", errors.New("clientConfig should not be empty")
	}
	if config.Service != nil && config.URL != nil {
		return "", fmt.Errorf("service and URL can't be set at the same time in a webhook: %v", config)
	}
	if config.Service == nil && config.URL == nil {
		return "", fmt.Errorf("one of service and URL need to be set in a webhook: %v", config)
	}
	if config.Service != nil {
		return generator.ServiceToCommonName(config.Service.Namespace, config.Service.Name), nil
	}
	u, err := url.Parse(*config.URL)
	if err != nil {
		return "", err
	}
	host, _, err := net.SplitHostPort(u.Host)
	if err != nil {
		return u.Host, nil
	}
	return host, err
}
