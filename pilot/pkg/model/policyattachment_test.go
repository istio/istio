// Copyright Istio Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package model

import (
	"testing"

	"istio.io/api/type/v1beta1"
	"istio.io/istio/pkg/config"
)

func TestGetPolicyMatcher(t *testing.T) {
	// sampleTargetRef := &v1beta1.PolicyTargetReference{
	// 	Group: gvk.KubernetesGateway.Group,
	// 	Kind:  gvk.KubernetesGateway.Kind,
	// 	Name:  "sample-gateway",
	// }
	// waypointTargetRef := &v1beta1.PolicyTargetReference{
	// 	Group: gvk.KubernetesGateway.Group,
	// 	Kind:  gvk.KubernetesGateway.Kind,
	// 	Name:  "sample-waypoint",
	// }
	// sampleSelector := &v1beta1.WorkloadSelector{
	// 	MatchLabels: labels.Instance{
	// 		"app": "my-app",
	// 	},
	// }
	// sampleGatewaySelector := &v1beta1.WorkloadSelector{
	// 	MatchLabels: labels.Instance{
	// 		constants.GatewayNameLabel: "sample-gateway",
	// 	},
	// }
	// sampleWaypointSelector := &v1beta1.WorkloadSelector{
	// 	MatchLabels: labels.Instance{
	// 		constants.GatewayNameLabel: "sample-waypoint",
	// 	},
	// }
	// regularApp := WorkloadPolicyMatcher{
	// 	Namespace:     "default",
	// 	WorkloadLabels: labels.Instance{
	// 		"app": "my-app",
	// 	},
	// 	IsWaypoint: false,
	// }
	// sampleGateway := WorkloadPolicyMatcher{
	// 	Namespace:     "default",
	// 	WorkloadLabels: labels.Instance{
	// 		constants.GatewayNameLabel: "sample-gateway",
	// 	},
	// 	IsWaypoint: false,
	// }
	// sampleWaypoint := WorkloadPolicyMatcher{
	// 	Namespace:     "default",
	// 	WorkloadLabels: labels.Instance{
	// 		constants.GatewayNameLabel: "sample-waypoint",
	// 	},
	// 	IsWaypoint: true,
	// }
	// tests := []struct {
	// 	name                   string
	// 	selection              WorkloadPolicyMatcher
	// 	policy                 TargetablePolicy
	// 	expected               PolicyMatch
	// 	enableSelectorPolicies bool
	// }{
	// 	{
	// 		name:      "non-gateway API workload and a targetRef",
	// 		selection: regularApp,
	// 		policy: &mockPolicyTargetGetter{
	// 			targetRef: sampleTargetRef,
	// 		},
	// 		expected:               policyMatchIgnore,
	// 		enableSelectorPolicies: true,
	// 	},
	// 	{
	// 		name:      "non-gateway API workload and a selector",
	// 		selection: regularApp,
	// 		policy: &mockPolicyTargetGetter{
	// 			selector: sampleSelector,
	// 		},
	// 		expected:               policyMatchSelector,
	// 		enableSelectorPolicies: true,
	// 	},
	// 	{
	// 		name:      "non-gateway API workload and both a targetRef and a selector",
	// 		selection: regularApp,
	// 		policy: &mockPolicyTargetGetter{
	// 			selector:  sampleSelector,
	// 			targetRef: sampleTargetRef,
	// 		},
	// 		expected:               policyMatchIgnore,
	// 		enableSelectorPolicies: true,
	// 	},
	// 	{
	// 		name:                   "non-gateway API workload and no targetRef or selector",
	// 		policy:                 &mockPolicyTargetGetter{},
	// 		selection:              regularApp,
	// 		expected:               policyMatchSelector,
	// 		enableSelectorPolicies: true,
	// 	},
	// 	{
	// 		name: "gateway API ingress and a targetRef",
	// 		policy: &mockPolicyTargetGetter{
	// 			targetRef: sampleTargetRef,
	// 		},
	// 		selection: sampleGateway,
	// 		expected:  policyMatchDirect,
	// 	},
	// 	{
	// 		name: "gateway API ingress and a selector",
	// 		policy: &mockPolicyTargetGetter{
	// 			selector: sampleGatewaySelector,
	// 		},
	// 		selection:              sampleGateway,
	// 		expected:               policyMatchSelector,
	// 		enableSelectorPolicies: true,
	// 	},
	// 	{
	// 		name: "gateway API ingress and a selector (policy attachment only)",
	// 		policy: &mockPolicyTargetGetter{
	// 			selector: sampleGatewaySelector,
	// 		},
	// 		selection:              sampleGateway,
	// 		expected:               policyMatchIgnore,
	// 		enableSelectorPolicies: false,
	// 	},
	// 	{
	// 		name: "gateway API ingress and both a targetRef and a selector",
	// 		policy: &mockPolicyTargetGetter{
	// 			targetRef: sampleTargetRef,
	// 			selector:  sampleGatewaySelector,
	// 		},
	// 		selection: sampleGateway,
	// 		expected:  policyMatchDirect,
	// 	},
	// 	{
	// 		name: "gateway API ingress and non-matching targetRef",
	// 		policy: &mockPolicyTargetGetter{
	// 			targetRef: waypointTargetRef,
	// 		},
	// 		selection:              sampleGateway,
	// 		expected:               policyMatchIgnore,
	// 		enableSelectorPolicies: true,
	// 	},
	// 	{
	// 		name:                   "gateway API ingress and no targetRef or selector",
	// 		selection:              sampleGateway,
	// 		policy:                 &mockPolicyTargetGetter{},
	// 		expected:               policyMatchSelector,
	// 		enableSelectorPolicies: true,
	// 	},
	// 	{
	// 		name: "waypoint and a targetRef",
	// 		policy: &mockPolicyTargetGetter{
	// 			targetRef: waypointTargetRef,
	// 		},
	// 		selection:              sampleWaypoint,
	// 		expected:               policyMatchDirect,
	// 		enableSelectorPolicies: true,
	// 	},
	// 	{
	// 		name: "waypoint and a selector",
	// 		policy: &mockPolicyTargetGetter{
	// 			selector: sampleWaypointSelector,
	// 		},
	// 		selection:              sampleWaypoint,
	// 		expected:               policyMatchIgnore,
	// 		enableSelectorPolicies: true,
	// 	},
	// 	{
	// 		name: "waypoint and both a targetRef and a selector",
	// 		policy: &mockPolicyTargetGetter{
	// 			targetRef: waypointTargetRef,
	// 			selector:  sampleWaypointSelector,
	// 		},
	// 		selection:              sampleWaypoint,
	// 		expected:               policyMatchDirect,
	// 		enableSelectorPolicies: true,
	// 	},
	// 	{
	// 		name:                   "waypoint and no targetRef or selector",
	// 		selection:              sampleWaypoint,
	// 		policy:                 &mockPolicyTargetGetter{},
	// 		expected:               policyMatchIgnore,
	// 		enableSelectorPolicies: true,
	// 	},
	// 	{
	// 		name:      "waypoint and non-matching targetRef",
	// 		selection: sampleWaypoint,
	// 		policy: &mockPolicyTargetGetter{
	// 			targetRef: sampleTargetRef,
	// 		},
	// 		expected:               policyMatchIgnore,
	// 		enableSelectorPolicies: true,
	// 	},
	// 	{
	// 		name: "waypoint and matching targetRefs",
	// 		opts: sampleWaypoint,
	// 		policy: &mockPolicyTargetGetter{
	// 			targetRefs: []*v1beta1.PolicyTargetReference{waypointTargetRef},
	// 		},
	// 		expected:               policyMatchDirect,
	// 		enableSelectorPolicies: true,
	// 	},
	// 	{
	// 		name: "waypoint and partial matching targetRefs",
	// 		opts: sampleWaypoint,
	// 		policy: &mockPolicyTargetGetter{
	// 			targetRefs: []*v1beta1.PolicyTargetReference{waypointTargetRef, sampleTargetRef},
	// 		},
	// 		expected:               policyMatchDirect,
	// 		enableSelectorPolicies: true,
	// 	},
	// 	{
	// 		name: "waypoint and non matching targetRefs",
	// 		opts: sampleWaypoint,
	// 		policy: &mockPolicyTargetGetter{
	// 			targetRefs: []*v1beta1.PolicyTargetReference{sampleTargetRef},
	// 		},
	// 		expected:               policyMatchIgnore,
	// 		enableSelectorPolicies: true,
	// 	},
	// }

	// for _, tt := range tests {
	// 	t.Run(tt.name, func(t *testing.T) {
	// 		test.SetForTest(t, &features.EnableSelectorBasedK8sGatewayPolicy, tt.enableSelectorPolicies)
	// 		nsName := types.NamespacedName{Name: "policy1", Namespace: "default"}
	// 		matcher := tt.selection.GetPolicyMatcher(mockKind, nsName, tt.policy)

	// 		if matcher != tt.expected {
	// 			t.Errorf("Expected %v, but got %v", tt.expected, matcher)
	// 		}
	// 	})
	// }
}

type mockPolicyTargetGetter struct {
	targetRef  *v1beta1.PolicyTargetReference
	targetRefs []*v1beta1.PolicyTargetReference
	selector   *v1beta1.WorkloadSelector
}

func (m *mockPolicyTargetGetter) GetTargetRef() *v1beta1.PolicyTargetReference {
	return m.targetRef
}

func (m *mockPolicyTargetGetter) GetTargetRefs() []*v1beta1.PolicyTargetReference {
	return m.targetRefs
}

func (m *mockPolicyTargetGetter) GetSelector() *v1beta1.WorkloadSelector {
	return m.selector
}

var mockKind = config.GroupVersionKind{
	Group:   "mock.istio.io",
	Version: "v1",
	Kind:    "MockKind",
}
