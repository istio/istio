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
	"bytes"
	"context"
	"strings"
	"testing"

	admitv1 "k8s.io/api/admissionregistration/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"

	"istio.io/api/label"
	"istio.io/istio/istioctl/pkg/util"
	"istio.io/istio/pkg/kube"
)

func TestTagList(t *testing.T) {
	tcs := []struct {
		name          string
		webhooks      admitv1.MutatingWebhookConfigurationList
		services      corev1.ServiceList
		namespaces    corev1.NamespaceList
		outputMatches []string
		error         string
	}{
		{
			name: "TestBasicTag_mwh",
			webhooks: admitv1.MutatingWebhookConfigurationList{
				Items: []admitv1.MutatingWebhookConfiguration{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name: "istio-revision-tag-sample",
							Labels: map[string]string{
								label.IoIstioTag.Name:         "sample",
								label.IoIstioRev.Name:         "sample-revision",
								"operator.istio.io/component": "Pilot",
							},
						},
					},
				},
			},
			services:      corev1.ServiceList{},
			namespaces:    corev1.NamespaceList{},
			outputMatches: []string{"sample", "sample-revision"},
			error:         "",
		},
		{
			name:     "TestBasicTag_svc",
			webhooks: admitv1.MutatingWebhookConfigurationList{},
			services: corev1.ServiceList{
				Items: []corev1.Service{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "istio-service-tag-sample",
							Namespace: "istio-system",
							Labels: map[string]string{
								label.IoIstioTag.Name:         "sample",
								label.IoIstioRev.Name:         "sample-revision",
								"operator.istio.io/component": "Pilot",
							},
						},
					},
				},
			},
			namespaces:    corev1.NamespaceList{},
			outputMatches: []string{"sample", "sample-revision"},
			error:         "",
		},
		{
			name: "TestBasicTag_both",
			webhooks: admitv1.MutatingWebhookConfigurationList{
				Items: []admitv1.MutatingWebhookConfiguration{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name: "istio-revision-tag-sample",
							Labels: map[string]string{
								label.IoIstioTag.Name:         "sample",
								label.IoIstioRev.Name:         "sample-revision",
								"operator.istio.io/component": "Pilot",
							},
						},
					},
				},
			},
			services: corev1.ServiceList{
				Items: []corev1.Service{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "istio-service-tag-sample",
							Namespace: "istio-system",
							Labels: map[string]string{
								label.IoIstioTag.Name:         "sample",
								label.IoIstioRev.Name:         "sample-revision",
								"operator.istio.io/component": "Pilot",
							},
						},
					},
				},
			},
			namespaces:    corev1.NamespaceList{},
			outputMatches: []string{"sample", "sample-revision"},
			error:         "",
		},

		{
			name: "TestNonTagWebhooksIncluded_mwh",
			webhooks: admitv1.MutatingWebhookConfigurationList{
				Items: []admitv1.MutatingWebhookConfiguration{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:   "istio-revision-test",
							Labels: map[string]string{label.IoIstioRev.Name: "test"},
						},
					},
				},
			},
			services:      corev1.ServiceList{},
			namespaces:    corev1.NamespaceList{},
			outputMatches: []string{"test"},
			error:         "",
		},
		{
			name:     "TestNonTagWebhooksIncluded_svc",
			webhooks: admitv1.MutatingWebhookConfigurationList{},
			services: corev1.ServiceList{
				Items: []corev1.Service{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "istio-service-tag-test",
							Namespace: "istio-system",
							Labels:    map[string]string{label.IoIstioRev.Name: "test"},
						},
					},
				},
			},
			namespaces:    corev1.NamespaceList{},
			outputMatches: []string{"test"},
			error:         "",
		},
		{
			name: "TestNonTagWebhooksIncluded_both",
			webhooks: admitv1.MutatingWebhookConfigurationList{
				Items: []admitv1.MutatingWebhookConfiguration{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:   "istio-revision-test",
							Labels: map[string]string{label.IoIstioRev.Name: "test"},
						},
					},
				},
			},
			services: corev1.ServiceList{
				Items: []corev1.Service{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "istio-service-tag-test",
							Namespace: "istio-system",
							Labels:    map[string]string{label.IoIstioRev.Name: "test"},
						},
					},
				},
			},
			namespaces:    corev1.NamespaceList{},
			outputMatches: []string{"test"},
			error:         "",
		},
		{
			name: "TestNamespacesIncluded_mwh",
			webhooks: admitv1.MutatingWebhookConfigurationList{
				Items: []admitv1.MutatingWebhookConfiguration{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name: "istio-revision-test",
							Labels: map[string]string{
								label.IoIstioRev.Name: "revision",
								label.IoIstioTag.Name: "test",
							},
						},
					},
				},
			},
			services: corev1.ServiceList{},
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
			outputMatches: []string{"test", "revision", "dependent"},
			error:         "",
		},
		{
			name:     "TestNamespacesIncluded_svc",
			webhooks: admitv1.MutatingWebhookConfigurationList{},
			services: corev1.ServiceList{
				Items: []corev1.Service{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "istio-revision-tag-test",
							Namespace: "istio-system",
							Labels: map[string]string{
								label.IoIstioRev.Name: "revision",
								label.IoIstioTag.Name: "test",
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
			outputMatches: []string{"test", "revision", "dependent"},
			error:         "",
		},
		{
			name: "TestNamespacesIncluded_both",
			webhooks: admitv1.MutatingWebhookConfigurationList{
				Items: []admitv1.MutatingWebhookConfiguration{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name: "istio-revision-test",
							Labels: map[string]string{
								label.IoIstioRev.Name: "revision",
								label.IoIstioTag.Name: "test",
							},
						},
					},
				},
			},
			services: corev1.ServiceList{
				Items: []corev1.Service{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "istio-revision-tag-test",
							Namespace: "istio-system",
							Labels: map[string]string{
								label.IoIstioRev.Name: "revision",
								label.IoIstioTag.Name: "test",
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
			outputMatches: []string{"test", "revision", "dependent"},
			error:         "",
		},
	}

	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			var out bytes.Buffer
			client := fake.NewClientset(tc.webhooks.DeepCopyObject(), tc.namespaces.DeepCopyObject(), tc.services.DeepCopyObject())
			outputFormat = util.JSONFormat
			err := listTags(context.Background(), client, "istio-system", &out)
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
		})
	}
}

