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
	"fmt"
	"strings"
	"testing"

	admitv1 "k8s.io/api/admissionregistration/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	"istio.io/api/label"
	"istio.io/istio/istioctl/pkg/util"
	"istio.io/istio/pkg/kube"
)

var resourceTypes = []string{"both", "svc", "mwh"}

func TestTagList(t *testing.T) {
	type testCase struct {
		name          string
		webhooks      admitv1.MutatingWebhookConfigurationList
		services      corev1.ServiceList
		namespaces    corev1.NamespaceList
		outputMatches []string
	}
	makeTc := func(name, tag, rev, namespace, resourceType string) testCase {
		labels := map[string]string{
			"operator.istio.io/component": "Pilot",
		}
		outputMatches := []string{}
		if tag != "" {
			labels[label.IoIstioTag.Name] = tag
			outputMatches = append(outputMatches, tag)
		}
		if rev != "" {
			labels[label.IoIstioRev.Name] = rev
			outputMatches = append(outputMatches, rev)
		}

		webhooks := admitv1.MutatingWebhookConfigurationList{}
		services := corev1.ServiceList{}

		switch resourceType {
		case "mwh":
			webhooks = admitv1.MutatingWebhookConfigurationList{
				Items: []admitv1.MutatingWebhookConfiguration{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:   fmt.Sprintf("istio-revision-tag-%s", tag),
							Labels: labels,
						},
					},
				},
			}
		case "svc":
			services = corev1.ServiceList{
				Items: []corev1.Service{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:      fmt.Sprintf("istio-service-tag-%s", tag),
							Namespace: "istio-system",
							Labels:    labels,
						},
					},
				},
			}
		case "both":
			webhooks = admitv1.MutatingWebhookConfigurationList{
				Items: []admitv1.MutatingWebhookConfiguration{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:   fmt.Sprintf("istio-revision-tag-%s", tag),
							Labels: labels,
						},
					},
				},
			}
			services = corev1.ServiceList{
				Items: []corev1.Service{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:      fmt.Sprintf("istio-service-tag-%s", tag),
							Namespace: "istio-system",
							Labels:    labels,
						},
					},
				},
			}
		}
		namespaces := corev1.NamespaceList{}
		if namespace != "" {
			namespaces = corev1.NamespaceList{
				Items: []corev1.Namespace{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:   namespace,
							Labels: map[string]string{label.IoIstioRev.Name: tag},
						},
					},
				},
			}
			outputMatches = append(outputMatches, namespace)
		}

		return testCase{
			name:          fmt.Sprintf("%s_%s", name, resourceType),
			webhooks:      webhooks,
			services:      services,
			namespaces:    namespaces,
			outputMatches: outputMatches,
		}
	}
	tcs := []testCase{}

	for _, rt := range resourceTypes {
		basicTag := makeTc("TestBasicTag", "sample", "sample-revision", "", rt)
		nonTagObjectIncluded := makeTc("TestNonTagObjectIncluded", "", "test", "", rt)
		namespacesIncluded := makeTc("TestNamespacesIncluded", "test", "revision", "dependent", rt)
		tcs = append(tcs, basicTag, nonTagObjectIncluded, namespacesIncluded)
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			var out bytes.Buffer
			client := fake.NewClientset(tc.webhooks.DeepCopyObject(), tc.namespaces.DeepCopyObject(), tc.services.DeepCopyObject())
			outputFormat = util.JSONFormat
			err := listTags(context.Background(), client, "istio-system", &out)
			if err != nil {
				t.Fatalf("expected no error, got %v", err)
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
	type testCase struct {
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
	}
	makeTc := func(name, existingTag, tagToDelete, resourcesToLook string, shouldDelete bool) testCase {
		webhooksBefore := admitv1.MutatingWebhookConfigurationList{}
		webhooksAfter := admitv1.MutatingWebhookConfigurationList{}
		servicesBefore := corev1.ServiceList{}
		servicesAfter := corev1.ServiceList{}

		switch resourcesToLook {
		case "mwh":
			webhooksBefore = admitv1.MutatingWebhookConfigurationList{
				Items: []admitv1.MutatingWebhookConfiguration{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:   fmt.Sprintf("istio-revision-tag-%s", existingTag),
							Labels: map[string]string{label.IoIstioTag.Name: existingTag},
						},
					},
				},
			}
		case "svc":
			servicesBefore = corev1.ServiceList{
				Items: []corev1.Service{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:      fmt.Sprintf("istio-service-tag-%s", existingTag),
							Namespace: "istio-system",
							Labels:    map[string]string{label.IoIstioTag.Name: existingTag},
						},
					},
				},
			}
		case "both":
			webhooksBefore = admitv1.MutatingWebhookConfigurationList{
				Items: []admitv1.MutatingWebhookConfiguration{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:   fmt.Sprintf("istio-revision-tag-%s", existingTag),
							Labels: map[string]string{label.IoIstioTag.Name: existingTag},
						},
					},
				},
			}
			servicesBefore = corev1.ServiceList{
				Items: []corev1.Service{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:      fmt.Sprintf("istio-service-tag-%s", existingTag),
							Namespace: "istio-system",
							Labels:    map[string]string{label.IoIstioTag.Name: existingTag},
						},
					},
				},
			}
		}

		if !shouldDelete {
			webhooksAfter = webhooksBefore
			servicesAfter = servicesBefore
		}

		return testCase{
			name:             fmt.Sprintf("%s_%s", name, resourcesToLook),
			tag:              tagToDelete,
			webhooksBefore:   webhooksBefore,
			webhooksAfter:    webhooksAfter,
			servicesBefore:   servicesBefore,
			servicesAfter:    servicesAfter,
			namespaces:       corev1.NamespaceList{},
			outputMatches:    []string{},
			skipConfirmation: true,
			error:            "",
		}
	}
	resourceTypes := []string{"both", "svc", "mwh"}
	tcs := []testCase{}

	for _, rt := range resourceTypes {
		// simple remove should succeed, base case
		simpleRemove := makeTc("TestSimpleRemove", "sample", "sample", rt, true)

		// Attempting to remove a non existant tag should error
		wrongTagNotRemoved := makeTc("TestWrongTagLabelNotRemoved", "wrong", "sample", rt, false)
		wrongTagNotRemoved.error = "cannot remove tag \"sample\""

		// If tag is used in another namespace, it should be reported
		deleteTagWithDependentNamespace := makeTc("TestDeleteTagWithDependentNamespace", "match", "match", rt, false)
		deleteTagWithDependentNamespace.namespaces = corev1.NamespaceList{
			Items: []corev1.Namespace{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:   "dependent",
						Labels: map[string]string{label.IoIstioRev.Name: "match"},
					},
				},
			},
		}
		deleteTagWithDependentNamespace.outputMatches = []string{"Caution, found 1 namespace(s) still injected by tag \"match\": dependent"}
		deleteTagWithDependentNamespace.skipConfirmation = false

		tcs = append(tcs, simpleRemove, wrongTagNotRemoved, deleteTagWithDependentNamespace)
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

			// check mutating webhooks after run
			webhooksAfter, _ := client.AdmissionregistrationV1().MutatingWebhookConfigurations().List(context.Background(), metav1.ListOptions{})
			servicesAfter, _ := client.CoreV1().Services("istio-system").List(context.Background(), metav1.ListOptions{})
			if len(webhooksAfter.Items) != len(tc.webhooksAfter.Items) {
				t.Fatalf("expected %d mutating webhooks after running, got %d", len(tc.webhooksAfter.Items), len(webhooksAfter.Items))
			}

			if len(servicesAfter.Items) != len(tc.servicesAfter.Items) {
				t.Fatalf("expected %d services after running, got %d", len(tc.webhooksAfter.Items), len(webhooksAfter.Items))
			}
		})
	}
}

func TestSetTagErrors(t *testing.T) {
	tcs := []struct {
		name          string
		tag           string
		revision      string
		webhookBefore *admitv1.MutatingWebhookConfiguration
		outputMatches []string
		error         string
	}{
		{
			name:          "TestErrorWhenRevisionWithNameCollision",
			tag:           "revision",
			revision:      "revision",
			webhookBefore: &revisionCanonicalWebhook,
			outputMatches: []string{},
			error:         "cannot create revision tag \"revision\"",
		},
	}

	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			var out bytes.Buffer

			client := kube.NewFakeClient(tc.webhookBefore)
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
