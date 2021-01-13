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
	"strings"
	"testing"

	"istio.io/istio/pkg/test/kube"

	admit_v1 "k8s.io/api/admissionregistration/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	"istio.io/api/label"
	"istio.io/istio/operator/pkg/helmreconciler"
)

var (
	revisionCanonicalWebhook = admit_v1.MutatingWebhookConfiguration{
		ObjectMeta: metav1.ObjectMeta{
			Name:   "istio-sidecar-injector-revision",
			Labels: map[string]string{label.IstioRev: "revision"},
		},
		Webhooks: []admit_v1.MutatingWebhook{
			{
				Name: istioInjectionWebhookName,
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
			Labels: map[string]string{label.IstioRev: "revision"},
		},
		Webhooks: []admit_v1.MutatingWebhook{
			{
				Name: istioInjectionWebhookName,
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
								label.IstioRev:                        "sample-revision",
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
							Labels: map[string]string{label.IstioRev: "test"},
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
								label.IstioRev: "revision",
								istioTagLabel:  "test",
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
							Labels: map[string]string{label.IstioRev: "test"},
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
							Labels: map[string]string{label.IstioRev: "match"},
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
			err := setTag(context.Background(), mockClient, tc.tag, tc.revision, &out)
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
		webhook              admit_v1.MutatingWebhookConfiguration
		tagName              string
		expectedSubstrings   []string
		unexpectedSubstrings []string
	}{
		{
			webhook: revisionCanonicalWebhook,
			tagName: "canary",
			expectedSubstrings: []string{
				"istio.io/rev: revision",
				"istio.io/tag: canary",
				"path: \"/inject\"",
				"caBundle: \"\"",
				"key: \"sidecar.istio.io/inject\"",
			},
			unexpectedSubstrings: []string{"url"},
		},
		{
			webhook: revisionCanonicalWebhookRemote,
			tagName: "canary",
			expectedSubstrings: []string{
				remoteInjectionURL,
				"istio.io/rev: revision",
				"istio.io/tag: canary",
				"caBundle: \"\"",
				"key: \"sidecar.istio.io/inject\"",
			},
			unexpectedSubstrings: []string{"service", "path:"},
		},
	}

	for _, tc := range tcs {
		webhookConfig, err := tagWebhookConfigFromCanonicalWebhook(tc.webhook, tc.tagName)
		if err != nil {
			t.Fatalf("webhook parsing failed with error: %v", err)
		}
		webhookYAML, err := tagWebhookYAML(webhookConfig)
		if err != nil {
			t.Fatalf("tag webhook YAML generation failed with error: %v", err)
		}

		for _, s := range tc.expectedSubstrings {
			if !strings.Contains(webhookYAML, s) {
				t.Errorf("expected substring in tag webhook YAML: %s, did not find", s)
			}
		}
		for _, s := range tc.unexpectedSubstrings {
			if strings.Contains(webhookYAML, s) {
				t.Errorf("unexpected substring in tag webhook YAML: %s", s)
			}
		}
	}
}