func TestRemoveTag(t *testing.T) {
	tcs := []struct {
		name             string
		tag              string
		webhooksBefore   admitv1.MutatingWebhookConfigurationList
		webhooksAfter    admitv1.MutatingWebhookConfigurationList
		servicesBefore   corev1.ServiceList
		servicesAfter    corev1.ServiceList
		namespaces       corev1.NamespaceList
		outputMatches    []string
		skipConfirmation bool
		error            string
	}{
		{
			name: "TestSimpleRemove",
			tag:  "sample",
			webhooksBefore: admitv1.MutatingWebhookConfigurationList{
				Items: []admitv1.MutatingWebhookConfiguration{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:   "istio-revision-tag-sample",
							Labels: map[string]string{label.IoIstioTag.Name: "sample"},
						},
					},
				},
			},
			webhooksAfter:    admitv1.MutatingWebhookConfigurationList{},
			servicesBefore:   corev1.ServiceList{},
			servicesAfter:    corev1.ServiceList{},
			namespaces:       corev1.NamespaceList{},
			outputMatches:    []string{},
			skipConfirmation: true,
			error:            "",
		},
		{
			name:           "TestSimpleRemove_svc",
			tag:            "sample",
			webhooksBefore: admitv1.MutatingWebhookConfigurationList{},
			webhooksAfter:  admitv1.MutatingWebhookConfigurationList{},
			servicesBefore: corev1.ServiceList{
				Items: []corev1.Service{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "istio-service-tag-sample",
							Namespace: "istio-system",
							Labels:    map[string]string{label.IoIstioTag.Name: "sample"},
						},
					},
				},
			},
			servicesAfter:    corev1.ServiceList{},
			namespaces:       corev1.NamespaceList{},
			outputMatches:    []string{},
			skipConfirmation: true,
			error:            "",
		},
		{
			name: "TestSimpleRemove_both",
			tag:  "sample",
			webhooksBefore: admitv1.MutatingWebhookConfigurationList{
				Items: []admitv1.MutatingWebhookConfiguration{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:   "istio-revision-tag-sample",
							Labels: map[string]string{label.IoIstioTag.Name: "sample"},
						},
					},
				},
			},
			webhooksAfter: admitv1.MutatingWebhookConfigurationList{},
			servicesBefore: corev1.ServiceList{
				Items: []corev1.Service{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "istio-service-tag-sample",
							Namespace: "istio-system",
							Labels:    map[string]string{label.IoIstioTag.Name: "sample"},
						},
					},
				},
			},
			servicesAfter:    corev1.ServiceList{},
			namespaces:       corev1.NamespaceList{},
			outputMatches:    []string{},
			skipConfirmation: true,
			error:            "",
		},
		{
			name: "TestWrongTagLabelNotRemoved_mwh",
			tag:  "sample",
			webhooksBefore: admitv1.MutatingWebhookConfigurationList{
				Items: []admitv1.MutatingWebhookConfiguration{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:   "istio-revision-tag-wrong",
							Labels: map[string]string{label.IoIstioTag.Name: "wrong"},
						},
					},
				},
			},
			webhooksAfter: admitv1.MutatingWebhookConfigurationList{
				Items: []admitv1.MutatingWebhookConfiguration{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:   "istio-revision-tag-wrong",
							Labels: map[string]string{label.IoIstioTag.Name: "wrong"},
						},
					},
				},
			},
			servicesBefore:   corev1.ServiceList{},
			servicesAfter:    corev1.ServiceList{},
			namespaces:       corev1.NamespaceList{},
			outputMatches:    []string{},
			skipConfirmation: true,
			error:            "cannot remove tag \"sample\"",
		},
		{
			name:           "TestWrongTagLabelNotRemoved_svc",
			tag:            "sample",
			webhooksBefore: admitv1.MutatingWebhookConfigurationList{},
			webhooksAfter:  admitv1.MutatingWebhookConfigurationList{},
			servicesBefore: corev1.ServiceList{
				Items: []corev1.Service{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "istio-service-tag-wrong",
							Namespace: "istio-system",
							Labels:    map[string]string{label.IoIstioTag.Name: "wrong"},
						},
					},
				},
			},
			servicesAfter: corev1.ServiceList{
				Items: []corev1.Service{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "istio-service-tag-wrong",
							Namespace: "istio-system",
							Labels:    map[string]string{label.IoIstioTag.Name: "wrong"},
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
			name: "TestWrongTagLabelNotRemoved_both",
			tag:  "sample",
			webhooksBefore: admitv1.MutatingWebhookConfigurationList{
				Items: []admitv1.MutatingWebhookConfiguration{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:   "istio-revision-tag-wrong",
							Labels: map[string]string{label.IoIstioTag.Name: "wrong"},
						},
					},
				},
			},
			webhooksAfter: admitv1.MutatingWebhookConfigurationList{
				Items: []admitv1.MutatingWebhookConfiguration{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:   "istio-revision-tag-wrong",
							Labels: map[string]string{label.IoIstioTag.Name: "wrong"},
						},
					},
				},
			},
			servicesBefore: corev1.ServiceList{
				Items: []corev1.Service{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "istio-service-tag-wrong",
							Namespace: "istio-system",
							Labels:    map[string]string{label.IoIstioTag.Name: "wrong"},
						},
					},
				},
			},
			servicesAfter: corev1.ServiceList{
				Items: []corev1.Service{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "istio-service-tag-wrong",
							Namespace: "istio-system",
							Labels:    map[string]string{label.IoIstioTag.Name: "wrong"},
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
			name: "TestDeleteTagWithDependentNamespace_mwh",
			tag:  "match",
			webhooksBefore: admitv1.MutatingWebhookConfigurationList{
				Items: []admitv1.MutatingWebhookConfiguration{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:   "istio-revision-tag-match",
							Labels: map[string]string{label.IoIstioTag.Name: "match"},
						},
					},
				},
			},
			webhooksAfter: admitv1.MutatingWebhookConfigurationList{
				Items: []admitv1.MutatingWebhookConfiguration{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:   "istio-revision-tag-match",
							Labels: map[string]string{label.IoIstioTag.Name: "match"},
						},
					},
				},
			},
			servicesBefore: corev1.ServiceList{},
			servicesAfter:  corev1.ServiceList{},
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
		{
			name:           "TestDeleteTagWithDependentNamespace_svc",
			tag:            "match",
			webhooksBefore: admitv1.MutatingWebhookConfigurationList{},
			webhooksAfter:  admitv1.MutatingWebhookConfigurationList{},
			servicesBefore: corev1.ServiceList{
				Items: []corev1.Service{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "istio-service-tag-match",
							Namespace: "istio-system",
							Labels:    map[string]string{label.IoIstioTag.Name: "match"},
						},
					},
				},
			},
			servicesAfter: corev1.ServiceList{
				Items: []corev1.Service{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "istio-service-tag-match",
							Namespace: "istio-system",
							Labels:    map[string]string{label.IoIstioTag.Name: "match"},
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
		{
			name: "TestDeleteTagWithDependentNamespace_both",
			tag:  "match",
			webhooksBefore: admitv1.MutatingWebhookConfigurationList{
				Items: []admitv1.MutatingWebhookConfiguration{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:   "istio-revision-tag-match",
							Labels: map[string]string{label.IoIstioTag.Name: "match"},
						},
					},
				},
			},
			webhooksAfter: admitv1.MutatingWebhookConfigurationList{
				Items: []admitv1.MutatingWebhookConfiguration{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:   "istio-revision-tag-match",
							Labels: map[string]string{label.IoIstioTag.Name: "match"},
						},
					},
				},
			},
			servicesBefore: corev1.ServiceList{
				Items: []corev1.Service{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "istio-service-tag-match",
							Namespace: "istio-system",
							Labels:    map[string]string{label.IoIstioTag.Name: "match"},
						},
					},
				},
			},
			servicesAfter: corev1.ServiceList{
				Items: []corev1.Service{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "istio-service-tag-match",
							Namespace: "istio-system",
							Labels:    map[string]string{label.IoIstioTag.Name: "match"},
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
			client := fake.NewClientset(tc.webhooksBefore.DeepCopyObject(), tc.namespaces.DeepCopyObject(), tc.servicesBefore.DeepCopyObject())
			err := removeTag(context.Background(), client, tc.tag, tc.skipConfirmation, "istio-system", &out)
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

			// Validate the state of webhooks and services after the removal
			// For webhooks
			webhooksAfter, _ := client.AdmissionregistrationV1().MutatingWebhookConfigurations().List(context.Background(), metav1.ListOptions{})
			if len(webhooksAfter.Items) != len(tc.webhooksAfter.Items) {
				t.Fatalf("expected %d webhooks, got %d", len(tc.webhooksAfter.Items), len(webhooksAfter.Items))
			}
			// For services
			servicesAfter, _ := client.CoreV1().Services("istio-system").List(context.Background(), metav1.ListOptions{})
			if len(servicesAfter.Items) != len(tc.servicesAfter.Items) {
				t.Fatalf("expected %d services, got %d", len(tc.servicesAfter.Items), len(servicesAfter.Items))
			}
		})
	}
}

