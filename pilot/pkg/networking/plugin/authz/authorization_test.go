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

package authz

import (
	"sort"
	"strings"
	"testing"

	rbachttp "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/rbac/v3"
	hcm "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/http_connection_manager/v3"
	"k8s.io/apimachinery/pkg/types"

	meshconfig "istio.io/api/mesh/v1alpha1"
	networking "istio.io/api/networking/v1alpha3"
	authpb "istio.io/api/security/v1beta1"
	selectorpb "istio.io/api/type/v1beta1"
	"istio.io/istio/pilot/pkg/config/memory"
	"istio.io/istio/pilot/pkg/model"
	istionetworking "istio.io/istio/pilot/pkg/networking"
	"istio.io/istio/pilot/pkg/util/protoconv"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/schema/collections"
	"istio.io/istio/pkg/config/schema/gvk"
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/wellknown"
)

// TestBuilder_ListenerSetScoping verifies a ListenerSet-targeted policy only shows up for that
// ListenerSet's own scope, while a Gateway-targeted policy shows up for every scope.
func TestBuilder_ListenerSetScoping(t *testing.T) {
	gwPolicy := authzPolicyConfig("gw-policy", "default", &selectorpb.PolicyTargetReference{
		Group: gvk.KubernetesGateway.Group,
		Kind:  gvk.KubernetesGateway.Kind,
		Name:  "my-gw",
	})
	lsAPolicy := authzPolicyConfig("ls-a-policy", "default", &selectorpb.PolicyTargetReference{
		Group: gvk.ListenerSet.Group,
		Kind:  gvk.ListenerSet.Kind,
		Name:  "ls-a",
	})
	lsBPolicy := authzPolicyConfig("ls-b-policy", "default", &selectorpb.PolicyTargetReference{
		Group: gvk.ListenerSet.Group,
		Kind:  gvk.ListenerSet.Kind,
		Name:  "ls-b",
	})

	store := memory.Make(collections.Pilot, false, test.NewStop(t))
	for _, c := range []config.Config{gwPolicy, lsAPolicy, lsBPolicy} {
		if _, err := store.Create(c); err != nil {
			t.Fatalf("failed to create config: %v", err)
		}
	}
	push := &model.PushContext{
		AuthzPolicies: model.GetAuthorizationPolicies(&model.Environment{ConfigStore: store}),
		Mesh:          &meshconfig.MeshConfig{},
	}

	serverA := &networking.Server{}
	serverB := &networking.Server{}
	proxy := &model.Proxy{
		Type:            model.Router,
		ConfigNamespace: "default",
		Labels:          map[string]string{"gateway.networking.k8s.io/gateway-name": "my-gw"},
		MergedGateway: &model.MergedGateway{
			ListenerSetForServer: map[*networking.Server]types.NamespacedName{
				serverA: {Namespace: "default", Name: "ls-a"},
				serverB: {Namespace: "default", Name: "ls-b"},
			},
		},
	}

	b := NewBuilder(Local, push, proxy, false)
	if b == nil {
		t.Fatal("expected non-nil builder")
	}

	cases := []struct {
		name  string
		scope types.NamespacedName
		want  []string
	}{
		{"native gateway listener", types.NamespacedName{}, []string{"gw-policy"}},
		{"ls-a listener", types.NamespacedName{Namespace: "default", Name: "ls-a"}, []string{"gw-policy", "ls-a-policy"}},
		{"ls-b listener", types.NamespacedName{Namespace: "default", Name: "ls-b"}, []string{"gw-policy", "ls-b-policy"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := policyNamesIn(t, b.BuildHTTP(istionetworking.ListenerClassGateway, tc.scope))
			assertSamePolicyNames(t, tc.want, got)
		})
	}
}

func authzPolicyConfig(name, namespace string, targetRef *selectorpb.PolicyTargetReference) config.Config {
	return config.Config{
		Meta: config.Meta{
			GroupVersionKind: gvk.AuthorizationPolicy,
			Name:             name,
			Namespace:        namespace,
		},
		Spec: &authpb.AuthorizationPolicy{
			Action:    authpb.AuthorizationPolicy_ALLOW,
			TargetRef: targetRef,
			Rules: []*authpb.Rule{
				{
					To: []*authpb.Rule_To{{Operation: &authpb.Operation{Methods: []string{"GET"}}}},
				},
			},
		},
	}
}

// policyNamesIn extracts the policy names embedded in the RBAC filters' policy keys, which are
// formatted as "ns[<namespace>]-policy[<name>]-rule[<i>]" by builder.policyName.
func policyNamesIn(t *testing.T, filters []*hcm.HttpFilter) []string {
	t.Helper()
	seen := map[string]bool{}
	for _, f := range filters {
		if f.GetName() != wellknown.HTTPRoleBasedAccessControl {
			continue
		}
		rbac, err := protoconv.UnmarshalAny[rbachttp.RBAC](f.GetTypedConfig())
		if err != nil {
			t.Fatalf("failed to unmarshal RBAC filter: %v", err)
		}
		for key := range rbac.GetRules().GetPolicies() {
			start := strings.Index(key, "-policy[")
			end := strings.Index(key, "]-rule[")
			if start < 0 || end < 0 {
				t.Fatalf("unexpected policy key format: %q", key)
			}
			seen[key[start+len("-policy["):end]] = true
		}
	}
	names := make([]string, 0, len(seen))
	for name := range seen {
		names = append(names, name)
	}
	return names
}

func assertSamePolicyNames(t *testing.T, want, got []string) {
	t.Helper()
	sort.Strings(want)
	sort.Strings(got)
	if strings.Join(want, ",") != strings.Join(got, ",") {
		t.Errorf("policy names mismatch: want %v, got %v", want, got)
	}
}
