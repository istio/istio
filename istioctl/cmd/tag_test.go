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

package cmd

import (
	"bytes"
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	admit_v1 "k8s.io/api/admissionregistration/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/client-go/kubernetes/fake"

	"istio.io/api/label"
	"istio.io/istio/operator/pkg/helmreconciler"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/test/env"
)

var (
	defaultRevisionCanonicalWebhook = admit_v1.MutatingWebhookConfiguration{
		ObjectMeta: metav1.ObjectMeta{
			Name:   "istio-sidecar-injector",
			Labels: map[string]string{label.IoIstioRev.Name: "default"},
		},
		Webhooks: []admit_v1.MutatingWebhook{
			{
				Name: fmt.Sprintf("namespace.%s", istioInjectionWebhookSuffix),
				ClientConfig: admit_v1.WebhookClientConfig{
					Service: &admit_v1.ServiceReference{
						Namespace: "default",
						Name:      "istiod",
					},
				},
			},
			{
				Name: fmt.Sprintf("object.%s", istioInjectionWebhookSuffix),
				ClientConfig: admit_v1.WebhookClientConfig{
					Service: &admit_v1.ServiceReference{
						Namespace: "default",
						Name:      "istiod",
					},
				},
			},
		},
	}
	revisionCanonicalWebhook = admit_v1.MutatingWebhookConfiguration{
		ObjectMeta: metav1.ObjectMeta{
			Name:   "istio-sidecar-injector-revision",
			Labels: map[string]string{label.IoIstioRev.Name: "revision"},
		},
		Webhooks: []admit_v1.MutatingWebhook{
			{
				Name: fmt.Sprintf("namespace.%s", istioInjectionWebhookSuffix),
				ClientConfig: admit_v1.WebhookClientConfig{
					Service: &admit_v1.ServiceReference{
						Namespace: "default",
						Name:      "istiod-revision",
					},
				},
			},
			{
				Name: fmt.Sprintf("object.%s", istioInjectionWebhookSuffix),
				ClientConfig: admit_v1.WebhookClientConfig{
					Service: &admit_v1.ServiceReference{
						Namespace: "default",
						Name:      "istiod-revision",
					},
				},
			},
		},
	}
	remoteInjectionURL             = "random.injection.url.com"
	revisionCanonicalWebhookRemote = admit_v1.MutatingWebhookConfiguration{
		ObjectMeta: metav1.ObjectMeta{
			Name:   "istio-sidecar-injector-revision",
			Labels: map[string]string{label.IoIstioRev.Name: "revision"},
		},
		Webhooks: []admit_v1.MutatingWebhook{
			{
				Name: fmt.Sprintf("namespace.%s", istioInjectionWebhookSuffix),
				ClientConfig: admit_v1.WebhookClientConfig{
					URL: &remoteInjectionURL,
				},
			},
			{
				Name: fmt.Sprintf("object.%s", istioInjectionWebhookSuffix),
				ClientConfig: admit_v1.WebhookClientConfig{
					URL: &remoteInjectionURL,
				},
			},
		},
	}
)

func TestTagList(t *testing.T) {
	tcs := []struct {
		name           string
		webhooks       admit_v1.MutatingWebhookConfigurationList
		namespaces     corev1.NamespaceList
		outputMatches  []string
		outputExcludes []string
		error          string
	}{
		{
			name: "TestBasicTag",
			webhooks: admit_v1.MutatingWebhookConfigurationList{
				Items: []admit_v1.MutatingWebhookConfiguration{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name: "istio-revision-tag-sample",
							Labels: map[string]string{
								istioTagLabel:                         "sample",
								label.IoIstioRev.Name:                 "sample-revision",
								helmreconciler.IstioComponentLabelStr: "Pilot",
							},
						},
					},
				},
			},
			namespaces:     corev1.NamespaceList{},
			outputMatches:  []string{"sample", "sample-revision"},
			outputExcludes: []string{},
			error:          "",
		},
		{
			name: "TestNonTagWebhooksExcluded",
			webhooks: admit_v1.MutatingWebhookConfigurationList{
				Items: []admit_v1.MutatingWebhookConfiguration{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:   "istio-revision-test",
							Labels: map[string]string{label.IoIstioRev.Name: "test"},
						},
					},
				},
			},
			namespaces:     corev1.NamespaceList{},
			outputMatches:  []string{},
			outputExcludes: []string{"test"},
			error:          "",
		},
		{
			name: "TestNamespacesIncluded",
			webhooks: admit_v1.MutatingWebhookConfigurationList{
				Items: []admit_v1.MutatingWebhookConfiguration{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name: "istio-revision-test",
							Labels: map[string]string{
								label.IoIstioRev.Name: "revision",
								istioTagLabel:         "test",
							},
						},
					},
				},
			},
			namespaces: corev1.NamespaceList{
				Items: []corev1.Namespace{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:   "dependent",
							Labels: map[string]string{label.IoIstioRev.Name: "test"},
						},
					},
				},
			},
			outputMatches:  []string{"test", "revision", "dependent"},
			outputExcludes: []string{},
			error:          "",
		},
	}

	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			var out bytes.Buffer
			client := fake.NewSimpleClientset(tc.webhooks.DeepCopyObject(), tc.namespaces.DeepCopyObject())
			err := listTags(context.Background(), client, &out)
			if tc.error == "" && err != nil {
				t.Fatalf("expected no error, got %v", err)
			}
			if tc.error != "" {
				if err == nil {
					t.Fatalf("expected error to include \"%s\" but got none", tc.error)
				}
				if !strings.Contains(err.Error(), tc.error) {
					t.Fatalf("expected \"%s\" in error, got %v", tc.error, err)
				}
			}

			commandOutput := out.String()
			for _, s := range tc.outputMatches {
				if !strings.Contains(commandOutput, s) {
					t.Fatalf("expected \"%s\" in command output, got %s", s, commandOutput)
				}
			}
			for _, s := range tc.outputExcludes {
				if strings.Contains(commandOutput, s) {
					t.Fatalf("expected \"%s\" in command output, got %s", s, commandOutput)
				}
			}
		})
	}
}

