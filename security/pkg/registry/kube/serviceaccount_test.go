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

func TestServiceAccountController(t *testing.T) {
	namespace := "test-ns"
	sa := createServiceAccount("svc@test.serviceaccount.com", namespace)
	sa1 := createServiceAccount("svc1@test.serviceaccount.com", namespace)
	sa2 := createServiceAccount("svc2@test.serviceaccount.com", namespace)

	testCases := map[string]struct {
		toAdd    *v1.ServiceAccount
		toDelete *v1.ServiceAccount
		toUpdate *serviceAccountPair
		mapping  map[string]string
	}{
		"add k8s service account": {
			toAdd: sa,
			mapping: map[string]string{
				getSpiffeID(sa): getSpiffeID(sa),
			},
		},
		"add and delete k8s service account": {
			toAdd:    sa,
			toDelete: sa,
			mapping:  map[string]string{},
		},
		"add and update k8s service account": {
			toAdd: sa1,
			toUpdate: &serviceAccountPair{
				oldSvcAcct: sa1,
				newSvcAcct: sa2,
			},
			mapping: map[string]string{
				getSpiffeID(sa2): getSpiffeID(sa2),
			},
		},
	}

	client := fake.NewSimpleClientset()
	for id, c := range testCases {
		reg := &registry.IdentityRegistry{
			Map: make(map[string]string),
		}
		controller := NewServiceAccountController(client.CoreV1(), "test-ns", reg)

		if c.toAdd != nil {
			controller.serviceAccountAdded(c.toAdd)
		}
		if c.toDelete != nil {
			controller.serviceAccountDeleted(c.toDelete)
		}
		if c.toUpdate != nil {
			controller.serviceAccountUpdated(c.toUpdate.oldSvcAcct, c.toUpdate.newSvcAcct)
		}

		if !reflect.DeepEqual(reg.Map, c.mapping) {
			t.Errorf("%s: registry content don't match. Expected %v, Actual %v", id, c.mapping, reg.Map)
		}
	}
}
