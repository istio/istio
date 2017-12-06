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

package registry

import (
	"reflect"
	"testing"

	"k8s.io/api/core/v1"
	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"istio.io/istio/pilot/platform/kube"
)

func createService(svcAcct string) *v1.Service {
	return &v1.Service{
		ObjectMeta: meta_v1.ObjectMeta{
			Annotations: map[string]string{
				kube.KubeServiceAccountsOnVMAnnotation: svcAcct,
			},
		},
	}
}

type servicePair struct {
	oldSvc *v1.Service
	newSvc *v1.Service
}

func TestK8SHandlers(t *testing.T) {
	testCases := map[string]struct {
		toAdd    *v1.Service
		toDelete *v1.Service
		toUpdate *servicePair
		mapping  map[string]string
	}{
		"add k8s service": {
			toAdd: createService("svc@test.serviceaccount.com"),
			mapping: map[string]string{
				"svc@test.serviceaccount.com": "svc@test.serviceaccount.com",
			},
		},
		"add and delete k8s service": {
			toAdd:    createService("svc@test.serviceaccount.com"),
			toDelete: createService("svc@test.serviceaccount.com"),
			mapping:  map[string]string{},
		},
		"add and update k8s service": {
			toAdd: createService("svc1@test.serviceaccount.com"),
			toUpdate: &servicePair{
				oldSvc: createService("svc1@test.serviceaccount.com"),
				newSvc: createService("svc2@test.serviceaccount.com"),
			},
			mapping: map[string]string{
				"svc2@test.serviceaccount.com": "svc2@test.serviceaccount.com",
			},
		},
	}

	for id, c := range testCases {
		// clear registry
		reg = &IdentityRegistry{
			Map: make(map[string]string),
		}

		if c.toAdd != nil {
			K8SServiceAdded(c.toAdd)
		}
		if c.toDelete != nil {
			K8SServiceDeleted(c.toDelete)
		}
		if c.toUpdate != nil {
			K8SServiceUpdated(c.toUpdate.oldSvc, c.toUpdate.newSvc)
		}

		if !reflect.DeepEqual(reg.(*IdentityRegistry).Map, c.mapping) {
			t.Errorf("%s: registry content don't match. Expected %v, Actual %v", id, c.mapping, reg.(*IdentityRegistry).Map)
		}
	}
}
