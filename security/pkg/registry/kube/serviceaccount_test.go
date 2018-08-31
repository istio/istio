// Copyright 2017 Istio Authors
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

package kube

import (
	"reflect"
	"testing"

	"k8s.io/api/core/v1"
	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	"istio.io/istio/security/pkg/registry"
)

func createServiceAccount(name, namespace string) *v1.ServiceAccount {
	return &v1.ServiceAccount{
		ObjectMeta: meta_v1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
	}
}

type serviceAccountPair struct {
	oldSvcAcct *v1.ServiceAccount
	newSvcAcct *v1.ServiceAccount
}

func TestSpiffeID(t *testing.T) {
	testCases := []struct {
		name      string
		namespace string
		expected  string
	}{
		{
			name:      "foo",
			namespace: "bar",
			expected:  "spiffe://cluster.local/ns/bar/sa/foo",
		},
		{
			name:      "foo",
			namespace: "",
			expected:  "spiffe://cluster.local/ns//sa/foo",
		},
		{
			name:      "",
			namespace: "bar",
			expected:  "spiffe://cluster.local/ns/bar/sa/",
		},
		{
			name:      "",
			namespace: "",
			expected:  "spiffe://cluster.local/ns//sa/",
		},
		{
			name:      "svc@test.serviceaccount.com",
			namespace: "default",
			expected:  "spiffe://cluster.local/ns/default/sa/svc@test.serviceaccount.com",
		},
	}
	for _, c := range testCases {
		if id := getSpiffeID(createServiceAccount(c.name, c.namespace)); id != c.expected {
			t.Errorf("getSpiffeID(%s, %s): expected %s, got %s", c.name, c.namespace, c.expected, id)
		}
	}
}

func TestServiceAccountController(t *testing.T) {
	namespace := "test-ns"
	sa := createServiceAccount("svc@test.serviceaccount.com", namespace)
	sa1 := createServiceAccount("svc1@test.serviceaccount.com", namespace)
	sa2 := createServiceAccount("svc2@test.serviceaccount.com", namespace)
	sa3 := createServiceAccount("svc@test.serviceaccount.com", "on-another-galaxy")

	testCases := map[string]struct {
		toAdd    []*v1.ServiceAccount
		toDelete []*v1.ServiceAccount
		toUpdate *serviceAccountPair
		mapping  map[string]string
	}{
		"add k8s service account": {
			toAdd: []*v1.ServiceAccount{sa},
			mapping: map[string]string{
				getSpiffeID(sa): getSpiffeID(sa),
			},
		},
		"add and delete k8s service account": {
			toAdd:    []*v1.ServiceAccount{sa},
			toDelete: []*v1.ServiceAccount{sa},
			mapping:  map[string]string{},
		},
		"add and update k8s service account": {
			toAdd: []*v1.ServiceAccount{sa1},
			toUpdate: &serviceAccountPair{
				oldSvcAcct: sa1,
				newSvcAcct: sa2,
			},
			mapping: map[string]string{
				getSpiffeID(sa2): getSpiffeID(sa2),
			},
		},
		"multiple add of different k8s service account": {
			toAdd: []*v1.ServiceAccount{sa, sa1, sa2},
			mapping: map[string]string{
				getSpiffeID(sa):  getSpiffeID(sa),
				getSpiffeID(sa1): getSpiffeID(sa1),
				getSpiffeID(sa2): getSpiffeID(sa2),
			},
		},
		"multiple add of the same k8s service account": {
			toAdd: []*v1.ServiceAccount{sa, sa},
			mapping: map[string]string{
				getSpiffeID(sa): getSpiffeID(sa),
			},
		},
		"multiple delete of the same k8s service account": {
			toAdd:    []*v1.ServiceAccount{sa},
			toDelete: []*v1.ServiceAccount{sa, sa1, sa},
			mapping:  map[string]string{},
		},
		"update none existing": {
			toUpdate: &serviceAccountPair{
				oldSvcAcct: sa1,
				newSvcAcct: sa2,
			},
			mapping: map[string]string{
				getSpiffeID(sa2): getSpiffeID(sa2),
			},
		},
		"update with same svc account": {
			toAdd: []*v1.ServiceAccount{sa1},
			toUpdate: &serviceAccountPair{
				oldSvcAcct: sa1,
				newSvcAcct: sa1,
			},
			mapping: map[string]string{
				getSpiffeID(sa1): getSpiffeID(sa1),
			},
		},
		"update with same non-exist svc account": {
			toUpdate: &serviceAccountPair{
				oldSvcAcct: sa1,
				newSvcAcct: sa1,
			},
			mapping: map[string]string{},
		},
		"update with sva from different namespace": {
			toAdd: []*v1.ServiceAccount{sa},
			toUpdate: &serviceAccountPair{
				oldSvcAcct: sa,
				newSvcAcct: sa3,
			},
			mapping: map[string]string{
				getSpiffeID(sa3): getSpiffeID(sa3),
			},
		},
	}

	client := fake.NewSimpleClientset()
	for id, c := range testCases {
		reg := &registry.IdentityRegistry{
			Map: make(map[string]string),
		}
		controller := NewServiceAccountController(client, "test-ns", reg)

		for _, svcAcc := range c.toAdd {
			controller.serviceAccountAdded(svcAcc)
		}
		for _, svcAcc := range c.toDelete {
			controller.serviceAccountDeleted(svcAcc)
		}
		if c.toUpdate != nil {
			controller.serviceAccountUpdated(c.toUpdate.oldSvcAcct, c.toUpdate.newSvcAcct)
		}

		if !reflect.DeepEqual(reg.Map, c.mapping) {
			t.Errorf("%s: registry content don't match. Expected %v, Actual %v", id, c.mapping, reg.Map)
		}
	}
}
