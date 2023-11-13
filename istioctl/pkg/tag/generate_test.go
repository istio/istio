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

package tag

import (
	"context"
	"fmt"
	"testing"

	admitv1 "k8s.io/api/admissionregistration/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"

	"istio.io/api/label"
	"istio.io/istio/pkg/kube"
)

var (
	defaultRevisionCanonicalWebhook = admitv1.MutatingWebhookConfiguration{
		ObjectMeta: metav1.ObjectMeta{
			Name:   "istio-sidecar-injector",
			Labels: map[string]string{label.IoIstioRev.Name: "default"},
		},
		Webhooks: []admitv1.MutatingWebhook{
			{
				Name: fmt.Sprintf("namespace.%s", istioInjectionWebhookSuffix),
				ClientConfig: admitv1.WebhookClientConfig{
					Service: &admitv1.ServiceReference{
						Namespace: "default",
						Name:      "istiod",
					},
					CABundle: []byte("ca"),
				},
			},
			{
				Name: fmt.Sprintf("object.%s", istioInjectionWebhookSuffix),
				ClientConfig: admitv1.WebhookClientConfig{
					Service: &admitv1.ServiceReference{
						Namespace: "default",
						Name:      "istiod",
					},
					CABundle: []byte("ca"),
				},
			},
		},
	}
	samplePath               = "/sample/path"
	revisionCanonicalWebhook = admitv1.MutatingWebhookConfiguration{
		ObjectMeta: metav1.ObjectMeta{
			Name:   "istio-sidecar-injector-revision",
			Labels: map[string]string{label.IoIstioRev.Name: "revision"},
		},
		Webhooks: []admitv1.MutatingWebhook{
			{
				Name: fmt.Sprintf("namespace.%s", istioInjectionWebhookSuffix),
				ClientConfig: admitv1.WebhookClientConfig{
					Service: &admitv1.ServiceReference{
						Namespace: "default",
						Name:      "istiod-revision",
						Path:      &samplePath,
					},
					CABundle: []byte("ca"),
				},
			},
			{
				Name: fmt.Sprintf("object.%s", istioInjectionWebhookSuffix),
				ClientConfig: admitv1.WebhookClientConfig{
					Service: &admitv1.ServiceReference{
						Namespace: "default",
						Name:      "istiod-revision",
					},
					CABundle: []byte("ca"),
				},
			},
		},
	}
	revisionCanonicalValidatingWebhook = admitv1.ValidatingWebhookConfiguration{
		ObjectMeta: metav1.ObjectMeta{
			Name:   "istio-validator-revision",
			Labels: map[string]string{label.IoIstioRev.Name: "revision"},
		},
		Webhooks: []admitv1.ValidatingWebhook{
			{
				Name: istioValidatingWebhookSuffix,
				ClientConfig: admitv1.WebhookClientConfig{
					Service: &admitv1.ServiceReference{
						Namespace: "default",
						Name:      "istiod-revision",
						Path:      &samplePath,
					},
					CABundle: []byte("ca"),
				},
			},
		},
	}
	remoteInjectionURL             = "https://random.host.com/inject/cluster/cluster1/net/net1"
	revisionCanonicalWebhookRemote = admitv1.MutatingWebhookConfiguration{
		ObjectMeta: metav1.ObjectMeta{
			Name:   "istio-sidecar-injector-revision",
			Labels: map[string]string{label.IoIstioRev.Name: "revision"},
		},
		Webhooks: []admitv1.MutatingWebhook{
			{
				Name: fmt.Sprintf("namespace.%s", istioInjectionWebhookSuffix),
				ClientConfig: admitv1.WebhookClientConfig{
					URL:      &remoteInjectionURL,
					CABundle: []byte("ca"),
				},
			},
			{
				Name: fmt.Sprintf("object.%s", istioInjectionWebhookSuffix),
				ClientConfig: admitv1.WebhookClientConfig{
					URL:      &remoteInjectionURL,
					CABundle: []byte("ca"),
				},
			},
		},
	}
	remoteValidationURL                      = "https://random.host.com/validate"
	revisionCanonicalValidatingWebhookRemote = admitv1.ValidatingWebhookConfiguration{
		ObjectMeta: metav1.ObjectMeta{
			Name:   "istio-validator-revision",
			Labels: map[string]string{label.IoIstioRev.Name: "revision"},
		},
		Webhooks: []admitv1.ValidatingWebhook{
			{
				Name: istioValidatingWebhookSuffix,
				ClientConfig: admitv1.WebhookClientConfig{
					URL:      &remoteValidationURL,
					CABundle: []byte("ca"),
				},
			},
		},
	}

	failPolicy                                   = admitv1.Fail
	revisionCanonicalDefaultTagValidatingWebhook = admitv1.ValidatingWebhookConfiguration{
		ObjectMeta: metav1.ObjectMeta{
			Name: vwhBaseTemplateName,
			Labels: map[string]string{
				label.IoIstioRev.Name: "revision",
				IstioTagLabel:         "default",
			},
		},
		Webhooks: []admitv1.ValidatingWebhook{
			{
				Name: istioValidatingWebhookSuffix,
				ClientConfig: admitv1.WebhookClientConfig{
					URL:      &samplePath,
					CABundle: []byte("ca"),
				},
				FailurePolicy: &failPolicy,
			},
		},
	}
)

