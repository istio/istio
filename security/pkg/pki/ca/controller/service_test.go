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

package controller

import (
	"reflect"
	"sync"
	"testing"

	"k8s.io/api/core/v1"
	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

type reg struct {
	sync.Mutex
	m map[string]bool
}

var (
	r = &reg{
		m: make(map[string]bool),
	}
)

func (r *reg) simpleAdd(svc *v1.Service) {
	r.Lock()
	r.m[svc.ObjectMeta.Name] = true
	r.Unlock()
}

func (r *reg) simpleDelete(svc *v1.Service) {
	r.Lock()
	delete(r.m, svc.ObjectMeta.Name)
	r.Unlock()
}

func (r *reg) simpleUpdate(oldSvc, newSvc *v1.Service) {
	r.Lock()
	delete(r.m, oldSvc.ObjectMeta.Name)
	r.m[newSvc.ObjectMeta.Name] = true
	r.Unlock()
}

func createService(name, namespace string) *v1.Service {
	return &v1.Service{
		ObjectMeta: meta_v1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
	}
}

type servicePair struct {
	oldSvc *v1.Service
	newSvc *v1.Service
}

func TestServiceController(t *testing.T) {
	namespace := "test-ns"
	testCases := map[string]struct {
		toAdd            *v1.Service
		toDelete         *v1.Service
		toUpdate         *servicePair
		expectedServices map[string]bool
	}{
		"add a serivce": {
			toAdd:            createService("test-svc", namespace),
			expectedServices: map[string]bool{"test-svc": true},
		},
		"add and update a service": {
			toAdd: createService("test-svc1", namespace),
			toUpdate: &servicePair{
				oldSvc: createService("test-svc1", namespace),
				newSvc: createService("test-svc2", namespace),
			},
			expectedServices: map[string]bool{"test-svc2": true},
		},
		"add and delete a service": {
			toAdd:            createService("test-svc", namespace),
			toDelete:         createService("test-svc", namespace),
			expectedServices: map[string]bool{},
		},
	}

	client := fake.NewSimpleClientset()
	controller := NewServiceController(client.CoreV1(), namespace, r.simpleAdd, r.simpleDelete, r.simpleUpdate)
	for id, c := range testCases {
		r.m = make(map[string]bool)

		if c.toAdd != nil {
			controller.serviceAdded(c.toAdd)
		}
		if c.toDelete != nil {
			controller.serviceDeleted(c.toDelete)
		}
		if c.toUpdate != nil {
			controller.serviceUpdated(c.toUpdate.oldSvc, c.toUpdate.newSvc)
		}

		if !reflect.DeepEqual(r.m, c.expectedServices) {
			t.Errorf("%s: service names don't match. Expected %v, Actual %v", id, c.expectedServices, r.m)
		}
	}
}
