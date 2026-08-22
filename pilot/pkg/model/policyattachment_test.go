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

// TestShouldAttachPolicy_ClassicGateway verifies targetRef-based attachment to a classic
// networking.istio.io Gateway. A classic Gateway proxy is NOT a Gateway API workload (it lacks the
// gateway.networking.k8s.io/gateway-name label), so it is matched via the WorkloadPolicyMatcher.Gateways
// list rather than via a workload label.
func TestShouldAttachPolicy_ClassicGateway(t *testing.T) {
	classicGatewayTargetRef := &v1beta1.PolicyTargetReference{
		Group: gvk.Gateway.Group, // networking.istio.io
		Kind:  gvk.Gateway.Kind,  // Gateway
		Name:  "gw-a",
	}
	classicGatewayTargetRefWrongName := &v1beta1.PolicyTargetReference{
		Group: gvk.Gateway.Group,
		Kind:  gvk.Gateway.Kind,
		Name:  "gw-b",
	}
	classicGatewayTargetRefExplicitOtherNS := &v1beta1.PolicyTargetReference{
		Group:     gvk.Gateway.Group,
		Kind:      gvk.Gateway.Kind,
		Name:      "gw-a",
		Namespace: "ns2",
	}
	classicGatewayTargetRefExplicitSameNS := &v1beta1.PolicyTargetReference{
		Group:     gvk.Gateway.Group,
		Kind:      gvk.Gateway.Kind,
		Name:      "gw-a",
		Namespace: "ns1",
	}
	// A classic ingress gateway workload: no gateway-name label, so it is not a Gateway API workload.
	classicGateway := WorkloadPolicyMatcher{
		WorkloadNamespace: "ns1",
		WorkloadLabels: labels.Instance{
			"app": "istio-ingressgateway",
		},
		IsWaypoint: false,
		Gateways:   []types.NamespacedName{{Name: "gw-a", Namespace: "ns1"}},
	}
	classicGatewayNoGateways := WorkloadPolicyMatcher{
		WorkloadNamespace: "ns1",
		WorkloadLabels: labels.Instance{
			"app": "istio-ingressgateway",
		},
		IsWaypoint: false,
	}

	tests := []struct {
		name                   string
		selection              WorkloadPolicyMatcher
		policy                 TargetablePolicy
		enableSelectorPolicies bool
		policyNamespacedName   *types.NamespacedName
		expected               bool
	}{
		{
			name:      "classic gateway matching targetRef (legacy singular)",
			selection: classicGateway,
			policy: &mockPolicyTargetGetter{
				targetRef: classicGatewayTargetRef,
			},
			policyNamespacedName: &types.NamespacedName{Namespace: "ns1", Name: "policy1"},
			expected:             true,
		},
		{
			name:      "classic gateway matching targetRefs (plural)",
			selection: classicGateway,
			policy: &mockPolicyTargetGetter{
				targetRefs: []*v1beta1.PolicyTargetReference{classicGatewayTargetRef},
			},
			policyNamespacedName: &types.NamespacedName{Namespace: "ns1", Name: "policy1"},
			expected:             true,
		},
		{
			name:      "classic gateway targetRef with explicit matching namespace",
			selection: classicGateway,
			policy: &mockPolicyTargetGetter{
				targetRefs: []*v1beta1.PolicyTargetReference{classicGatewayTargetRefExplicitSameNS},
			},
			policyNamespacedName: &types.NamespacedName{Namespace: "ns1", Name: "policy1"},
			expected:             true,
		},
		{
			name:      "classic gateway targetRef wrong name",
			selection: classicGateway,
			policy: &mockPolicyTargetGetter{
				targetRefs: []*v1beta1.PolicyTargetReference{classicGatewayTargetRefWrongName},
			},
			policyNamespacedName: &types.NamespacedName{Namespace: "ns1", Name: "policy1"},
			expected:             false,
		},
		{
			name:      "classic gateway targetRef policy in different namespace",
			selection: classicGateway,
			policy: &mockPolicyTargetGetter{
				targetRefs: []*v1beta1.PolicyTargetReference{classicGatewayTargetRef},
			},
			policyNamespacedName: &types.NamespacedName{Namespace: "ns2", Name: "policy1"},
			expected:             false,
		},
		{
			name:      "classic gateway targetRef with explicit different namespace",
			selection: classicGateway,
			policy: &mockPolicyTargetGetter{
				targetRefs: []*v1beta1.PolicyTargetReference{classicGatewayTargetRefExplicitOtherNS},
			},
			policyNamespacedName: &types.NamespacedName{Namespace: "ns1", Name: "policy1"},
			expected:             false,
		},
		{
			name:      "classic gateway targetRef but empty Gateways",
			selection: classicGatewayNoGateways,
			policy: &mockPolicyTargetGetter{
				targetRefs: []*v1beta1.PolicyTargetReference{classicGatewayTargetRef},
			},
			policyNamespacedName: &types.NamespacedName{Namespace: "ns1", Name: "policy1"},
			expected:             false,
		},
		{
			name:      "classic gateway with KubernetesGateway-kind targetRef does not match classic Gateway",
			selection: classicGateway,
			policy: &mockPolicyTargetGetter{
				targetRefs: []*v1beta1.PolicyTargetReference{{
					Group: gvk.KubernetesGateway.Group,
					Kind:  gvk.KubernetesGateway.Kind,
					Name:  "gw-a",
				}},
			},
			policyNamespacedName: &types.NamespacedName{Namespace: "ns1", Name: "policy1"},
			expected:             false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			test.SetForTest(t, &features.EnableSelectorBasedK8sGatewayPolicy, tt.enableSelectorPolicies)
			nsName := types.NamespacedName{Name: "policy1", Namespace: "default"}
			if tt.policyNamespacedName != nil {
				nsName = *tt.policyNamespacedName
			}
			got := tt.selection.ShouldAttachPolicy(mockKind, nsName, tt.policy)
			if got != tt.expected {
				t.Errorf("Expected %v, but got %v", tt.expected, got)
			}
		})
	}
}

