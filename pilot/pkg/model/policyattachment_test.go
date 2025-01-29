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

	"k8s.io/apimachinery/pkg/types"

	"istio.io/api/label"
	"istio.io/api/type/v1beta1"
	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/serviceregistry/provider"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/config/labels"
	"istio.io/istio/pkg/config/schema/gvk"
	"istio.io/istio/pkg/test"
)

func TestPolicyMatcher(t *testing.T) {
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
	istioWaypointClassTargetRef := &v1beta1.PolicyTargetReference{
		Group: gvk.GatewayClass.Group,
		Kind:  gvk.GatewayClass.Kind,
		Name:  constants.WaypointGatewayClassName,
	}
	serviceTargetRef := &v1beta1.PolicyTargetReference{
		Group: gvk.Service.Group,
		Kind:  gvk.Service.Kind,
		Name:  "sample-svc",
	}
	serviceEntryTargetRef := &v1beta1.PolicyTargetReference{
		Group: gvk.ServiceEntry.Group,
		Kind:  gvk.ServiceEntry.Kind,
		Name:  "sample-svc-entry",
	}
	sampleSelector := &v1beta1.WorkloadSelector{
		MatchLabels: labels.Instance{
			"app": "my-app",
		},
	}
	sampleGatewaySelector := &v1beta1.WorkloadSelector{
		MatchLabels: labels.Instance{
			label.IoK8sNetworkingGatewayGatewayName.Name: "sample-gateway",
		},
	}
	sampleWaypointSelector := &v1beta1.WorkloadSelector{
		MatchLabels: labels.Instance{
			label.IoK8sNetworkingGatewayGatewayName.Name: "sample-waypoint",
		},
	}
	regularApp := WorkloadPolicyMatcher{
		WorkloadNamespace: "default",
		WorkloadLabels: labels.Instance{
			"app": "my-app",
		},
		IsWaypoint: false,
	}
	sampleGateway := WorkloadPolicyMatcher{
		WorkloadNamespace: "default",
		WorkloadLabels: labels.Instance{
			label.IoK8sNetworkingGatewayGatewayName.Name: "sample-gateway",
		},
		IsWaypoint: false,
	}
	sampleWaypoint := WorkloadPolicyMatcher{
		WorkloadNamespace: "default",
		WorkloadLabels: labels.Instance{
			label.IoK8sNetworkingGatewayGatewayName.Name: "sample-waypoint",
		},
		IsWaypoint:    true,
		RootNamespace: "istio-system",
	}
	serviceTarget := WorkloadPolicyMatcher{
		WorkloadNamespace: "default",
		WorkloadLabels: labels.Instance{
			"app": "my-app",
			label.IoK8sNetworkingGatewayGatewayName.Name: "sample-waypoint",
		},
		IsWaypoint: true,
		Services:   []ServiceInfoForPolicyMatcher{{Name: "sample-svc", Namespace: "default", Registry: provider.Kubernetes}},
	}
	serviceEntryTarget := WorkloadPolicyMatcher{
		WorkloadNamespace: "default",
		WorkloadLabels: labels.Instance{
			"app": "my-app",
			label.IoK8sNetworkingGatewayGatewayName.Name: "sample-waypoint",
		},
		IsWaypoint: true,
		Services:   []ServiceInfoForPolicyMatcher{{Name: "sample-svc-entry", Namespace: "default", Registry: provider.External}},
	}
	tests := []struct {
		name                   string
		selection              WorkloadPolicyMatcher
		policy                 TargetablePolicy
		enableSelectorPolicies bool
		policyNamespacedName   *types.NamespacedName

		expected bool
	}{
		{
			name:      "non-gateway API workload and a targetRef",
			selection: regularApp,
			policy: &mockPolicyTargetGetter{
				targetRef: sampleTargetRef,
			},
			expected:               false,
			enableSelectorPolicies: true,
		},
		{
			name:      "non-gateway API workload and a selector",
			selection: regularApp,
			policy: &mockPolicyTargetGetter{
				selector: sampleSelector,
			},
			expected:               true,
			enableSelectorPolicies: true,
		},
		{
			name:      "non-gateway API workload and both a targetRef and a selector",
			selection: regularApp,
			policy: &mockPolicyTargetGetter{
				selector:  sampleSelector,
				targetRef: sampleTargetRef,
			},
			expected:               false,
			enableSelectorPolicies: true,
		},
		{
			name:                   "non-gateway API workload and no targetRef or selector",
			policy:                 &mockPolicyTargetGetter{},
			selection:              regularApp,
			expected:               true,
			enableSelectorPolicies: true,
		},
		{
			name: "gateway API ingress and a targetRef",
			policy: &mockPolicyTargetGetter{
				targetRef: sampleTargetRef,
			},
			selection: sampleGateway,
			expected:  true,
		},
		{
			name: "gateway API ingress and a selector",
			policy: &mockPolicyTargetGetter{
				selector: sampleGatewaySelector,
			},
			selection:              sampleGateway,
			expected:               true,
			enableSelectorPolicies: true,
		},
		{
			name: "gateway API ingress and a selector (policy attachment only)",
			policy: &mockPolicyTargetGetter{
				selector: sampleGatewaySelector,
			},
			selection:              sampleGateway,
			expected:               false,
			enableSelectorPolicies: false,
		},
		{
			name: "gateway API ingress and both a targetRef and a selector",
			policy: &mockPolicyTargetGetter{
				targetRef: sampleTargetRef,
				selector:  sampleGatewaySelector,
			},
			selection: sampleGateway,
			expected:  true,
		},
		{
			name: "gateway API ingress and non-matching targetRef",
			policy: &mockPolicyTargetGetter{
				targetRef: waypointTargetRef,
			},
			selection:              sampleGateway,
			expected:               false,
			enableSelectorPolicies: true,
		},
		{
			name:                   "gateway API ingress and no targetRef or selector",
			selection:              sampleGateway,
			policy:                 &mockPolicyTargetGetter{},
			expected:               true,
			enableSelectorPolicies: true,
		},
		{
			name: "waypoint and a targetRef",
			policy: &mockPolicyTargetGetter{
				targetRef: waypointTargetRef,
			},
			selection:              sampleWaypoint,
			expected:               true,
			enableSelectorPolicies: true,
		},
		{
			name: "waypoint and a selector",
			policy: &mockPolicyTargetGetter{
				selector: sampleWaypointSelector,
			},
			selection:              sampleWaypoint,
			expected:               false,
			enableSelectorPolicies: true,
		},
		{
			name: "waypoint and both a targetRef and a selector",
			policy: &mockPolicyTargetGetter{
				targetRef: waypointTargetRef,
				selector:  sampleWaypointSelector,
			},
			selection:              sampleWaypoint,
			expected:               true,
			enableSelectorPolicies: true,
		},
		{
			name:                   "waypoint and no targetRef or selector",
			selection:              sampleWaypoint,
			policy:                 &mockPolicyTargetGetter{},
			expected:               false,
			enableSelectorPolicies: true,
		},
		{
			name:      "waypoint and non-matching targetRef",
			selection: sampleWaypoint,
			policy: &mockPolicyTargetGetter{
				targetRef: sampleTargetRef,
			},
			expected:               false,
			enableSelectorPolicies: true,
		},
		{
			name:      "waypoint and matching targetRefs",
			selection: sampleWaypoint,
			policy: &mockPolicyTargetGetter{
				targetRefs: []*v1beta1.PolicyTargetReference{waypointTargetRef},
			},
			expected:               true,
			enableSelectorPolicies: true,
		},
		{
			name:      "waypoint and partial matching targetRefs",
			selection: sampleWaypoint,
			policy: &mockPolicyTargetGetter{
				targetRefs: []*v1beta1.PolicyTargetReference{waypointTargetRef, sampleTargetRef},
			},
			expected:               true,
			enableSelectorPolicies: true,
		},
		{
			name:      "waypoint and non matching targetRefs",
			selection: sampleWaypoint,
			policy: &mockPolicyTargetGetter{
				targetRefs: []*v1beta1.PolicyTargetReference{sampleTargetRef},
			},
			expected:               false,
			enableSelectorPolicies: true,
		},
		{
			name:      "service attached policy",
			selection: serviceTarget,
			policy: &mockPolicyTargetGetter{
				targetRefs: []*v1beta1.PolicyTargetReference{serviceTargetRef},
			},
			enableSelectorPolicies: false,
			expected:               true,
		},
		{
			name:      "service entry attached policy",
			selection: serviceEntryTarget,
			policy: &mockPolicyTargetGetter{
				targetRefs: []*v1beta1.PolicyTargetReference{serviceEntryTargetRef},
			},
			enableSelectorPolicies: false,
			expected:               true,
		},
		{
			name:      "service entry policy selecting",
			selection: serviceTarget,
			policy: &mockPolicyTargetGetter{
				targetRefs: []*v1beta1.PolicyTargetReference{{
					Group: gvk.ServiceEntry.Group,
					Kind:  gvk.ServiceEntry.Kind,
					Name:  "sample-svc",
				}},
			},
			enableSelectorPolicies: false,
			expected:               false,
		},
		{
			name:      "gateway attached policy with service",
			selection: serviceTarget,
			policy: &mockPolicyTargetGetter{
				targetRefs: []*v1beta1.PolicyTargetReference{waypointTargetRef},
			},
			enableSelectorPolicies: false,
			expected:               true,
		},
		{
			name: "gateway attached policy with multi-service",
			// selection: serviceTarget,
			selection: func() WorkloadPolicyMatcher {
				base := serviceTarget
				base.Services = append(base.Services, ServiceInfoForPolicyMatcher{Name: "sample-svc-1", Namespace: "default", Registry: provider.Kubernetes})
				return base
			}(),
			policy: &mockPolicyTargetGetter{
				targetRefs: []*v1beta1.PolicyTargetReference{waypointTargetRef},
			},
			enableSelectorPolicies: false,
			expected:               true,
		},
		{
			name: "gateway attached policy with cross-namespace service",
			selection: func() WorkloadPolicyMatcher {
				base := serviceTarget
				// Waypoint is in 'waypoint'
				base.WorkloadNamespace = "waypoint"
				// Policy and service are in default
				base.Services = []ServiceInfoForPolicyMatcher{{Name: "sample-svc", Namespace: "default", Registry: provider.Kubernetes}}
				return base
			}(),
			// Policy points to a waypoint.. but its in the wrong namespace
			policy: &mockPolicyTargetGetter{
				targetRefs: []*v1beta1.PolicyTargetReference{waypointTargetRef},
			},
			enableSelectorPolicies: false,
			expected:               false,
		},
		{
			name: "gateway class attached policy in root namespace",
			selection: func() WorkloadPolicyMatcher {
				return sampleWaypoint
			}(),
			// Policy points to a waypoint.. but its in the wrong namespace
			policy: &mockPolicyTargetGetter{
				targetRefs: []*v1beta1.PolicyTargetReference{istioWaypointClassTargetRef},
			},
			enableSelectorPolicies: false,
			policyNamespacedName: &types.NamespacedName{
				Namespace: "istio-system",
				Name:      "global default",
			},
			expected: true,
		},
		{
			name: "gateway class attached policy in non-root namespace",
			selection: func() WorkloadPolicyMatcher {
				return sampleWaypoint
			}(),
			// Policy points to a waypoint.. but its in the wrong namespace
			policy: &mockPolicyTargetGetter{
				targetRefs: []*v1beta1.PolicyTargetReference{istioWaypointClassTargetRef},
			},
			enableSelectorPolicies: false,
			expected:               false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			test.SetForTest(t, &features.EnableSelectorBasedK8sGatewayPolicy, tt.enableSelectorPolicies)
			var nsName types.NamespacedName
			if tt.policyNamespacedName != nil {
				nsName = *tt.policyNamespacedName
			} else {
				nsName = types.NamespacedName{Name: "policy1", Namespace: "default"}
			}
			matcher := tt.selection.ShouldAttachPolicy(mockKind, nsName, tt.policy)

			if matcher != tt.expected {
				t.Errorf("Expected %v, but got %v", tt.expected, matcher)
			}
		})
	}
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
