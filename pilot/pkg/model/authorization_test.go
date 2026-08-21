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

package model

import (
	"fmt"
	"reflect"
	"testing"

	"k8s.io/apimachinery/pkg/types"

	"istio.io/api/label"
	meshconfig "istio.io/api/mesh/v1alpha1"
	authpb "istio.io/api/security/v1beta1"
	selectorpb "istio.io/api/type/v1beta1"
	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/serviceregistry/provider"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/labels"
	"istio.io/istio/pkg/config/mesh/meshwatcher"
	"istio.io/istio/pkg/config/schema/collection"
	"istio.io/istio/pkg/config/schema/gvk"
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/util/protomarshal"
)

func TestAuthorizationPolicies_ListAuthorizationPolicies(t *testing.T) {
	policy := &authpb.AuthorizationPolicy{
		Rules: []*authpb.Rule{
			{
				From: []*authpb.Rule_From{
					{
						Source: &authpb.Source{
							Principals: []string{"sleep"},
						},
					},
				},
				To: []*authpb.Rule_To{
					{
						Operation: &authpb.Operation{
							Methods: []string{"GET"},
						},
					},
				},
			},
		},
	}
	policyWithSelector := protomarshal.Clone(policy)
	policyWithSelector.Selector = &selectorpb.WorkloadSelector{
		MatchLabels: labels.Instance{
			"app":     "httpbin",
			"version": "v1",
		},
	}
	policyWithTargetRef := protomarshal.Clone(policy)
	policyWithTargetRef.TargetRef = &selectorpb.PolicyTargetReference{
		Group:     gvk.KubernetesGateway.Group,
		Kind:      gvk.KubernetesGateway.Kind,
		Name:      "my-gateway",
		Namespace: "bar",
	}

	policyWithServiceRef := protomarshal.Clone(policy)
	policyWithServiceRef.TargetRef = &selectorpb.PolicyTargetReference{
		Group:     gvk.Service.Group,
		Kind:      gvk.Service.Kind,
		Name:      "foo-svc",
		Namespace: "foo",
	}
	policyWithGWClassRef := protomarshal.Clone(policy)
	policyWithGWClassRef.TargetRef = &selectorpb.PolicyTargetReference{
		Group: gvk.GatewayClass.Group,
		Kind:  gvk.GatewayClass.Kind,
		Name:  "istio-waypoint",
	}

	denyPolicy := protomarshal.Clone(policy)
	denyPolicy.Action = authpb.AuthorizationPolicy_DENY

	auditPolicy := protomarshal.Clone(policy)
	auditPolicy.Action = authpb.AuthorizationPolicy_AUDIT

	customPolicy := protomarshal.Clone(policy)
	customPolicy.Action = authpb.AuthorizationPolicy_CUSTOM

	cases := []struct {
		name          string
		selectionOpts WorkloadPolicyMatcher
		configs       []config.Config
		wantDeny      []AuthorizationPolicy
		wantAllow     []AuthorizationPolicy
		wantAudit     []AuthorizationPolicy
		wantCustom    []AuthorizationPolicy
	}{
		{
			name: "no policies",
			selectionOpts: WorkloadPolicyMatcher{
				WorkloadNamespace: "foo",
			},
			wantAllow: nil,
		},
		{
			name: "no policies in namespace foo",
			selectionOpts: WorkloadPolicyMatcher{
				WorkloadNamespace: "foo",
			},
			configs: []config.Config{
				newConfig("authz-1", "bar", policy),
				newConfig("authz-2", "bar", policy),
			},
			wantAllow: nil,
		},
		{
			name: "no policies with a targetRef in namespace foo",
			selectionOpts: WorkloadPolicyMatcher{
				WorkloadNamespace: "foo",
				WorkloadLabels: labels.Instance{
					label.IoK8sNetworkingGatewayGatewayName.Name: "my-gateway",
				},
			},
			configs: []config.Config{
				newConfig("authz-1", "bar", policyWithTargetRef),
			},
			wantAllow: nil,
		},
		{
			name: "one allow policy",
			selectionOpts: WorkloadPolicyMatcher{
				WorkloadNamespace: "bar",
			},
			configs: []config.Config{
				newConfig("authz-1", "bar", policy),
			},
			wantAllow: []AuthorizationPolicy{
				{
					Name:      "authz-1",
					Namespace: "bar",
					Spec:      policy,
				},
			},
		},
		{
			name: "one deny policy",
			selectionOpts: WorkloadPolicyMatcher{
				WorkloadNamespace: "bar",
			},
			configs: []config.Config{
				newConfig("authz-1", "bar", denyPolicy),
			},
			wantDeny: []AuthorizationPolicy{
				{
					Name:      "authz-1",
					Namespace: "bar",
					Spec:      denyPolicy,
				},
			},
		},
		{
			name: "one audit policy",
			selectionOpts: WorkloadPolicyMatcher{
				WorkloadNamespace: "bar",
			},
			configs: []config.Config{
				newConfig("authz-1", "bar", auditPolicy),
			},
			wantAudit: []AuthorizationPolicy{
				{
					Name:      "authz-1",
					Namespace: "bar",
					Spec:      auditPolicy,
				},
			},
		},
		{
			name: "one custom policy",
			selectionOpts: WorkloadPolicyMatcher{
				WorkloadNamespace: "bar",
			},
			configs: []config.Config{
				newConfig("authz-1", "bar", customPolicy),
			},
			wantCustom: []AuthorizationPolicy{
				{
					Name:      "authz-1",
					Namespace: "bar",
					Spec:      customPolicy,
				},
			},
		},
		{
			name: "two policies",
			selectionOpts: WorkloadPolicyMatcher{
				WorkloadNamespace: "bar",
			},
			configs: []config.Config{
				newConfig("authz-1", "foo", policy),
				newConfig("authz-1", "bar", policy),
				newConfig("authz-2", "bar", policy),
			},
			wantAllow: []AuthorizationPolicy{
				{
					Name:      "authz-1",
					Namespace: "bar",
					Spec:      policy,
				},
				{
					Name:      "authz-2",
					Namespace: "bar",
					Spec:      policy,
				},
			},
		},
		{
			name: "mixing allow, deny, and audit policies",
			selectionOpts: WorkloadPolicyMatcher{
				WorkloadNamespace: "bar",
			},
			configs: []config.Config{
				newConfig("authz-1", "bar", policy),
				newConfig("authz-2", "bar", denyPolicy),
				newConfig("authz-3", "bar", auditPolicy),
				newConfig("authz-4", "bar", auditPolicy),
			},
			wantDeny: []AuthorizationPolicy{
				{
					Name:      "authz-2",
					Namespace: "bar",
					Spec:      denyPolicy,
				},
			},
			wantAllow: []AuthorizationPolicy{
				{
					Name:      "authz-1",
					Namespace: "bar",
					Spec:      policy,
				},
			},
			wantAudit: []AuthorizationPolicy{
				{
					Name:      "authz-3",
					Namespace: "bar",
					Spec:      auditPolicy,
				},
				{
					Name:      "authz-4",
					Namespace: "bar",
					Spec:      auditPolicy,
				},
			},
		},
		{
			name: "targetRef is an exact match",
			selectionOpts: WorkloadPolicyMatcher{
				WorkloadNamespace: "bar",
				WorkloadLabels: labels.Instance{
					label.IoK8sNetworkingGatewayGatewayName.Name: "my-gateway",
				},
			},
			configs: []config.Config{
				newConfig("authz-1", "bar", policyWithTargetRef),
			},
			wantAllow: []AuthorizationPolicy{
				{
					Name:      "authz-1",
					Namespace: "bar",
					Spec:      policyWithTargetRef,
				},
			},
		},
		{
			name: "selector exact match",
			selectionOpts: WorkloadPolicyMatcher{
				WorkloadNamespace: "bar",
				WorkloadLabels: labels.Instance{
					"app":     "httpbin",
					"version": "v1",
				},
			},
			configs: []config.Config{
				newConfig("authz-1", "bar", policyWithSelector),
			},
			wantAllow: []AuthorizationPolicy{
				{
					Name:      "authz-1",
					Namespace: "bar",
					Spec:      policyWithSelector,
				},
			},
		},
		{
			name: "selector subset match",
			selectionOpts: WorkloadPolicyMatcher{
				WorkloadNamespace: "bar",
				WorkloadLabels: labels.Instance{
					"app":     "httpbin",
					"version": "v1",
					"env":     "dev",
				},
			},
			configs: []config.Config{
				newConfig("authz-1", "bar", policyWithSelector),
			},
			wantAllow: []AuthorizationPolicy{
				{
					Name:      "authz-1",
					Namespace: "bar",
					Spec:      policyWithSelector,
				},
			},
		},
		{
			name: "targetRef is not a match",
			selectionOpts: WorkloadPolicyMatcher{
				WorkloadNamespace: "bar",
				WorkloadLabels: labels.Instance{
					label.IoK8sNetworkingGatewayGatewayName.Name: "my-gateway2",
				},
			},
			configs: []config.Config{
				newConfig("authz-1", "bar", policyWithTargetRef),
			},
			wantAllow: nil,
		},
		{
			name: "selector not match",
			selectionOpts: WorkloadPolicyMatcher{
				WorkloadNamespace: "bar",
				WorkloadLabels: labels.Instance{
					"app":     "httpbin",
					"version": "v2",
				},
			},
			configs: []config.Config{
				newConfig("authz-1", "bar", policyWithSelector),
			},
			wantAllow: nil,
		},
		{
			name: "namespace not match",
			selectionOpts: WorkloadPolicyMatcher{
				WorkloadNamespace: "foo",
				WorkloadLabels: labels.Instance{
					"app":     "httpbin",
					"version": "v1",
				},
			},
			configs: []config.Config{
				newConfig("authz-1", "bar", policyWithSelector),
			},
			wantAllow: nil,
		},
		{
			name: "root namespace",
			selectionOpts: WorkloadPolicyMatcher{
				WorkloadNamespace: "bar",
			},
			configs: []config.Config{
				newConfig("authz-1", "istio-config", policy),
			},
			wantAllow: []AuthorizationPolicy{
				{
					Name:      "authz-1",
					Namespace: "istio-config",
					Spec:      policy,
				},
			},
		},
		{
			name: "root namespace equals config namespace",
			selectionOpts: WorkloadPolicyMatcher{
				WorkloadNamespace: "istio-config",
			},
			configs: []config.Config{
				newConfig("authz-1", "istio-config", policy),
			},
			wantAllow: []AuthorizationPolicy{
				{
					Name:      "authz-1",
					Namespace: "istio-config",
					Spec:      policy,
				},
			},
		},
		{
			name: "root namespace and config namespace",
			selectionOpts: WorkloadPolicyMatcher{
				WorkloadNamespace: "bar",
			},
			configs: []config.Config{
				newConfig("authz-1", "istio-config", policy),
				newConfig("authz-2", "bar", policy),
			},
			wantAllow: []AuthorizationPolicy{
				{
					Name:      "authz-1",
					Namespace: "istio-config",
					Spec:      policy,
				},
				{
					Name:      "authz-2",
					Namespace: "bar",
					Spec:      policy,
				},
			},
		},
		{
			name: "waypoint service attached",
			selectionOpts: WorkloadPolicyMatcher{
				IsWaypoint: true,
				Services: []ServiceInfoForPolicyMatcher{
					{Name: "foo-svc", Namespace: "foo", Registry: provider.Kubernetes},
				},
				WorkloadNamespace: "foo",
				WorkloadLabels: labels.Instance{
					label.IoK8sNetworkingGatewayGatewayName.Name: "foo-waypoint",
					// labels match in selector policy but ignore them for waypoint
					"app":     "httpbin",
					"version": "v1",
				},
			},
			configs: []config.Config{
				newConfig("authz-1", "foo", policyWithServiceRef),
				newConfig("authz-2", "foo", policyWithSelector),
			},
			wantAllow: []AuthorizationPolicy{
				{Name: "authz-1", Namespace: "foo", Spec: policyWithServiceRef},
			},
		},
		{
			name: "waypoint with default policies",
			selectionOpts: WorkloadPolicyMatcher{
				IsWaypoint: true,
				Services: []ServiceInfoForPolicyMatcher{
					{Name: "foo-svc", Namespace: "foo", Registry: provider.Kubernetes},
				},
				WorkloadNamespace: "foo",
				WorkloadLabels: labels.Instance{
					label.IoK8sNetworkingGatewayGatewayName.Name: "foo-waypoint",
					// labels match in selector policy but ignore them for waypoint
					"app":     "httpbin",
					"version": "v1",
				},
				RootNamespace: "istio-config",
			},
			configs: []config.Config{
				newConfig("authz-1", "foo", policyWithServiceRef),
				newConfig("default", "istio-config", policyWithGWClassRef),
				newConfig("default-non-waypoint", "istio-config", policyWithSelector),
			},
			wantAllow: []AuthorizationPolicy{
				{Name: "default", Namespace: "istio-config", Spec: policyWithGWClassRef},
				{Name: "authz-1", Namespace: "foo", Spec: policyWithServiceRef},
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			authzPolicies := createFakeAuthorizationPolicies(tc.configs)

			result := authzPolicies.ListAuthorizationPolicies(tc.selectionOpts)
			if !reflect.DeepEqual(tc.wantAllow, result.Allow) {
				t.Errorf("wantAllow:%v\n but got: %v\n", tc.wantAllow, result.Allow)
			}
			if !reflect.DeepEqual(tc.wantDeny, result.Deny) {
				t.Errorf("wantDeny:%v\n but got: %v\n", tc.wantDeny, result.Deny)
			}
			if !reflect.DeepEqual(tc.wantAudit, result.Audit) {
				t.Errorf("wantAudit:%v\n but got: %v\n", tc.wantAudit, result.Audit)
			}
			if !reflect.DeepEqual(tc.wantCustom, result.Custom) {
				t.Errorf("wantCustom:%v\n but got: %v\n", tc.wantCustom, result.Custom)
			}
		})
	}
}