// TestShouldAttachPolicy_ClassicGatewayZeroImpact asserts the zero-impact invariant: adding the
// Gateways field must not change the behavior of no-targetRef selector policies (they still fall
// through to selector matching), and a non-Gateway-kind targetRef on a non-GatewayAPI proxy is still
// ignored (returns false), regardless of whether Gateways is populated.
func TestShouldAttachPolicy_ClassicGatewayZeroImpact(t *testing.T) {
	test.SetForTest(t, &features.EnableSelectorBasedK8sGatewayPolicy, true)
	nsName := types.NamespacedName{Name: "policy1", Namespace: "ns1"}

	baseLabels := labels.Instance{"app": "istio-ingressgateway"}
	withoutGateways := WorkloadPolicyMatcher{
		WorkloadNamespace: "ns1",
		WorkloadLabels:    baseLabels,
	}
	withGateways := WorkloadPolicyMatcher{
		WorkloadNamespace: "ns1",
		WorkloadLabels:    baseLabels,
		Gateways:          []types.NamespacedName{{Name: "gw-a", Namespace: "ns1"}},
	}

	// A selector-only policy that matches the workload labels.
	selectorPolicy := &mockPolicyTargetGetter{
		selector: &v1beta1.WorkloadSelector{MatchLabels: labels.Instance{"app": "istio-ingressgateway"}},
	}
	if got := withoutGateways.ShouldAttachPolicy(mockKind, nsName, selectorPolicy); !got {
		t.Fatalf("selector policy without Gateways: expected true, got %v", got)
	}
	if got := withGateways.ShouldAttachPolicy(mockKind, nsName, selectorPolicy); !got {
		t.Fatalf("selector policy with Gateways: expected true, got %v", got)
	}

	// A no-targetRef, no-selector policy is selected on a non-gateway workload; result must be identical.
	emptyPolicy := &mockPolicyTargetGetter{}
	if a, b := withoutGateways.ShouldAttachPolicy(mockKind, nsName, emptyPolicy),
		withGateways.ShouldAttachPolicy(mockKind, nsName, emptyPolicy); a != b {
		t.Fatalf("empty policy behavior changed by Gateways field: without=%v with=%v", a, b)
	}

	// A non-Gateway-kind targetRef (e.g. a Service) on a non-GatewayAPI proxy is still ignored today,
	// whether or not Gateways is populated.
	nonGatewayTargetRefPolicy := &mockPolicyTargetGetter{
		targetRefs: []*v1beta1.PolicyTargetReference{{
			Group: gvk.Service.Group,
			Kind:  gvk.Service.Kind,
			Name:  "gw-a",
		}},
	}
	if got := withoutGateways.ShouldAttachPolicy(mockKind, nsName, nonGatewayTargetRefPolicy); got {
		t.Fatalf("non-Gateway targetRef without Gateways: expected false, got %v", got)
	}
	if got := withGateways.ShouldAttachPolicy(mockKind, nsName, nonGatewayTargetRefPolicy); got {
		t.Fatalf("non-Gateway targetRef with Gateways: expected false, got %v", got)
	}
}