func TestGenerateValidatingWebhook(t *testing.T) {
	tcs := []struct {
		name                 string
		istioNamespace       string
		webhook              admitv1.ValidatingWebhookConfiguration
		tagName              string
		revision             string
		whURL                string
		whSVC                string
		whCA                 string
		overwrite            bool
		wantNilFailurePolicy bool
	}{
		{
			name:           "webhook-pointing-to-service",
			istioNamespace: "istio-system",
			webhook:        revisionCanonicalValidatingWebhook,
			tagName:        "canary",
			revision:       "revision",
			whURL:          "",
			whSVC:          "istiod-revision",
			whCA:           "ca",
		},
		{
			name:           "webhook-custom-istio-namespace",
			istioNamespace: "istio-system-blue",
			webhook:        revisionCanonicalValidatingWebhook,
			tagName:        "canary",
			revision:       "revision",
			whURL:          "",
			whSVC:          "istiod-revision",
			whCA:           "ca",
		},
		{
			name:           "webhook-pointing-to-url",
			istioNamespace: "istio-system",
			webhook:        revisionCanonicalValidatingWebhookRemote,
			tagName:        "canary",
			revision:       "revision",
			whURL:          remoteValidationURL,
			whSVC:          "",
			whCA:           "ca",
		},
		{
			name:                 "webhook-pointing-to-default-tag",
			istioNamespace:       "istio-system",
			webhook:              revisionCanonicalValidatingWebhook,
			tagName:              "default",
			revision:             "revision",
			whURL:                "",
			whSVC:                "istiod-revision",
			whCA:                 "ca",
			overwrite:            true,
			wantNilFailurePolicy: true,
		},
	}
	scheme := runtime.NewScheme()
	codecFactory := serializer.NewCodecFactory(scheme)
	deserializer := codecFactory.UniversalDeserializer()

	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			client := kube.NewFakeClient(&tc.webhook, &revisionCanonicalDefaultTagValidatingWebhook)
			opts := &GenerateOptions{Tag: tc.tagName, Revision: tc.revision, Overwrite: tc.overwrite}
			webhookYAML, err := GenerateValidatingWebhook(context.Background(), client, opts, tc.istioNamespace)
			if err != nil {
				t.Fatalf("tag validating webhook YAML generation failed with error: %v", err)
			}
			vwhObject, _, err := deserializer.Decode([]byte(webhookYAML), nil, &admitv1.ValidatingWebhookConfiguration{})
			if err != nil {
				t.Fatalf("could not parse validating webhook from generated YAML: %s", vwhObject)
			}
			wh := vwhObject.(*admitv1.ValidatingWebhookConfiguration)

			for _, webhook := range wh.Webhooks {
				validationWhConf := webhook.ClientConfig

				if tc.wantNilFailurePolicy {
					// this is nil since we've already have one with failed FailurePolicy in the fake client
					if webhook.FailurePolicy != nil {
						t.Fatalf("expected FailurePolicy to be nil, got %v", *webhook.FailurePolicy)
					}
				}

				if tc.whSVC != "" {
					if validationWhConf.Service == nil {
						t.Fatalf("expected validation service %s, got nil", tc.whSVC)
					}
					if validationWhConf.Service.Name != tc.whSVC {
						t.Fatalf("expected validation service %s, got %s", tc.whSVC, validationWhConf.Service.Name)
					}
					if validationWhConf.Service.Namespace != tc.istioNamespace {
						t.Fatalf("expected validation service namespace %s, got %s", tc.istioNamespace, validationWhConf.Service.Namespace)
					}
				}
				if tc.whURL != "" {
					if validationWhConf.URL == nil {
						t.Fatalf("expected validation URL %s, got nil", tc.whURL)
					}
					if *validationWhConf.URL != tc.whURL {
						t.Fatalf("expected validation URL %s, got %s", tc.whURL, *validationWhConf.URL)
					}
				}
				if tc.whCA != "" {
					if string(validationWhConf.CABundle) != tc.whCA {
						t.Fatalf("expected CA bundle %q, got %q", tc.whCA, validationWhConf.CABundle)
					}
				}
			}
		})
	}
}

