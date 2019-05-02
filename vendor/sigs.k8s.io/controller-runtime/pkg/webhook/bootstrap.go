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

package webhook

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"path"
	"sort"
	"strconv"

	"k8s.io/api/admissionregistration/v1beta1"
	admissionregistration "k8s.io/api/admissionregistration/v1beta1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/config"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
	"sigs.k8s.io/controller-runtime/pkg/webhook/internal/cert"
	"sigs.k8s.io/controller-runtime/pkg/webhook/internal/cert/writer"
	"sigs.k8s.io/controller-runtime/pkg/webhook/types"
)

// setDefault does defaulting for the Server.
func (s *Server) setDefault() {
	s.setServerDefault()
	s.setBootstrappingDefault()
}

// setServerDefault does defaulting for the ServerOptions.
func (s *Server) setServerDefault() {
	if len(s.Name) == 0 {
		s.Name = "default-k8s-webhook-server"
	}
	if s.registry == nil {
		s.registry = map[string]Webhook{}
	}
	if s.sMux == nil {
		s.sMux = http.DefaultServeMux
	}
	if s.Port <= 0 {
		s.Port = 443
	}
	if len(s.CertDir) == 0 {
		s.CertDir = path.Join("k8s-webhook-server", "cert")
	}
	if s.DisableWebhookConfigInstaller == nil {
		diwc := false
		s.DisableWebhookConfigInstaller = &diwc
	}

	if s.Client == nil {
		cfg, err := config.GetConfig()
		if err != nil {
			s.err = err
			return
		}
		s.Client, err = client.New(cfg, client.Options{})
		if err != nil {
			s.err = err
			return
		}
	}
}

// setBootstrappingDefault does defaulting for the Server bootstrapping.
func (s *Server) setBootstrappingDefault() {
	if s.BootstrapOptions == nil {
		s.BootstrapOptions = &BootstrapOptions{}
	}
	if len(s.MutatingWebhookConfigName) == 0 {
		s.MutatingWebhookConfigName = "mutating-webhook-configuration"
	}
	if len(s.ValidatingWebhookConfigName) == 0 {
		s.ValidatingWebhookConfigName = "validating-webhook-configuration"
	}
	if s.Host == nil && s.Service == nil {
		varString := "localhost"
		s.Host = &varString
	}

	var certWriter writer.CertWriter
	var err error
	if s.Secret != nil {
		certWriter, err = writer.NewSecretCertWriter(
			writer.SecretCertWriterOptions{
				Secret: s.Secret,
				Client: s.Client,
			})
	} else {
		certWriter, err = writer.NewFSCertWriter(
			writer.FSCertWriterOptions{
				Path: s.CertDir,
			})
	}
	if err != nil {
		s.err = err
		return
	}
	s.certProvisioner = &cert.Provisioner{
		CertWriter: certWriter,
	}
}

// InstallWebhookManifests creates the admissionWebhookConfiguration objects and service if any.
// It also provisions the certificate for the admission server.
func (s *Server) InstallWebhookManifests() error {
	// do defaulting if necessary
	s.once.Do(s.setDefault)
	if s.err != nil {
		return s.err
	}

	var err error
	s.webhookConfigurations, err = s.whConfigs()
	if err != nil {
		return err
	}
	svc := s.service()
	objects := append(s.webhookConfigurations, svc)

	cc, err := s.getClientConfig()
	if err != nil {
		return err
	}
	// Provision the cert by creating new one or refreshing existing one.
	_, err = s.certProvisioner.Provision(cert.Options{
		ClientConfig: cc,
		Objects:      s.webhookConfigurations,
	})
	if err != nil {
		return err
	}

	return batchCreateOrReplace(s.Client, objects...)
}

func (s *Server) getClientConfig() (*admissionregistration.WebhookClientConfig, error) {
	if s.Host != nil && s.Service != nil {
		return nil, errors.New("URL and Service can't be set at the same time")
	}
	cc := &admissionregistration.WebhookClientConfig{
		CABundle: []byte{},
	}
	if s.Host != nil {
		u := url.URL{
			Scheme: "https",
			Host:   net.JoinHostPort(*s.Host, strconv.Itoa(int(s.Port))),
		}
		urlString := u.String()
		cc.URL = &urlString
	}
	if s.Service != nil {
		cc.Service = &admissionregistration.ServiceReference{
			Name:      s.Service.Name,
			Namespace: s.Service.Namespace,
			// Path will be set later
		}
	}
	return cc, nil
}

// getClientConfigWithPath constructs a WebhookClientConfig based on the server options.
// It will use path to the set the path in WebhookClientConfig.
func (s *Server) getClientConfigWithPath(path string) (*admissionregistration.WebhookClientConfig, error) {
	cc, err := s.getClientConfig()
	if err != nil {
		return nil, err
	}
	return cc, setPath(cc, path)
}