// TestShouldAttachPolicy_MixedGatewayAPIAndClassic covers a workload that is BOTH a Gateway API
// gateway (it carries the gateway.networking.k8s.io/gateway-name label, so isGatewayAPI==true) AND an
// instance of a classic networking.istio.io Gateway (p.Gateways is non-empty). A classic-Gateway
// targetRef must attach to such a mixed workload; the classic-Gateway match runs independently of
// isGatewayAPI rather than only inside the `if !isGatewayAPI` branch. The remaining cases guard the
// surrounding behavior: a Gateway-API targetRef must still attach, and a classic targetRef must NOT
// attach when the workload is not an instance of any classic Gateway (empty p.Gateways).
func TestShouldAttachPolicy_MixedGatewayAPIAndClassic(t *testing.T) {
	const k8sGatewayName = "some-k8s-gw"

	classicGatewayTargetRef := &v1beta1.PolicyTargetReference{
		Group: gvk.Gateway.Group,
		Kind:  gvk.Gateway.Kind,
		Name:  "gw-a",
	}
	k8sGatewayTargetRef := &v1beta1.PolicyTargetReference{
		Group: gvk.KubernetesGateway.Group,
		Kind:  gvk.KubernetesGateway.Kind,
		Name:  k8sGatewayName,
	}

	// A workload that is simultaneously a Gateway API gateway (label present => isGatewayAPI==true)
	// and an instance of the classic Gateway gw-a (Gateways non-empty).
	mixedWorkload := WorkloadPolicyMatcher{
		WorkloadNamespace: "ns1",
		WorkloadLabels: labels.Instance{
			label.IoK8sNetworkingGatewayGatewayName.Name: k8sGatewayName,
		},
		IsWaypoint: false,
		Gateways:   []types.NamespacedName{{Name: "gw-a", Namespace: "ns1"}},
	}
	// A pure Gateway API workload: it carries the gateway-name label but is not an instance of any
	// classic Gateway (empty Gateways).
	pureK8sGatewayWorkload := WorkloadPolicyMatcher{
		WorkloadNamespace: "ns1",
		WorkloadLabels: labels.Instance{
			label.IoK8sNetworkingGatewayGatewayName.Name: k8sGatewayName,
		},
		IsWaypoint: false,
	}

	tests := []struct {
		name      string
		selection WorkloadPolicyMatcher
		policy    TargetablePolicy
		expected  bool
	}{
		{
			// The classic-Gateway targetRef attaches even though the workload is also a Gateway API
			// gateway, because the workload is an instance of the targeted classic Gateway.
			name:      "mixed workload attaches classic Gateway targetRef",
			selection: mixedWorkload,
			policy: &mockPolicyTargetGetter{
				targetRefs: []*v1beta1.PolicyTargetReference{classicGatewayTargetRef},
			},
			expected: true,
		},
		{
			// Guard: a Gateway-API (KubernetesGateway) targetRef must still attach to the mixed workload
			// via its gateway-name label, exactly as before.
			name:      "mixed workload still attaches Gateway-API targetRef",
			selection: mixedWorkload,
			policy: &mockPolicyTargetGetter{
				targetRefs: []*v1beta1.PolicyTargetReference{k8sGatewayTargetRef},
			},
			expected: true,
		},
		{
			// Guard: a pure Gateway-API workload still attaches a matching Gateway-API targetRef.
			name:      "pure Gateway-API workload attaches Gateway-API targetRef",
			selection: pureK8sGatewayWorkload,
			policy: &mockPolicyTargetGetter{
				targetRefs: []*v1beta1.PolicyTargetReference{k8sGatewayTargetRef},
			},
			expected: true,
		},
		{
			// Guard: a classic-Gateway targetRef must NOT attach to a Gateway-API workload that is not an
			// instance of any classic Gateway (empty Gateways); the match keys off the Gateways list.
			name:      "pure Gateway-API workload does not attach classic Gateway targetRef",
			selection: pureK8sGatewayWorkload,
			policy: &mockPolicyTargetGetter{
				targetRefs: []*v1beta1.PolicyTargetReference{classicGatewayTargetRef},
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			test.SetForTest(t, &features.EnableSelectorBasedK8sGatewayPolicy, true)
			nsName := types.NamespacedName{Name: "policy1", Namespace: "ns1"}
			got := tt.selection.ShouldAttachPolicy(mockKind, nsName, tt.policy)
			if got != tt.expected {
				t.Errorf("Expected %v, but got %v", tt.expected, got)
			}
		})
	}
}

