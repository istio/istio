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

	"github.com/gogo/protobuf/proto"

	meshconfig "istio.io/api/mesh/v1alpha1"
	authpb "istio.io/api/security/v1beta1"
	selectorpb "istio.io/api/type/v1beta1"
	"istio.io/pkg/ledger"

	"istio.io/istio/pkg/config/labels"
	"istio.io/istio/pkg/config/mesh"
	"istio.io/istio/pkg/config/schema/collection"
	"istio.io/istio/pkg/config/schema/collections"
	"istio.io/istio/pkg/config/schema/resource"
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

	cases := []struct {
		name           string
		ns             string
		workloadLabels map[string]string
		configs        []Config
		wantDeny       []AuthorizationPolicy
		wantAllow      []AuthorizationPolicy
	}{
		{
			name:      "no policies",
			ns:        "foo",
			wantAllow: nil,
		},
		{
			name: "no policies in namespace foo",
			ns:   "foo",
			configs: []Config{
				newConfig("authz-1", "bar", policy),
				newConfig("authz-2", "bar", policy),
			},
			wantAllow: nil,
		},
		{
			name: "one allow policy",
			ns:   "bar",
			configs: []Config{
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
			configs: []Config{
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
			name: "two policies",
			ns:   "bar",
			configs: []Config{
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
			name: "mixing allow and deny policies",
			ns:   "bar",
			configs: []Config{
				newConfig("authz-1", "bar", policy),
				newConfig("authz-2", "bar", denyPolicy),
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
		},
		{
			name: "selector exact match",
			ns:   "bar",
			workloadLabels: map[string]string{
				"app":     "httpbin",
				"version": "v1",
			},
			configs: []Config{
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
			configs: []Config{
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
			configs: []Config{
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
			configs: []Config{
				newConfig("authz-1", "bar", policyWithSelector),
			},
			wantAllow: nil,
		},
		{
			name: "root namespace",
			ns:   "bar",
			configs: []Config{
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
			configs: []Config{
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
			configs: []Config{
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

			gotDeny, gotAllow := authzPolicies.ListAuthorizationPolicies(
				tc.ns, []labels.Instance{tc.workloadLabels})
			if !reflect.DeepEqual(tc.wantAllow, gotAllow) {
				t.Errorf("wantAllow:%v\n but got: %v\n", tc.wantAllow, gotAllow)
			}
			if !reflect.DeepEqual(tc.wantDeny, gotDeny) {
				t.Errorf("wantDeny:%v\n but got: %v\n", tc.wantDeny, gotDeny)
			}
		})
	}
}

func createFakeAuthorizationPolicies(configs []Config, t *testing.T) *AuthorizationPolicies {
	store := &authzFakeStore{}
	for _, cfg := range configs {
		store.add(cfg)
	}
	environment := &Environment{
		IstioConfigStore: MakeIstioStore(store),
		Watcher:          mesh.NewFixedWatcher(&meshconfig.MeshConfig{RootNamespace: "istio-config"}),
	}
	authzPolicies, err := GetAuthorizationPolicies(environment)
	if err != nil {
		t.Fatalf("GetAuthorizationPolicies failed: %v", err)
	}
	return authzPolicies
}

func newConfig(name, ns string, spec proto.Message) Config {
	return Config{
		ConfigMeta: ConfigMeta{
			GroupVersionKind: collections.IstioSecurityV1Beta1Authorizationpolicies.Resource().GroupVersionKind(),
			Name:             name,
			Namespace:        ns,
		},
		Spec: spec,
	}
}

type authzFakeStore struct {
	data []struct {
		typ resource.GroupVersionKind
		ns  string
		cfg Config
	}
}

func (fs *authzFakeStore) GetLedger() ledger.Ledger {
	panic("implement me")
}

func (fs *authzFakeStore) SetLedger(ledger.Ledger) error {
	panic("implement me")
}

func (fs *authzFakeStore) add(config Config) {
	fs.data = append(fs.data, struct {
		typ resource.GroupVersionKind
		ns  string
		cfg Config
	}{
		typ: config.GroupVersionKind,
		ns:  config.Namespace,
		cfg: config,
	})
}

func (fs *authzFakeStore) Schemas() collection.Schemas {
	return collection.SchemasFor()
}

func (fs *authzFakeStore) Get(_ resource.GroupVersionKind, _, _ string) *Config {
	return nil
}

func (fs *authzFakeStore) List(typ resource.GroupVersionKind, namespace string) ([]Config, error) {
	var configs []Config
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

func (fs *authzFakeStore) Delete(_ resource.GroupVersionKind, _, _ string) error {
	return fmt.Errorf("not implemented")
}
func (fs *authzFakeStore) Create(Config) (string, error) {
	return "not implemented", nil
}

func (fs *authzFakeStore) Update(Config) (string, error) {
	return "not implemented", nil
}

func (fs *authzFakeStore) Version() string {
	return "not implemented"
}
func (fs *authzFakeStore) GetResourceAtVersion(version string, key string) (resourceVersion string, err error) {
	return "not implemented", nil
}
