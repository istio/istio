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
	"path/filepath"
	"testing"

	admitv1 "k8s.io/api/admissionregistration/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"istio.io/api/label"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/test/env"
	"istio.io/istio/pkg/test/util/assert"
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
	operatorManaged          = operatorNamespace + "/managed"
	revisionCanonicalWebhook = admitv1.MutatingWebhookConfiguration{
		ObjectMeta: metav1.ObjectMeta{
			Name: "istio-sidecar-injector-revision",
			Labels: map[string]string{
				label.IoIstioRev.Name: "revision",
				operatorManaged:       "Reconcile",
			},
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
	remoteValidationURL                                        = "https://random.host.com/validate"
	neverReinvocationPolicy                                    = admitv1.NeverReinvocationPolicy
	ifNeededReinvocationPolicy                                 = admitv1.IfNeededReinvocationPolicy
	defaultRevisionCanonicalWebhookWithNeverReinvocationPolicy = admitv1.MutatingWebhookConfiguration{
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
				ReinvocationPolicy: &neverReinvocationPolicy,
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
				ReinvocationPolicy: &neverReinvocationPolicy,
			},
		},
	}
	defaultRevisionCanonicalWebhookWithIfNeededReinvocationPolicy = admitv1.MutatingWebhookConfiguration{
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
				ReinvocationPolicy: &ifNeededReinvocationPolicy,
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
				ReinvocationPolicy: &ifNeededReinvocationPolicy,
			},
		},
	}
)

