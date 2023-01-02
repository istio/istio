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

package xds

import (
	"testing"

	discovery "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"

	authpb "istio.io/api/security/v1beta1"
	grpname "istio.io/istio/operator/pkg/name"
	"istio.io/istio/pilot/pkg/ambient"
	"istio.io/istio/pilot/pkg/model"
	v3 "istio.io/istio/pilot/pkg/xds/v3"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/rbacapi"
	"istio.io/istio/pkg/test/util/assert"
	"istio.io/istio/pkg/util/sets"
)

func TestRBAC(t *testing.T) {
	expect := func(resp *discovery.DeltaDiscoveryResponse, names ...string) {
		t.Helper()
		want := sets.New(names...)
		have := sets.New[string]()
		for _, r := range resp.Resources {
			rbac := &rbacapi.RBAC{}
			r.Resource.UnmarshalTo(rbac)
			have.Insert(rbac.Name)
		}
		assert.Equal(t, sets.SortedList(have), sets.SortedList(want))
	}
	expectRemoved := func(resp *discovery.DeltaDiscoveryResponse, names ...string) {
		t.Helper()
		want := sets.New(names...)
		have := sets.New[string]()
		for _, r := range resp.RemovedResources {
			have.Insert(r)
		}
		assert.Equal(t, sets.SortedList(have), sets.SortedList(want))
	}

	s := NewFakeDiscoveryServer(t, FakeOptions{})
	ads := s.ConnectDeltaADS().WithType(v3.RBACType).WithMetadata(model.NodeMetadata{
		AmbientType: ambient.TypeZTunnel,
	})
	ads.Request(&discovery.DeltaDiscoveryRequest{
		ResourceNamesSubscribe: []string{"*"},
	})
	ads.ExpectEmptyResponse()

	// Create the first authorization policy, due to wildcard subscribe we should receive it
	rev1 := makeIstioObject(t, s.Store(), createAuthzPolicyConfig("policy1", "ns1"))
	expect(ads.ExpectResponse(), "policy1/ns1")

	// A new policy in the same namespace should push only that one
	rev3 := makeIstioObject(t, s.Store(), createAuthzPolicyConfig("policy3", "ns1"))
	expect(ads.ExpectResponse(), "policy3/ns1")

	// A new policy in a different namespace should push only that one
	rev2 := makeIstioObject(t, s.Store(), createAuthzPolicyConfig("policy2", "ns2"))
	expect(ads.ExpectResponse(), "policy2/ns2")

	// Delete a policy should push only the name of the deleted resource
	deleteIstioObject(t, s.Store(), "policy3", "ns1", rev3)
	expectRemoved(ads.ExpectResponse(), "policy3/ns1")

	// A new policy in the same namespace should push only that one
	rev3 = makeIstioObject(t, s.Store(), createAuthzPolicyConfig("policy3", "ns1"))
	expect(ads.ExpectResponse(), "policy3/ns1")

	// Delete a policy should push only the name of the deleted resource
	deleteIstioObject(t, s.Store(), "policy3", "ns1", rev3)
	expectRemoved(ads.ExpectResponse(), "policy3/ns1")

	// Delete a policy should push only the name of the deleted resource
	deleteIstioObject(t, s.Store(), "policy2", "ns2", rev2)
	expectRemoved(ads.ExpectResponse(), "policy2/ns2")

	// Delete a policy should push only the name of the deleted resource
	deleteIstioObject(t, s.Store(), "policy1", "ns1", rev1)
	expectRemoved(ads.ExpectResponse(), "policy1/ns1")
}

func createAuthzPolicyConfig(name string, ns string) config.Config {
	authzPolicy := config.Config{
		Meta: config.Meta{
			Name:             name,
			Namespace:        ns,
			Domain:           "cluster.local",
			GroupVersionKind: authzPolicyGVK(),
		},
		Spec: &authpb.AuthorizationPolicy{
			Rules: []*authpb.Rule{
				{
					From: []*authpb.Rule_From{
						{
							Source: &authpb.Source{
								Principals: []string{"cluster.local/ns/default/sa/sleep"},
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
		},
	}
	return authzPolicy
}

func makeIstioObject(t *testing.T, store model.ConfigStore, cfg config.Config) string {
	t.Helper()
	rev, err := store.Create(cfg)
	if err != nil && err.Error() == "item already exists" {
		rev, err = store.Update(cfg)
	}
	if err != nil {
		t.Fatal(err)
	}
	return rev
}

func deleteIstioObject(t *testing.T, store model.ConfigStore, name, namespace, revision string) {
	t.Helper()
	err := store.Delete(authzPolicyGVK(), name, namespace, &revision)
	if err != nil {
		t.Fatal(err)
	}
}

func authzPolicyGVK() config.GroupVersionKind {
	return config.GroupVersionKind{
		Group:   grpname.SecurityAPIGroupName,
		Version: "v1beta1",
		Kind:    "AuthorizationPolicy",
	}
}

// func createAuthorizationPolicy(s *FakeDiscoveryServer, name string, ns string) *v1beta1.AuthorizationPolicy {
// 	authzPolicy := &v1beta1.AuthorizationPolicy{
// 		ObjectMeta: metav1.ObjectMeta{
// 			Name:      name,
// 			Namespace: ns,
// 		},
// 		Spec: authpb.AuthorizationPolicy{
// 			Rules: []*authpb.Rule{
// 				{
// 					From: []*authpb.Rule_From{
// 						{
// 							Source: &authpb.Source{
// 								Principals: []string{"cluster.local/ns/default/sa/sleep"},
// 							},
// 						},
// 					},
// 					To: []*authpb.Rule_To{
// 						{
// 							Operation: &authpb.Operation{
// 								Methods: []string{"GET"},
// 							},
// 						},
// 					},
// 				},
// 			},
// 		},
// 	}
// 	created, err := s.kubeClient.Istio().SecurityV1beta1().AuthorizationPolicies(ns).Create(context.Background(), authzPolicy, v1.CreateOptions{})
// 	if err != nil {
// 		log.Error(err)
// 	}
// 	return created
// }