// TestWithGateways verifies the plural WithGateways helper appends every non-zero NamespacedName to
// the matcher's Gateways slice (mirroring WithServices), is nil-slice safe, and skips zero values
// (delegating to the Change-2 WithGateway, which no-ops on the zero NamespacedName).
func TestWithGateways(t *testing.T) {
	base := WorkloadPolicyMatcher{WorkloadNamespace: "ns1"}

	// nil slice: no change, no panic.
	if got := base.WithGateways(nil); len(got.Gateways) != 0 {
		t.Fatalf("WithGateways(nil): expected 0 gateways, got %v", got.Gateways)
	}

	// empty slice: no change.
	if got := base.WithGateways([]types.NamespacedName{}); len(got.Gateways) != 0 {
		t.Fatalf("WithGateways([]): expected 0 gateways, got %v", got.Gateways)
	}

	// multiple gateways are all appended, order preserved.
	gws := []types.NamespacedName{{Name: "gw-a", Namespace: "ns1"}, {Name: "gw-b", Namespace: "ns2"}}
	got := base.WithGateways(gws)
	if len(got.Gateways) != 2 {
		t.Fatalf("WithGateways: expected 2 gateways, got %v", got.Gateways)
	}
	if got.Gateways[0] != gws[0] || got.Gateways[1] != gws[1] {
		t.Fatalf("WithGateways: expected %v, got %v", gws, got.Gateways)
	}

	// zero NamespacedName is skipped (WithGateway no-ops on the zero value).
	gotZero := base.WithGateways([]types.NamespacedName{{}, {Name: "gw-a", Namespace: "ns1"}})
	if len(gotZero.Gateways) != 1 || gotZero.Gateways[0] != (types.NamespacedName{Name: "gw-a", Namespace: "ns1"}) {
		t.Fatalf("WithGateways: expected only non-zero gw-a, got %v", gotZero.Gateways)
	}

	// receiver is not mutated (value semantics, like WithService/WithServices).
	if len(base.Gateways) != 0 {
		t.Fatalf("WithGateways mutated receiver: %v", base.Gateways)
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