func TestGenerateMutatingWebhook(t *testing.T) {
	tcs := []struct {
		name        string
		webhook     admitv1.MutatingWebhookConfiguration
		tagName     string
		revision    string
		whURL       string
		whSVC       string
		whCA        string
		numWebhooks int
	}{
		{
			name:        "webhook-pointing-to-service",
			webhook:     revisionCanonicalWebhook,
			tagName:     "canary",
			revision:    "revision",
			whURL:       "",
			whSVC:       "istiod-revision",
			whCA:        "ca",
			numWebhooks: 2,
		},
		{
			name:        "webhook-pointing-to-url",
			webhook:     revisionCanonicalWebhookRemote,
			tagName:     "canary",
			revision:    "revision",
			whURL:       remoteInjectionURL,
			whSVC:       "",
			whCA:        "ca",
			numWebhooks: 2,
		},
		{
			name:        "webhook-pointing-to-default-revision",
			webhook:     defaultRevisionCanonicalWebhook,
			tagName:     "canary",
			revision:    "default",
			whURL:       "",
			whSVC:       "istiod",
			whCA:        "ca",
			numWebhooks: 2,
		},
		{
			name:        "webhook-pointing-to-default-revision",
			webhook:     defaultRevisionCanonicalWebhook,
			tagName:     "default",
			revision:    "default",
			whURL:       "",
			whSVC:       "istiod",
			whCA:        "ca",
			numWebhooks: 4,
		},
	}
	scheme := runtime.NewScheme()
	codecFactory := serializer.NewCodecFactory(scheme)
	deserializer := codecFactory.UniversalDeserializer()

	for _, tc := range tcs {
		client := kube.NewFakeClient(&tc.webhook)
		opts := &GenerateOptions{Tag: tc.tagName, Revision: tc.revision}
		webhookYAML, err := GenerateMutatingWebhook(context.Background(), client, opts, "default")
		if err != nil {
			t.Fatalf("tag webhook YAML generation failed with error: %v", err)
		}

		whObject, _, err := deserializer.Decode([]byte(webhookYAML), nil, &admitv1.MutatingWebhookConfiguration{})
		if err != nil {
			t.Fatalf("could not parse webhook from generated YAML: %s", webhookYAML)
		}
		wh := whObject.(*admitv1.MutatingWebhookConfiguration)

		// expect both namespace.sidecar-injector.istio.io and object.sidecar-injector.istio.io webhooks
		if len(wh.Webhooks) != tc.numWebhooks {
			t.Errorf("expected %d webhook(s) in MutatingWebhookConfiguration, found %d",
				tc.numWebhooks, len(wh.Webhooks))
		}
		tag, exists := wh.ObjectMeta.Labels[IstioTagLabel]
		if !exists {
			t.Errorf("expected tag webhook to have %s label, did not find", IstioTagLabel)
		}
		if tag != tc.tagName {
			t.Errorf("expected tag webhook to have istio.io/tag=%s, found %s instead", tc.tagName, tag)
		}

		// ensure all webhooks have the correct client config
		for _, webhook := range wh.Webhooks {
			injectionWhConf := webhook.ClientConfig
			if tc.whSVC != "" {
				if injectionWhConf.Service == nil {
					t.Fatalf("expected injection service %s, got nil", tc.whSVC)
				}
				if injectionWhConf.Service.Name != tc.whSVC {
					t.Fatalf("expected injection service %s, got %s", tc.whSVC, injectionWhConf.Service.Name)
				}
			}
			if tc.whURL != "" {
				if injectionWhConf.URL == nil {
					t.Fatalf("expected injection URL %s, got nil", tc.whURL)
				}
				if *injectionWhConf.URL != tc.whURL {
					t.Fatalf("expected injection URL %s, got %s", tc.whURL, *injectionWhConf.URL)
				}
			}
			if tc.whCA != "" {
				if string(injectionWhConf.CABundle) != tc.whCA {
					t.Fatalf("expected CA bundle %q, got %q", tc.whCA, injectionWhConf.CABundle)
				}
			}
		}
	}
}