func createFakeAuthorizationPolicies(configs []config.Config) *AuthorizationPolicies {
	store := &authzFakeStore{}
	for _, cfg := range configs {
		store.add(cfg)
	}
	environment := &Environment{
		ConfigStore: store,
		Watcher:     meshwatcher.NewTestWatcher(&meshconfig.MeshConfig{RootNamespace: "istio-config"}),
	}
	authzPolicies := GetAuthorizationPolicies(environment)
	return authzPolicies
}

func newConfig(name, ns string, spec config.Spec) config.Config {
	return config.Config{
		Meta: config.Meta{
			GroupVersionKind: gvk.AuthorizationPolicy,
			Name:             name,
			Namespace:        ns,
		},
		Spec: spec,
	}
}

type authzFakeStore struct {
	data []struct {
		typ config.GroupVersionKind
		ns  string
		cfg config.Config
	}
}

func (fs *authzFakeStore) add(cfg config.Config) {
	fs.data = append(fs.data, struct {
		typ config.GroupVersionKind
		ns  string
		cfg config.Config
	}{
		typ: cfg.GroupVersionKind,
		ns:  cfg.Namespace,
		cfg: cfg,
	})
}

func (fs *authzFakeStore) Schemas() collection.Schemas {
	return collection.SchemasFor()
}

