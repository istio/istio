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

package agentgateway

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	"istio.io/istio/pkg/config/schema/gvk"
	"istio.io/istio/pkg/kube/krt"
	"istio.io/istio/pkg/kube/krt/krttest"
	"istio.io/istio/pkg/ptr"
	"istio.io/istio/pkg/test"
)

func TestBackendTLSPolicyRejectsMissingOrEmptyCACertificate(t *testing.T) {
	const (
		namespace   = "default"
		policyName  = "backend-tls"
		backendName = "backend"
		gatewayName = "gateway"
		generation  = int64(7)
	)
	testCases := []struct {
		name string
		data map[string]string
	}{
		{
			name: "missing ca.crt",
			data: map[string]string{},
		},
		{
			name: "empty ca.crt",
			data: map[string]string{"ca.crt": ""},
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			opts := krttest.Options(t)
			policy := &gatewayv1.BackendTLSPolicy{
				ObjectMeta: metav1.ObjectMeta{
					Name:       policyName,
					Namespace:  namespace,
					Generation: generation,
				},
				Spec: gatewayv1.BackendTLSPolicySpec{
					TargetRefs: []gatewayv1.LocalPolicyTargetReferenceWithSectionName{{
						LocalPolicyTargetReference: gatewayv1.LocalPolicyTargetReference{
							Group: "",
							Kind:  "Service",
							Name:  backendName,
						},
						SectionName: ptr.Of(gatewayv1.SectionName("https")),
					}},
					Validation: gatewayv1.BackendTLSPolicyValidation{
						CACertificateRefs: []gatewayv1.LocalObjectReference{{
							Group: "",
							Kind:  "ConfigMap",
							Name:  "backend-ca",
						}},
						Hostname: "backend.example.com",
					},
				},
			}
			ancestor := &AncestorBackend{
				Gateway: types.NamespacedName{Namespace: namespace, Name: gatewayName},
				Backend: types.NamespacedName{Namespace: namespace, Name: backendName},
				Source: TypedResource{
					Kind: gvk.HTTPRoute,
					Name: types.NamespacedName{Namespace: namespace, Name: "route"},
				},
			}

			statuses, _ := BackendTLSPolicyCollection(BackendTLSPolicyInputs{
				BackendTLSPolicies: krt.NewStaticCollection(nil, []*gatewayv1.BackendTLSPolicy{policy}, opts.WithName("Policies")...),
				ConfigMaps: krt.NewStaticCollection(nil, []*corev1.ConfigMap{{
					ObjectMeta: metav1.ObjectMeta{Name: "backend-ca", Namespace: namespace},
					Data:       tc.data,
				}}, opts.WithName("ConfigMaps")...),
				Secrets: krt.NewStaticCollection[*corev1.Secret](nil, nil, opts.WithName("Secrets")...),
				Services: krt.NewStaticCollection(nil, []*corev1.Service{{
					ObjectMeta: metav1.ObjectMeta{Name: backendName, Namespace: namespace},
					Spec: corev1.ServiceSpec{Ports: []corev1.ServicePort{{
						Name: "https",
						Port: 443,
					}}},
				}}, opts.WithName("Services")...),
				Gateways:         krt.NewStaticCollection[*gatewayv1.Gateway](nil, nil, opts.WithName("Gateways")...),
				AncestorBackends: krt.NewStaticCollection(nil, []*AncestorBackend{ancestor}, opts.WithName("Ancestors")...),
				ControllerName:   "istio.io/agentgateway-controller",
				DomainSuffix:     "cluster.local",
			}, opts)
			statuses.WaitUntilSynced(test.NewStop(t))

			got := statuses.GetKey(namespace + "/" + policyName)
			if got == nil {
				t.Fatal("expected BackendTLSPolicy status")
			}
			if len(got.Status.Ancestors) != 1 {
				t.Fatalf("got %d ancestors, want 1", len(got.Status.Ancestors))
			}
			conditions := got.Status.Ancestors[0].Conditions
			assertPolicyCondition(t, conditions, string(gatewayv1.PolicyConditionAccepted), metav1.ConditionFalse,
				string(gatewayv1.BackendTLSPolicyReasonNoValidCACertificate), generation)
			assertPolicyCondition(t, conditions, string(gatewayv1.BackendTLSPolicyConditionResolvedRefs), metav1.ConditionFalse,
				string(gatewayv1.BackendTLSPolicyReasonInvalidCACertificateRef), generation)
		})
	}
}

func assertPolicyCondition(
	t *testing.T,
	conditions []metav1.Condition,
	conditionType string,
	status metav1.ConditionStatus,
	reason string,
	generation int64,
) {
	t.Helper()
	for _, condition := range conditions {
		if condition.Type != conditionType {
			continue
		}
		if condition.Status != status || condition.Reason != reason || condition.ObservedGeneration != generation {
			t.Fatalf("condition %q = %#v, want status=%q reason=%q observedGeneration=%d",
				conditionType, condition, status, reason, generation)
		}
		return
	}
	t.Fatalf("condition %q not found in %#v", conditionType, conditions)
}
