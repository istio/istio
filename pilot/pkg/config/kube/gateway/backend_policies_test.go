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

package gateway

import (
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	k8s "sigs.k8s.io/gateway-api/apis/v1"

	"istio.io/istio/pkg/config/schema/gvk"
	"istio.io/istio/pkg/ptr"
	"istio.io/istio/pkg/test/util/assert"
)

// TestBackendTLSPolicyIgnoredAnnotation verifies that a BackendTLSPolicy carrying
// the istio.io/ignore-policy-attachment="true" annotation is excluded from the
// BackendTLSPolicy krt collection. As a result it must:
//   - produce no DestinationRule, and
//   - not participate in conflict detection against sibling policies targeting
//     the same backend.
//
// See backend_policies.go (BackendTLSPolicyCollection) for the production filter.
func TestBackendTLSPolicyIgnoredAnnotation(t *testing.T) {
	const (
		ns      = "default"
		svcName = "echo"
	)

	echoSvc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: svcName, Namespace: ns},
		Spec: corev1.ServiceSpec{
			Ports: []corev1.ServicePort{{Name: "https", Port: 443, Protocol: corev1.ProtocolTCP}},
		},
	}

	// Use distinct timestamps so priority ordering is deterministic.
	older := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	newer := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)

	makeBTLS := func(name string, createdAt time.Time, annotations map[string]string) *k8s.BackendTLSPolicy {
		return &k8s.BackendTLSPolicy{
			ObjectMeta: metav1.ObjectMeta{
				Name:              name,
				Namespace:         ns,
				Annotations:       annotations,
				CreationTimestamp: metav1.NewTime(createdAt),
			},
			Spec: k8s.BackendTLSPolicySpec{
				TargetRefs: []k8s.LocalPolicyTargetReferenceWithSectionName{{
					LocalPolicyTargetReference: k8s.LocalPolicyTargetReference{
						Group: "",
						Kind:  "Service",
						Name:  k8s.ObjectName(svcName),
					},
				}},
				Validation: k8s.BackendTLSPolicyValidation{
					WellKnownCACertificates: ptr.Of(k8s.WellKnownCACertificatesSystem),
					Hostname:                k8s.PreciseHostname("echo.example.com"),
				},
			},
		}
	}

	cases := []struct {
		name string
		objs []runtime.Object
		// expectedDRs is the number of DestinationRules expected in the default
		// namespace after the controller settles. BackendTLSPolicies are the only
		// source of DestinationRules in these test cases.
		expectedDRs int
	}{
		{
			name:        "policy without annotation translates to a DestinationRule",
			objs:        []runtime.Object{echoSvc, makeBTLS("active", older, nil)},
			expectedDRs: 1,
		},
		{
			name:        "policy with ignore annotation set to \"true\" is excluded",
			objs:        []runtime.Object{echoSvc, makeBTLS("ignored", older, map[string]string{IgnorePolicyAttachment: "true"})},
			expectedDRs: 0,
		},
		{
			// The ignored policy is older and would normally win conflict
			// resolution, pushing the sibling into Conflicted. Once excluded,
			// the sibling must translate to a DestinationRule unimpeded.
			name: "ignored policy does not conflict with sibling targeting the same Service",
			objs: []runtime.Object{
				echoSvc,
				makeBTLS("ignored", older, map[string]string{IgnorePolicyAttachment: "true"}),
				makeBTLS("active", newer, nil),
			},
			expectedDRs: 1,
		},
		{
			name:        "annotation value other than \"true\" does not exclude the policy",
			objs:        []runtime.Object{echoSvc, makeBTLS("active", older, map[string]string{IgnorePolicyAttachment: "false"})},
			expectedDRs: 1,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			controller := setupController(t, tc.objs...)
			assert.EventuallyEqual(t, func() int {
				return len(controller.List(gvk.DestinationRule, ns))
			}, tc.expectedDRs)
		})
	}
}
