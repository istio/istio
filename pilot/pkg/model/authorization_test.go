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

	"google.golang.org/protobuf/proto"

	meshconfig "istio.io/api/mesh/v1alpha1"
	authpb "istio.io/api/security/v1beta1"
	selectorpb "istio.io/api/type/v1beta1"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/mesh"
	"istio.io/istio/pkg/config/schema/collection"
	"istio.io/istio/pkg/config/schema/collections"
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
	policyWithSelector := proto.Clone(policy).(*authpb.AuthorizationPolicy)
	policyWithSelector.Selector = &selectorpb.WorkloadSelector{
		MatchLabels: map[string]string{
			"app":     "httpbin",
			"version": "v1",
		},
	}
	denyPolicy := proto.Clone(policy).(*authpb.AuthorizationPolicy)
	denyPolicy.Action = authpb.AuthorizationPolicy_DENY

	auditPolicy := proto.Clone(policy).(*authpb.AuthorizationPolicy)
	auditPolicy.Action = authpb.AuthorizationPolicy_AUDIT

	customPolicy := proto.Clone(policy).(*authpb.AuthorizationPolicy)
	customPolicy.Action = authpb.AuthorizationPolicy_CUSTOM

	cases := []struct {
		name           string
		ns             string
		workloadLabels map[string]string
		configs        []config.Config
		wantDeny       []AuthorizationPolicy
		wantAllow      []AuthorizationPolicy
		wantAudit      []AuthorizationPolicy
		wantCustom     []AuthorizationPolicy
	}{
		{
			name:      "no policies",
			ns:        "foo",
			wantAllow: nil,
		},
		{
			name: "no policies in namespace foo",
			ns:   "foo",
			configs: []config.Config{
				newConfig("authz-1", "bar", policy),
				newConfig("authz-2", "bar", policy),
			},
			wantAllow: nil,
		},
		{
			name: "one allow policy",
			ns:   "bar",
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
			ns:   "bar",
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
			ns:   "bar",
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
			ns:   "bar",
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
			ns:   "bar",
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
			ns:   "bar",
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
			name: "selector exact match",
			ns:   "bar",
			workloadLabels: map[string]string{
				"app":     "httpbin",
				"version": "v1",
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
			ns:   "bar",
			workloadLabels: map[string]string{
				"app":     "httpbin",
				"version": "v1",
				"env":     "dev",
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
			name: "selector not match",
			ns:   "bar",
			workloadLabels: map[string]string{
				"app":     "httpbin",
				"version": "v2",
			},
			configs: []config.Config{
				newConfig("authz-1", "bar", policyWithSelector),
			},
			wantAllow: nil,
		},
		{
			name: "namespace not match",
			ns:   "foo",
			workloadLabels: map[string]string{
				"app":     "httpbin",
				"version": "v1",
			},
			configs: []config.Config{
				newConfig("authz-1", "bar", policyWithSelector),
			},
			wantAllow: nil,
		},
		{
			name: "root namespace",
			ns:   "bar",
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
			ns:   "istio-config",
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
			ns:   "bar",
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
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			authzPolicies := createFakeAuthorizationPolicies(tc.configs, t)

			result := authzPolicies.ListAuthorizationPolicies(tc.ns, tc.workloadLabels)
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

func createFakeAuthorizationPolicies(configs []config.Config, t *testing.T) *AuthorizationPolicies {
	store := &authzFakeStore{}
	for _, cfg := range configs {
		store.add(cfg)
	}
	environment := &Environment{
		ConfigStore: MakeIstioStore(store),
		Watcher:     mesh.NewFixedWatcher(&meshconfig.MeshConfig{RootNamespace: "istio-config"}),
	}
	authzPolicies, err := GetAuthorizationPolicies(environment)
	if err != nil {
		t.Fatalf("GetAuthorizationPolicies failed: %v", err)
	}
	return authzPolicies
}

func newConfig(name, ns string, spec config.Spec) config.Config {
	return config.Config{
		Meta: config.Meta{
			GroupVersionKind: collections.IstioSecurityV1Beta1Authorizationpolicies.Resource().GroupVersionKind(),
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

func (fs *authzFakeStore) List(typ config.GroupVersionKind, namespace string) ([]config.Config, error) {
	var configs []config.Config
	for _, data := range fs.data {
		if data.typ == typ {
			if namespace != "" && data.ns == namespace {
				continue
			}
			configs = append(configs, data.cfg)
		}
	}
	return configs, nil
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

func (fs *authzFakeStore) Patch(orig config.Config, patchFn config.PatchFunc) (string, error) {
	return "not implemented", nil
}