func TestSetTagErrors(t *testing.T) {
	serviceBefore := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "istio-service-tag-revision",
			Namespace: "istio-system",
			Labels: map[string]string{
				label.IoIstioRev.Name: "revision",
				"app":                 "istiod",
			},
		},
	}
	tcs := []struct {
		name          string
		tag           string
		revision      string
		webhookBefore *admitv1.MutatingWebhookConfiguration
		serviceBefore *corev1.Service
		outputMatches []string
		error         string
	}{
		{
			name:          "TestErrorWhenRevisionWithNameCollision",
			tag:           "revision",
			revision:      "revision",
			webhookBefore: &revisionCanonicalWebhook,
			serviceBefore: nil,
			outputMatches: []string{},
			error:         "cannot create revision tag \"revision\"",
		},
		{
			name:          "TestErrorWhenRevisionWithNameCollision_svc",
			tag:           "revision",
			revision:      "revision",
			webhookBefore: nil,
			serviceBefore: serviceBefore,
			outputMatches: []string{},
			error:         "cannot create revision tag \"revision\"",
		},
		{
			name:          "TestErrorWhenRevisionWithNameCollision_both",
			tag:           "revision",
			revision:      "revision",
			webhookBefore: &revisionCanonicalWebhook,
			serviceBefore: serviceBefore,
			outputMatches: []string{},
			error:         "cannot create revision tag \"revision\"",
		},
	}

	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			var out bytes.Buffer
			objects := []runtime.Object{}
			if tc.webhookBefore != nil {
				objects = append(objects, tc.webhookBefore)
			}
			if tc.serviceBefore != nil {
				objects = append(objects, tc.serviceBefore)
			}
			client := kube.NewFakeClient(objects...)

			skipConfirmation = true
			err := setTag(context.Background(), client, tc.tag, tc.revision, "istio-system", false, &out, nil)
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