// setPath sets the path in the WebhookClientConfig.
func setPath(cc *admissionregistration.WebhookClientConfig, path string) error {
	if cc.URL != nil {
		u, err := url.Parse(*cc.URL)
		if err != nil {
			return err
		}
		u.Path = path
		urlString := u.String()
		cc.URL = &urlString
	}
	if cc.Service != nil {
		cc.Service.Path = &path
	}
	return nil
}

// whConfigs creates a mutatingWebhookConfiguration and(or) a validatingWebhookConfiguration based on registry.
// For the same type of webhook configuration, it generates a webhook entry per endpoint.
func (s *Server) whConfigs() ([]runtime.Object, error) {
	objs := []runtime.Object{}
	mutatingWH, err := s.mutatingWHConfigs()
	if err != nil {
		return nil, err
	}
	if mutatingWH != nil {
		objs = append(objs, mutatingWH)
	}
	validatingWH, err := s.validatingWHConfigs()
	if err != nil {
		return nil, err
	}
	if validatingWH != nil {
		objs = append(objs, validatingWH)
	}
	return objs, nil
}

func (s *Server) mutatingWHConfigs() (runtime.Object, error) {
	mutatingWebhooks := []v1beta1.Webhook{}
	for path, webhook := range s.registry {
		if webhook.GetType() != types.WebhookTypeMutating {
			continue
		}

		admissionWebhook := webhook.(*admission.Webhook)
		wh, err := s.admissionWebhook(path, admissionWebhook)
		if err != nil {
			return nil, err
		}
		mutatingWebhooks = append(mutatingWebhooks, *wh)
	}

	sort.Slice(mutatingWebhooks, func(i, j int) bool {
		return mutatingWebhooks[i].Name < mutatingWebhooks[j].Name
	})

	if len(mutatingWebhooks) > 0 {
		return &admissionregistration.MutatingWebhookConfiguration{
			TypeMeta: metav1.TypeMeta{
				APIVersion: fmt.Sprintf("%s/%s", admissionregistration.GroupName, "v1beta1"),
				Kind:       "MutatingWebhookConfiguration",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name: s.MutatingWebhookConfigName,
			},
			Webhooks: mutatingWebhooks,
		}, nil
	}
	return nil, nil
}

func (s *Server) validatingWHConfigs() (runtime.Object, error) {
	validatingWebhooks := []v1beta1.Webhook{}
	for path, webhook := range s.registry {
		var admissionWebhook *admission.Webhook
		if webhook.GetType() != types.WebhookTypeValidating {
			continue
		}

		admissionWebhook = webhook.(*admission.Webhook)
		wh, err := s.admissionWebhook(path, admissionWebhook)
		if err != nil {
			return nil, err
		}
		validatingWebhooks = append(validatingWebhooks, *wh)
	}

	sort.Slice(validatingWebhooks, func(i, j int) bool {
		return validatingWebhooks[i].Name < validatingWebhooks[j].Name
	})

	if len(validatingWebhooks) > 0 {
		return &admissionregistration.ValidatingWebhookConfiguration{
			TypeMeta: metav1.TypeMeta{
				APIVersion: fmt.Sprintf("%s/%s", admissionregistration.GroupName, "v1beta1"),
				Kind:       "ValidatingWebhookConfiguration",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name: s.ValidatingWebhookConfigName,
			},
			Webhooks: validatingWebhooks,
		}, nil
	}
	return nil, nil
}

func (s *Server) admissionWebhook(path string, wh *admission.Webhook) (*admissionregistration.Webhook, error) {
	if wh.NamespaceSelector == nil && s.Service != nil && len(s.Service.Namespace) > 0 {
		wh.NamespaceSelector = &metav1.LabelSelector{
			MatchExpressions: []metav1.LabelSelectorRequirement{
				{
					Key:      "control-plane",
					Operator: metav1.LabelSelectorOpDoesNotExist,
				},
			},
		}
	}

	webhook := &admissionregistration.Webhook{
		Name:              wh.GetName(),
		Rules:             wh.Rules,
		FailurePolicy:     wh.FailurePolicy,
		NamespaceSelector: wh.NamespaceSelector,
		ClientConfig: admissionregistration.WebhookClientConfig{
			// The reason why we assign an empty byte array to CABundle is that
			// CABundle field will be updated by the Provisioner.
			CABundle: []byte{},
		},
	}
	cc, err := s.getClientConfigWithPath(path)
	if err != nil {
		return nil, err
	}
	webhook.ClientConfig = *cc
	return webhook, nil
}

// service creates a corev1.service object fronting the admission server.
func (s *Server) service() runtime.Object {
	if s.Service == nil {
		return nil
	}
	svc := &corev1.Service{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "Service",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      s.Service.Name,
			Namespace: s.Service.Namespace,
		},
		Spec: corev1.ServiceSpec{
			Selector: s.Service.Selectors,
			Ports: []corev1.ServicePort{
				{
					// When using service, kube-apiserver will send admission request to port 443.
					Port:       443,
					TargetPort: intstr.IntOrString{Type: intstr.Int, IntVal: s.Port},
				},
			},
		},
	}
	return svc
}