func TestGenerateValidatingWebhook(t *testing.T) {
	tcs := []struct {
		name           string
		istioNamespace string
		webhook        admitv1.MutatingWebhookConfiguration
		whURL          string
		whSVC          string
		whCA           string
		userManaged    bool
	}{
		{
			name:           "webhook-pointing-to-service",
			istioNamespace: "istio-system",
			webhook:        revisionCanonicalWebhook,
			whURL:          "",
			whSVC:          "istiod-revision",
			whCA:           "ca",
		},
		{
			name:           "webhook-custom-istio-namespace",
			istioNamespace: "istio-system-blue",
			webhook:        revisionCanonicalWebhook,
			whURL:          "",
			whSVC:          "istiod-revision",
			whCA:           "ca",
		},
		{
			name:           "webhook-pointing-to-url",
			istioNamespace: "istio-system",
			webhook:        revisionCanonicalWebhookRemote,
			whURL:          remoteValidationURL,
			whSVC:          "",
			whCA:           "ca",
		},
		{
			name:           "webhook-process-failure-policy",
			istioNamespace: "istio-system",
			webhook:        revisionCanonicalWebhook,
			whURL:          "",
			whSVC:          "istiod-revision",
			whCA:           "ca",
			userManaged:    true,
		},
	}

	fail := admitv1.Fail
	fakeClient := kube.NewFakeClient(&admitv1.ValidatingWebhookConfiguration{
		TypeMeta: metav1.TypeMeta{},
		ObjectMeta: metav1.ObjectMeta{
			Name: "istiod-default-validator",
		},
		Webhooks: []admitv1.ValidatingWebhook{
			{
				Name: "random",
			},
			{
				FailurePolicy: &fail,
				Name:          "validation.istio.io",
			},
		},
	})
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			webhookConfig, err := tagWebhookConfigFromCanonicalWebhook(tc.webhook, "default", tc.istioNamespace)
			if err != nil {
				t.Fatalf("webhook parsing failed with error: %v", err)
			}
			webhookConfig, err = fixWhConfig(fakeClient, webhookConfig)
			if err != nil {
				t.Fatalf("webhook fixing failed with error: %v", err)
			}
			opts := &GenerateOptions{
				ManifestsPath: filepath.Join(env.IstioSrc, "manifests"),
			}
			if tc.userManaged {
				opts.UserManaged = true
			}
			wh, err := generateValidatingWebhook(webhookConfig, opts)
			if err != nil {
				t.Fatalf("tag webhook YAML generation failed with error: %v", err)
			}

			if tc.userManaged {
				// User created webhooks should not have operator labels, otherwise will be pruned.
				_, ok := wh.GetLabels()[operatorManaged]
				assert.Equal(t, ok, false)
			}

			for _, webhook := range wh.Webhooks {
				validationWhConf := webhook.ClientConfig

				// this is nil since we've already have one with failed FailurePolicy in the fake client
				if webhook.FailurePolicy != nil {
					t.Fatalf("expected FailurePolicy to be nil, got %v", *webhook.FailurePolicy)
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
		name                 string
		webhook              admitv1.MutatingWebhookConfiguration
		tagName              string
		whURL                string
		whSVC                string
		whCA                 string
		whReinvocationPolicy string
		numWebhooks          int
	}{
		{
			name:                 "webhook-pointing-to-service",
			webhook:              revisionCanonicalWebhook,
			tagName:              "canary",
			whURL:                "",
			whSVC:                "istiod-revision",
			whCA:                 "ca",
			whReinvocationPolicy: string(admitv1.NeverReinvocationPolicy),
			numWebhooks:          2,
		},
		{
			name:                 "webhook-pointing-to-url",
			webhook:              revisionCanonicalWebhookRemote,
			tagName:              "canary",
			whURL:                remoteInjectionURL,
			whSVC:                "",
			whCA:                 "ca",
			whReinvocationPolicy: string(admitv1.NeverReinvocationPolicy),
			numWebhooks:          2,
		},
		{
			name:                 "webhook-pointing-to-default-revision",
			webhook:              defaultRevisionCanonicalWebhook,
			tagName:              "canary",
			whURL:                "",
			whSVC:                "istiod",
			whCA:                 "ca",
			whReinvocationPolicy: string(admitv1.NeverReinvocationPolicy),
			numWebhooks:          2,
		},
		{
			name:                 "webhook-pointing-to-default-revision",
			webhook:              defaultRevisionCanonicalWebhook,
			tagName:              "default",
			whURL:                "",
			whSVC:                "istiod",
			whCA:                 "ca",
			whReinvocationPolicy: string(admitv1.NeverReinvocationPolicy),
			numWebhooks:          4,
		},
		{
			name:                 "webhook-pointing-to-default-revision-with-never-reinvocation-policy",
			webhook:              defaultRevisionCanonicalWebhookWithNeverReinvocationPolicy,
			tagName:              "default",
			whURL:                "",
			whSVC:                "istiod",
			whCA:                 "ca",
			whReinvocationPolicy: string(admitv1.NeverReinvocationPolicy),
			numWebhooks:          4,
		},
		{
			name:                 "webhook-pointing-to-default-revision-with-ifneeded-reinvocation-policy",
			webhook:              defaultRevisionCanonicalWebhookWithIfNeededReinvocationPolicy,
			tagName:              "default",
			whURL:                "",
			whSVC:                "istiod",
			whCA:                 "ca",
			whReinvocationPolicy: string(admitv1.IfNeededReinvocationPolicy),
			numWebhooks:          4,
		},
	}

	for _, tc := range tcs {
		webhookConfig, err := tagWebhookConfigFromCanonicalWebhook(tc.webhook, tc.tagName, "istio-system")
		if err != nil {
			t.Fatalf("webhook parsing failed with error: %v", err)
		}
		wh, err := generateMutatingWebhook(webhookConfig, &GenerateOptions{
			WebhookName:          "",
			ManifestsPath:        filepath.Join(env.IstioSrc, "manifests"),
			AutoInjectNamespaces: false,
			CustomLabels:         nil,
		})
		if err != nil {
			t.Fatalf("tag webhook YAML generation failed with error: %v", err)
		}

		// expect both namespace.sidecar-injector.istio.io and object.sidecar-injector.istio.io webhooks
		if len(wh.Webhooks) != tc.numWebhooks {
			t.Errorf("expected %d webhook(s) in MutatingWebhookConfiguration, found %d",
				tc.numWebhooks, len(wh.Webhooks))
		}
		tag, exists := wh.ObjectMeta.Labels[label.IoIstioTag.Name]
		if !exists {
			t.Errorf("expected tag webhook to have %s label, did not find", label.IoIstioTag.Name)
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

		// ensure all webhooks have the correct reinvocation policy
		for _, webhook := range wh.Webhooks {
			if webhook.ReinvocationPolicy == nil {
				t.Fatalf("expected reinvocation policy %q, got nil", tc.whReinvocationPolicy)
			}
			if string(*webhook.ReinvocationPolicy) != tc.whReinvocationPolicy {
				t.Fatalf("expected reinvocation policy %q, got %q", tc.whReinvocationPolicy, *webhook.ReinvocationPolicy)
			}
		}
	}
}

func testGenerateOption(t *testing.T, generate bool, assertFunc func(*testing.T, []admitv1.MutatingWebhook, []admitv1.MutatingWebhook)) {
	defaultWh := defaultRevisionCanonicalWebhook.DeepCopy()
	fakeClient := kube.NewFakeClient(defaultWh)

	opts := &GenerateOptions{
		Generate:       generate,
		Tag:            "default",
		Revision:       "default",
		IstioNamespace: "istio-system",
	}

	_, err := Generate(context.TODO(), fakeClient, opts)
	assert.NoError(t, err)

	wh, err := fakeClient.Kube().AdmissionregistrationV1().MutatingWebhookConfigurations().
		Get(context.Background(), "istio-sidecar-injector", metav1.GetOptions{})
	assert.NoError(t, err)

	assertFunc(t, wh.Webhooks, defaultWh.Webhooks)
}

func TestGenerateOptions(t *testing.T) {
	// Test generate option 'true', should not modify webhooks
	testGenerateOption(t, true, func(t *testing.T, actual, expected []admitv1.MutatingWebhook) {
		assert.Equal(t, actual, expected)
	})

	// Test generate option 'false', should modify webhooks
	testGenerateOption(t, false, func(t *testing.T, actual, expected []admitv1.MutatingWebhook) {
		if err := assert.Compare(actual, expected); err == nil {
			t.Errorf("expected diff between webhooks, got none")
		}
	})
}
