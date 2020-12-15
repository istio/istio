package cmd

import (
	"bytes"
	"context"
	"github.com/davecgh/go-spew/spew"
	"istio.io/api/label"
	admit_v1 "k8s.io/api/admissionregistration/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
	"reflect"
	"strings"
	"testing"
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
								istioTagLabel:  "sample",
								label.IstioRev: "sample-revision"},
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
			err := listTags(context.TODO(), client, &out)
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
		name           string
		tag            string
		webhooksBefore admit_v1.MutatingWebhookConfigurationList
		webhooksAfter  admit_v1.MutatingWebhookConfigurationList
		namespaces     corev1.NamespaceList
		outputMatches  []string
		error          string
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
			webhooksAfter: admit_v1.MutatingWebhookConfigurationList{},
			namespaces:    corev1.NamespaceList{},
			outputMatches: []string{},
			error:         "",
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
			namespaces:    corev1.NamespaceList{},
			outputMatches: []string{},
			error:         "revision tag sample does not exist",
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
			webhooksAfter: admit_v1.MutatingWebhookConfigurationList{},
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
			outputMatches: []string{"found 1 namespaces still pointing to tag \"match\": dependent"},
			error:         "",
		},
	}

	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			var out bytes.Buffer
			client := fake.NewSimpleClientset(tc.webhooksBefore.DeepCopyObject(), tc.namespaces.DeepCopyObject())
			err := removeTag(context.TODO(), client, tc.tag, &out)
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
			webhooksAfter, _ := client.AdmissionregistrationV1().MutatingWebhookConfigurations().List(context.TODO(), metav1.ListOptions{})
			if len(webhooksAfter.Items) != len(tc.webhooksAfter.Items) {
				t.Fatalf("expected %d after running, got %d", len(tc.webhooksAfter.Items), len(webhooksAfter.Items))
			}
		})
	}
}

func TestApplyTag(t *testing.T) {
	revisionCanonicalWebhook := admit_v1.MutatingWebhookConfiguration{
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
	injectionURL := "random.injection.url.com"
	revisionCanonicalWebhookRemote := admit_v1.MutatingWebhookConfiguration{
		ObjectMeta: metav1.ObjectMeta{
			Name:   "istio-sidecar-injector-revision",
			Labels: map[string]string{label.IstioRev: "revision"},
		},
		Webhooks: []admit_v1.MutatingWebhook{
			{
				Name: istioInjectionWebhookName,
				ClientConfig: admit_v1.WebhookClientConfig{
					URL: &injectionURL,
				},
			},
		},
	}
	tcs := []struct {
		name           string
		tag            string
		revision       string
		webhooksBefore admit_v1.MutatingWebhookConfigurationList
		webhooksAfter  admit_v1.MutatingWebhookConfigurationList
		namespaces     corev1.NamespaceList
		outputMatches  []string
		error          string
	}{
		{
			name:     "TestSimpleCreate",
			tag:      "test",
			revision: "revision",
			webhooksBefore: admit_v1.MutatingWebhookConfigurationList{
				Items: []admit_v1.MutatingWebhookConfiguration{revisionCanonicalWebhook},
			},
			webhooksAfter: admit_v1.MutatingWebhookConfigurationList{
				Items: []admit_v1.MutatingWebhookConfiguration{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name: "istio-revision-tag-test",
							Labels: map[string]string{
								label.IstioRev: "revision",
								istioTagLabel:  "test",
							},
						},
						Webhooks: []admit_v1.MutatingWebhook{
							{
								Name: istioInjectionWebhookName,
								ClientConfig: admit_v1.WebhookClientConfig{
									Service: &admit_v1.ServiceReference{
										Namespace: "default",
										Name:      "istiod-revision",
									},
									CABundle: []byte(""),
								},
								NamespaceSelector: &metav1.LabelSelector{
									MatchExpressions: []metav1.LabelSelectorRequirement{
										{
											Key: label.IstioRev, Operator: metav1.LabelSelectorOpIn, Values: []string{"test"},
										},
										{
											Key: "istio-injection", Operator: metav1.LabelSelectorOpDoesNotExist,
										},
									},
								},
							},
						},
					},
					revisionCanonicalWebhook,
				},
			},
			namespaces:    corev1.NamespaceList{},
			outputMatches: []string{},
			error:         "",
		},
		{
			name:     "TestCreateWithURL",
			tag:      "test",
			revision: "revision",
			webhooksBefore: admit_v1.MutatingWebhookConfigurationList{
				Items: []admit_v1.MutatingWebhookConfiguration{revisionCanonicalWebhookRemote},
			},
			webhooksAfter: admit_v1.MutatingWebhookConfigurationList{
				Items: []admit_v1.MutatingWebhookConfiguration{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name: "istio-revision-tag-test",
							Labels: map[string]string{
								label.IstioRev: "revision",
								istioTagLabel:  "test",
							},
						},
						Webhooks: []admit_v1.MutatingWebhook{
							{
								Name: istioInjectionWebhookName,
								ClientConfig: admit_v1.WebhookClientConfig{
									URL:      &injectionURL,
									CABundle: []byte(""),
								},
								NamespaceSelector: &metav1.LabelSelector{
									MatchExpressions: []metav1.LabelSelectorRequirement{
										{
											Key: label.IstioRev, Operator: metav1.LabelSelectorOpIn, Values: []string{"test"},
										},
										{
											Key: "istio-injection", Operator: metav1.LabelSelectorOpDoesNotExist,
										},
									},
								},
							},
						},
					},
					revisionCanonicalWebhookRemote,
				},
			},
			namespaces:    corev1.NamespaceList{},
			outputMatches: []string{},
			error:         "",
		},
	}

	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			var out bytes.Buffer
			client := fake.NewSimpleClientset(tc.webhooksBefore.DeepCopyObject(), tc.namespaces.DeepCopyObject())
			err := applyTag(context.TODO(), client, tc.tag, tc.revision, &out)
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

			webhooksAfter, _ := client.AdmissionregistrationV1().MutatingWebhookConfigurations().List(context.TODO(), metav1.ListOptions{})
			if len(webhooksAfter.Items) != len(tc.webhooksAfter.Items) {
				t.Fatalf("expected %d after running, got %d", len(tc.webhooksAfter.Items), len(webhooksAfter.Items))
			}
			for i, w := range webhooksAfter.Items {
				if !reflect.DeepEqual(w, tc.webhooksAfter.Items[i]) {
					t.Fatalf("expected webhook %v not equal to actual %v\n", spew.Sdump(tc.webhooksAfter.Items[i]), spew.Sdump(w))
				}
			}
		})
	}
}