func (fs *authzFakeStore) Get(_ config.GroupVersionKind, _, _ string) *config.Config {
	return nil
}

func (fs *authzFakeStore) List(typ config.GroupVersionKind, namespace string) []config.Config {
	var configs []config.Config
	for _, data := range fs.data {
		if data.typ == typ {
			if namespace != "" && data.ns == namespace {
				continue
			}
			configs = append(configs, data.cfg)
		}
	}
	return configs
}

func (fs *authzFakeStore) Delete(_ config.GroupVersionKind, _, _ string, _ *string) error {
	return fmt.Errorf("not implemented")
}

func (fs *authzFakeStore) Create(config.Config) (string, error) {
	return "not implemented", nil
}

func (fs *authzFakeStore) Update(config.Config) (string, error) {
	return "not implemented", nil
}

func (fs *authzFakeStore) UpdateStatus(config.Config) (string, error) {
	return "not implemented", nil
}

func httpRouteTargetRefPolicy(action authpb.AuthorizationPolicy_Action, routeName string) *authpb.AuthorizationPolicy {
	return &authpb.AuthorizationPolicy{
		Action: action,
		TargetRef: &selectorpb.PolicyTargetReference{
			Group: gvk.HTTPRoute.Group,
			Kind:  gvk.HTTPRoute.Kind,
			Name:  routeName,
		},
	}
}

