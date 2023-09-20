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
	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/config/labels"
	"istio.io/istio/pkg/config/schema/gvk"
	"istio.io/istio/pkg/test"
)

func TestGetPolicyMatcher(t *testing.T) {
	sampleTargetRef := &v1beta1.PolicyTargetReference{
		Group: gvk.KubernetesGateway.Group,
		Kind:  gvk.KubernetesGateway.Kind,
		Name:  "sample-gateway",
	}
	waypointTargetRef := &v1beta1.PolicyTargetReference{
		Group: gvk.KubernetesGateway.Group,
		Kind:  gvk.KubernetesGateway.Kind,
		Name:  "sample-waypoint",
	}
	sampleSelector := &v1beta1.WorkloadSelector{
		MatchLabels: labels.Instance{
			"app": "my-app",
		},
	}
	sampleGatewaySelector := &v1beta1.WorkloadSelector{
		MatchLabels: labels.Instance{
			constants.GatewayNameLabel: "sample-gateway",
		},
	}
	sampleWaypointSelector := &v1beta1.WorkloadSelector{
		MatchLabels: labels.Instance{
			constants.GatewayNameLabel: "sample-waypoint",
		},
	}
	regularApp := WorkloadSelectionOpts{
		RootNamespace: "root",
		Namespace:     "default",
		WorkloadLabels: labels.Instance{
			"app": "my-app",
		},
		IsWaypoint: false,
	}
	sampleGateway := WorkloadSelectionOpts{
		RootNamespace: "root",
		Namespace:     "default",
		WorkloadLabels: labels.Instance{
			constants.GatewayNameLabel: "sample-gateway",
		},
		IsWaypoint: false,
	}
	sampleWaypoint := WorkloadSelectionOpts{
		RootNamespace: "root",
		Namespace:     "default",
		WorkloadLabels: labels.Instance{
			constants.GatewayNameLabel: "sample-waypoint",
		},
		IsWaypoint: true,
	}
	tests := []struct {
		name                   string
		opts                   WorkloadSelectionOpts
		policy                 policyTargetGetter
		expected               policyMatch
		enableSelectorPolicies bool
	}{
		{
			name: "non-gateway API workload and a targetRef",
			opts: regularApp,
			policy: &mockPolicyTargetGetter{
				targetRef: sampleTargetRef,
			},
			expected:               policyMatchIgnore,
			enableSelectorPolicies: true,
		},
		{
			name: "non-gateway API workload and a selector",
			opts: regularApp,
			policy: &mockPolicyTargetGetter{
				selector: sampleSelector,
			},
			expected:               policyMatchSelector,
			enableSelectorPolicies: true,
		},
		{
			name: "non-gateway API workload and both a targetRef and a selector",
			opts: regularApp,
			policy: &mockPolicyTargetGetter{
				selector:  sampleSelector,
				targetRef: sampleTargetRef,
			},
			expected:               policyMatchIgnore,
			enableSelectorPolicies: true,
		},
		{
			name:                   "non-gateway API workload and no targetRef or selector",
			policy:                 &mockPolicyTargetGetter{},
			opts:                   regularApp,
			expected:               policyMatchSelector,
			enableSelectorPolicies: true,
		},
		{
			name: "gateway API ingress and a targetRef",
			policy: &mockPolicyTargetGetter{
				targetRef: sampleTargetRef,
			},
			opts:     sampleGateway,
			expected: policyMatchDirect,
		},
		{
			name: "gateway API ingress and a selector",
			policy: &mockPolicyTargetGetter{
				selector: sampleGatewaySelector,
			},
			opts:                   sampleGateway,
			expected:               policyMatchSelector,
			enableSelectorPolicies: true,
		},
		{
			name: "gateway API ingress and a selector (policy attachment only)",
			policy: &mockPolicyTargetGetter{
				selector: sampleGatewaySelector,
			},
			opts:                   sampleGateway,
			expected:               policyMatchIgnore,
			enableSelectorPolicies: false,
		},
		{
			name: "gateway API ingress and both a targetRef and a selector",
			policy: &mockPolicyTargetGetter{
				targetRef: sampleTargetRef,
				selector:  sampleGatewaySelector,
			},
			opts:     sampleGateway,
			expected: policyMatchDirect,
		},
		{
			name: "gateway API ingress and non-matching targetRef",
			policy: &mockPolicyTargetGetter{
				targetRef: waypointTargetRef,
			},
			opts:                   sampleGateway,
			expected:               policyMatchIgnore,
			enableSelectorPolicies: true,
		},
		{
			name:                   "gateway API ingress and no targetRef or selector",
			opts:                   sampleGateway,
			policy:                 &mockPolicyTargetGetter{},
			expected:               policyMatchSelector,
			enableSelectorPolicies: true,
		},
		{
			name: "waypoint and a targetRef",
			policy: &mockPolicyTargetGetter{
				targetRef: waypointTargetRef,
			},
			opts:                   sampleWaypoint,
			expected:               policyMatchDirect,
			enableSelectorPolicies: true,
		},
		{
			name: "waypoint and a selector",
			policy: &mockPolicyTargetGetter{
				selector: sampleWaypointSelector,
			},
			opts:                   sampleWaypoint,
			expected:               policyMatchIgnore,
			enableSelectorPolicies: true,
		},
		{
			name: "waypoint and both a targetRef and a selector",
			policy: &mockPolicyTargetGetter{
				targetRef: waypointTargetRef,
				selector:  sampleWaypointSelector,
			},
			opts:                   sampleWaypoint,
			expected:               policyMatchDirect,
			enableSelectorPolicies: true,
		},
		{
			name:                   "waypoint and no targetRef or selector",
			opts:                   sampleWaypoint,
			policy:                 &mockPolicyTargetGetter{},
			expected:               policyMatchSelector,
			enableSelectorPolicies: true,
		},
		{
			name: "waypoint and non-matching targetRef",
			opts: sampleWaypoint,
			policy: &mockPolicyTargetGetter{
				targetRef: sampleTargetRef,
			},
			expected:               policyMatchIgnore,
			enableSelectorPolicies: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			test.SetForTest(t, &features.EnableSelectorBasedK8sGatewayPolicy, tt.enableSelectorPolicies)
			matcher := getPolicyMatcher(mockKind, "policy1", tt.opts, tt.policy)

			if matcher != tt.expected {
				t.Errorf("Expected %v, but got %v", tt.expected, matcher)
			}
		})
	}
}

type mockPolicyTargetGetter struct {
	targetRef *v1beta1.PolicyTargetReference
	selector  *v1beta1.WorkloadSelector
}

func (m *mockPolicyTargetGetter) GetTargetRef() *v1beta1.PolicyTargetReference {
	return m.targetRef
}

func (m *mockPolicyTargetGetter) GetSelector() *v1beta1.WorkloadSelector {
	return m.selector
}

var mockKind = config.GroupVersionKind{
	Group:   "mock.istio.io",
	Version: "v1",
	Kind:    "MockKind",
}