func TestRemoveTag(t *testing.T) {
	tcs := []struct {
		name             string
		tag              string
		webhooksBefore   admit_v1.MutatingWebhookConfigurationList
		webhooksAfter    admit_v1.MutatingWebhookConfigurationList
		namespaces       corev1.NamespaceList
		outputMatches    []string
		skipConfirmation bool
		error            string
	}{
		{
			name: "TestSimpleRemove",
			tag:  "sample",
			webhooksBefore: admit_v1.MutatingWebhookConfigurationList{
				Items: []admit_v1.MutatingWebhookConfiguration{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:   "istio-revision-tag-sample",
							Labels: map[string]string{istioTagLabel: "sample"},
						},
					},
				},
			},
			webhooksAfter:    admit_v1.MutatingWebhookConfigurationList{},
			namespaces:       corev1.NamespaceList{},
			outputMatches:    []string{},
			skipConfirmation: true,
			error:            "",
		},
		{
			name: "TestWrongTagLabelNotRemoved",
			tag:  "sample",
			webhooksBefore: admit_v1.MutatingWebhookConfigurationList{
				Items: []admit_v1.MutatingWebhookConfiguration{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:   "istio-revision-tag-wrong",
							Labels: map[string]string{istioTagLabel: "wrong"},
						},
					},
				},
			},
			webhooksAfter: admit_v1.MutatingWebhookConfigurationList{
				Items: []admit_v1.MutatingWebhookConfiguration{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:   "istio-revision-tag-wrong",
							Labels: map[string]string{istioTagLabel: "wrong"},
						},
					},
				},
			},
			namespaces:       corev1.NamespaceList{},
			outputMatches:    []string{},
			skipConfirmation: true,
			error:            "cannot remove tag \"sample\"",
		},
		{
			name: "TestDeleteTagWithDependentNamespace",
			tag:  "match",
			webhooksBefore: admit_v1.MutatingWebhookConfigurationList{
				Items: []admit_v1.MutatingWebhookConfiguration{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:   "istio-revision-tag-match",
							Labels: map[string]string{istioTagLabel: "match"},
						},
					},
				},
			},
			webhooksAfter: admit_v1.MutatingWebhookConfigurationList{
				Items: []admit_v1.MutatingWebhookConfiguration{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:   "istio-revision-tag-match",
							Labels: map[string]string{istioTagLabel: "match"},
						},
					},
				},
			},
			namespaces: corev1.NamespaceList{
				Items: []corev1.Namespace{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:   "dependent",
							Labels: map[string]string{label.IoIstioRev.Name: "match"},
						},
					},
				},
			},
			outputMatches:    []string{"Caution, found 1 namespace(s) still injected by tag \"match\": dependent"},
			skipConfirmation: false,
			error:            "",
		},
	}

	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			var out bytes.Buffer
			client := fake.NewSimpleClientset(tc.webhooksBefore.DeepCopyObject(), tc.namespaces.DeepCopyObject())
			err := removeTag(context.Background(), client, tc.tag, tc.skipConfirmation, &out)
			if tc.error == "" && err != nil {
				t.Fatalf("expected no error, got %v", err)
			}
			if tc.error != "" {
				if err == nil {
					t.Fatalf("expected error to include \"%s\" but got none", tc.error)
				}
				if !strings.Contains(err.Error(), tc.error) {
					t.Fatalf("expected \"%s\" in error, got %v", tc.error, err)
				}
			}

			commandOutput := out.String()
			for _, s := range tc.outputMatches {
				if !strings.Contains(commandOutput, s) {
					t.Fatalf("expected \"%s\" in command output, got %s", s, commandOutput)
				}
			}

			// check mutating webhooks after run
			webhooksAfter, _ := client.AdmissionregistrationV1().MutatingWebhookConfigurations().List(context.Background(), metav1.ListOptions{})
			if len(webhooksAfter.Items) != len(tc.webhooksAfter.Items) {
				t.Fatalf("expected %d after running, got %d", len(tc.webhooksAfter.Items), len(webhooksAfter.Items))
			}
		})
	}
}