func TestListAuthorizationPoliciesForHTTPRoute(t *testing.T) {
	test.SetForTest(t, &features.EnableGatewayAPIHTTPRouteAuth, true)
	allowRouteA := httpRouteTargetRefPolicy(authpb.AuthorizationPolicy_ALLOW, "route-a")
	denyRouteA := httpRouteTargetRefPolicy(authpb.AuthorizationPolicy_DENY, "route-a")
	allowRouteB := httpRouteTargetRefPolicy(authpb.AuthorizationPolicy_ALLOW, "route-b")
	// AUDIT is not a valid action for an HTTPRoute targetRef and must not be indexed.
	auditRouteA := httpRouteTargetRefPolicy(authpb.AuthorizationPolicy_AUDIT, "route-a")
	// A workload-wide policy must never show up in the route index.
	workloadWide := &authpb.AuthorizationPolicy{Action: authpb.AuthorizationPolicy_ALLOW}

	configs := []config.Config{
		newConfig("allow-a", "foo", allowRouteA),
		newConfig("deny-a", "foo", denyRouteA),
		newConfig("allow-b", "foo", allowRouteB),
		newConfig("audit-a", "foo", auditRouteA),
		newConfig("workload-wide", "foo", workloadWide),
		// Same route name in a different namespace must be a distinct key.
		newConfig("allow-a-other-ns", "bar", allowRouteA),
	}
	authzPolicies := createFakeAuthorizationPolicies(configs)

	cases := []struct {
		name      string
		route     types.NamespacedName
		wantAllow []string
		wantDeny  []string
	}{
		{
			name:      "allow and deny targeting the route",
			route:     types.NamespacedName{Name: "route-a", Namespace: "foo"},
			wantAllow: []string{"allow-a"},
			wantDeny:  []string{"deny-a"},
		},
		{
			name:      "only allow targeting the route",
			route:     types.NamespacedName{Name: "route-b", Namespace: "foo"},
			wantAllow: []string{"allow-b"},
		},
		{
			name:      "route in another namespace is a distinct key",
			route:     types.NamespacedName{Name: "route-a", Namespace: "bar"},
			wantAllow: []string{"allow-a-other-ns"},
		},
		{
			name:  "unknown route has no policies",
			route: types.NamespacedName{Name: "missing", Namespace: "foo"},
		},
		{
			name:  "zero value route has no policies",
			route: types.NamespacedName{},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := authzPolicies.ListAuthorizationPoliciesForHTTPRoute(tc.route)
			assertPolicyNames(t, "allow", got.Allow, tc.wantAllow)
			assertPolicyNames(t, "deny", got.Deny, tc.wantDeny)
			// Only ALLOW/DENY are valid for HTTPRoute targetRefs.
			if len(got.Audit) != 0 {
				t.Errorf("expected no audit policies, got %d", len(got.Audit))
			}
			if len(got.Custom) != 0 {
				t.Errorf("expected no custom policies, got %d", len(got.Custom))
			}
		})
	}
}