func TestSetTagErrors(t *testing.T) {
	tcs := []struct {
		name           string
		tag            string
		revision       string
		webhooksBefore admit_v1.MutatingWebhookConfigurationList
		namespaces     corev1.NamespaceList
		outputMatches  []string
		error          string
	}{
		{
			name:     "TestErrorWhenRevisionWithNameCollision",
			tag:      "revision",
			revision: "revision",
			webhooksBefore: admit_v1.MutatingWebhookConfigurationList{
				Items: []admit_v1.MutatingWebhookConfiguration{revisionCanonicalWebhook},
			},
			namespaces:    corev1.NamespaceList{},
			outputMatches: []string{},
			error:         "cannot create revision tag \"revision\"",
		},
	}

	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			var out bytes.Buffer

			client := fake.NewSimpleClientset(tc.webhooksBefore.DeepCopyObject(), tc.namespaces.DeepCopyObject())
			mockClient := kube.MockClient{
				Interface: client,
			}
			skipConfirmation = true
			err := setTag(context.Background(), mockClient, tc.tag, tc.revision, false, &out)
			if tc.error == "" && err != nil {
				t.Fatalf("expected no error, got %v", err)
			}
			if tc.error != "" {
				if err == nil {
					t.Fatalf("expected error to include \"%s\" but got none", tc.error)
				}
				if !strings.Contains(err.Error(), tc.error) {
					t.Fatalf("expected \"%s\" in error, got %v", tc.error, err)
				}
			}
		})
	}
}

func TestSetTagWebhookCreation(t *testing.T) {
	tcs := []struct {
		name    string
		webhook admit_v1.MutatingWebhookConfiguration
		tagName string
		whURL   string
		whSVC   string
	}{
		{
			name:    "webhook-pointing-to-service",
			webhook: revisionCanonicalWebhook,
			tagName: "canary",
			whURL:   "",
			whSVC:   "istiod-revision",
		},
		{
			name:    "webhook-pointing-to-url",
			webhook: revisionCanonicalWebhookRemote,
			tagName: "canary",
			whURL:   remoteInjectionURL,
			whSVC:   "",
		},
		{
			name:    "webhook-pointing-to-default-revision",
			webhook: defaultRevisionCanonicalWebhook,
			tagName: "canary",
			whURL:   "",
			whSVC:   "istiod",
		},
	}
	scheme := runtime.NewScheme()
	codecFactory := serializer.NewCodecFactory(scheme)
	deserializer := codecFactory.UniversalDeserializer()

	istioNamespace = "istio-system"
	for _, tc := range tcs {
		webhookConfig, err := tagWebhookConfigFromCanonicalWebhook(tc.webhook, tc.tagName)
		if err != nil {
			t.Fatalf("webhook parsing failed with error: %v", err)
		}
		webhookYAML, err := tagWebhookYAML(webhookConfig, filepath.Join(env.IstioSrc, "manifests"))
		if err != nil {
			t.Fatalf("tag webhook YAML generation failed with error: %v", err)
		}

		whObject, _, err := deserializer.Decode([]byte(webhookYAML), nil, &admit_v1.MutatingWebhookConfiguration{})
		if err != nil {
			t.Fatalf("could not parse webhook from generated YAML: %s", webhookYAML)
		}
		wh := whObject.(*admit_v1.MutatingWebhookConfiguration)

		// expect both namespace.sidecar-injector.istio.io and object.sidecar-injector.istio.io webhooks
		if len(wh.Webhooks) != 2 {
			t.Errorf("expected 1 webhook in MutatingWebhookConfiguration, found %d", len(wh.Webhooks))
		}
		tag, exists := wh.ObjectMeta.Labels[istioTagLabel]
		if !exists {
			t.Errorf("expected tag webhook to have %s label, did not find", istioTagLabel)
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
		}
	}
}