func assertPolicyNames(t *testing.T, action string, got []AuthorizationPolicy, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%s: got %d policies, want %d", action, len(got), len(want))
	}
	for i, name := range want {
		if got[i].Name != name {
			t.Errorf("%s[%d]: got %q, want %q", action, i, got[i].Name, name)
		}
	}
}

func TestListAuthorizationPoliciesRootDeny(t *testing.T) {
	denySpec := func() *authpb.AuthorizationPolicy {
		return &authpb.AuthorizationPolicy{
			Action: authpb.AuthorizationPolicy_DENY,
			Rules:  []*authpb.Rule{{}},
		}
	}
	allowSpec := func() *authpb.AuthorizationPolicy {
		return &authpb.AuthorizationPolicy{
			Action: authpb.AuthorizationPolicy_ALLOW,
			Rules:  []*authpb.Rule{{}},
		}
	}

	configs := []config.Config{
		newConfig("root-deny", "istio-config", denySpec()),
		newConfig("ns-deny", "foo", denySpec()),
		newConfig("root-allow", "istio-config", allowSpec()),
		newConfig("ns-allow", "foo", allowSpec()),
	}

	opts := WorkloadPolicyMatcher{
		WorkloadNamespace: "foo",
		WorkloadLabels:    labels.Instance{"app": "httpbin"},
		RootNamespace:     "istio-config",
	}

	t.Run("enabled splits root deny", func(t *testing.T) {
		test.SetForTest(t, &features.EnableGatewayAPIHTTPRouteAuth, true)
		got := createFakeAuthorizationPolicies(configs).ListAuthorizationPolicies(opts)
		assertPolicyNames(t, "rootDeny", got.RootDeny, []string{"root-deny"})
		assertPolicyNames(t, "deny", got.Deny, []string{"ns-deny"})
		// ALLOW stays merged: chaining separate ALLOW filters would turn Istio's OR into an AND.
		assertPolicyNames(t, "allow", got.Allow, []string{"root-allow", "ns-allow"})
	})

	t.Run("disabled keeps root deny merged", func(t *testing.T) {
		test.SetForTest(t, &features.EnableGatewayAPIHTTPRouteAuth, false)
		got := createFakeAuthorizationPolicies(configs).ListAuthorizationPolicies(opts)
		if len(got.RootDeny) != 0 {
			t.Errorf("expected no root deny policies, got %d", len(got.RootDeny))
		}
		assertPolicyNames(t, "deny", got.Deny, []string{"root-deny", "ns-deny"})
	})
}

func TestIndexHTTPRoutePoliciesFeatureGate(t *testing.T) {
	test.SetForTest(t, &features.EnableGatewayAPIHTTPRouteAuth, false)
	configs := []config.Config{
		newConfig("allow-a", "foo", httpRouteTargetRefPolicy(authpb.AuthorizationPolicy_ALLOW, "route-a")),
	}
	got := createFakeAuthorizationPolicies(configs).
		ListAuthorizationPoliciesForHTTPRoute(types.NamespacedName{Name: "route-a", Namespace: "foo"})
	if len(got.Allow) != 0 || len(got.Deny) != 0 {
		t.Errorf("expected empty route index when the feature is disabled, got allow=%d deny=%d", len(got.Allow), len(got.Deny))
	}
}
