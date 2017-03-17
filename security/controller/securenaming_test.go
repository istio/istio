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
	"sort"
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/pkg/api/v1"
)

func TestProcessNextServices(t *testing.T) {
	cases := map[string]struct {
		expectedMapping  map[string]sets.String
		initialMapping   map[string]sets.String
		pods             []*v1.Pod
		serviceToProcess *v1.Service
		services         []*v1.Service
	}{
		"Non-existent service is removed from secure naming": {
			expectedMapping: map[string]sets.String{},
			initialMapping: map[string]sets.String{
				"default/svc": sets.NewString("acct"),
			},
			services:         []*v1.Service{},
			serviceToProcess: createService("svc", nil),
		},
		"An empty entry for a service is created properly": {
			expectedMapping: map[string]sets.String{
				"ns/svc": sets.NewString(),
			},
			initialMapping:   map[string]sets.String{},
			services:         []*v1.Service{createServiceWithNamespace("svc", "ns", nil)},
			serviceToProcess: createServiceWithNamespace("svc", "ns", nil),
		},
		"A service with service accounts": {
			expectedMapping: map[string]sets.String{
				"ns/svc": sets.NewString("acct1", "acct4"),
			},
			initialMapping: map[string]sets.String{},
			pods: []*v1.Pod{
				// A pod that is part of the service.
				createPod(&podSpec{
					labels:             map[string]string{"app": "test-app"},
					name:               "name1",
					namespace:          "ns",
					serviceAccountName: "acct1",
				}),
				// A pod that is NOT part of the service.
				createPod(&podSpec{
					labels:             map[string]string{"app": "prod-app"},
					name:               "name2",
					namespace:          "ns",
					serviceAccountName: "acct2",
				}),
				// A pod that is of a different namespace.
				createPod(&podSpec{
					labels:             map[string]string{"app": "prod-app"},
					name:               "name3",
					namespace:          "ns1",
					serviceAccountName: "acct3",
				}),
				// A pod that is of a different namespace.
				createPod(&podSpec{
					labels:             map[string]string{"app": "test-app"},
					name:               "name4",
					namespace:          "ns",
					serviceAccountName: "acct4",
				}),
			},
			services: []*v1.Service{
				createServiceWithNamespace("svc", "ns", map[string]string{"app": "test-app"}),
			},
			serviceToProcess: createServiceWithNamespace("svc", "ns", map[string]string{"app": "test-app"}),
		},
	}

	for d, c := range cases {
		core := fake.NewSimpleClientset().CoreV1()
		snc := NewSecureNamingController(core)

		snc.mapping.mapping = c.initialMapping
		snc.enqueueService(c.serviceToProcess)

		// Add services to the service indexer.
		for _, s := range c.services {
			err := snc.serviceIndexer.Add(s)
			if err != nil {
				t.Errorf("Cannot add service to the indexer (error: %v)", err)
			}
		}

		for _, p := range c.pods {
			_, err := core.Pods(p.GetNamespace()).Create(p)
			if err != nil {
				t.Errorf("Cannot create pod in namespace %s (error: %v)", p.GetNamespace(), err)
			}
		}

		snc.processNextService()

		if !reflect.DeepEqual(c.expectedMapping, snc.mapping.mapping) {
			t.Errorf("%s: expecting the mapping to be %v but the actual mapping is %v",
				d, c.expectedMapping, snc.mapping.mapping)
		}
	}
}

func TestGetPodServices(t *testing.T) {
	cases := []struct {
		allServices      []*v1.Service
		expectedServices []*v1.Service
		pod              *v1.Pod
	}{
		{
			allServices:      []*v1.Service{},
			expectedServices: []*v1.Service{},
			pod: createPod(&podSpec{
				labels: map[string]string{"app": "test-app"},
			}),
		},
		{
			allServices:      []*v1.Service{createService("service1", nil)},
			expectedServices: []*v1.Service{},
			pod: createPod(&podSpec{
				labels: map[string]string{"app": "test-app"},
			}),
		},
		{
			allServices:      []*v1.Service{createService("service1", map[string]string{"app": "prod-app"})},
			expectedServices: []*v1.Service{},
			pod: createPod(&podSpec{
				labels: map[string]string{"app": "test-app"},
			}),
		},
		{
			allServices:      []*v1.Service{createService("service1", map[string]string{"app": "test-app"})},
			expectedServices: []*v1.Service{createService("service1", map[string]string{"app": "test-app"})},
			pod: createPod(&podSpec{
				labels: map[string]string{"app": "test-app"},
			}),
		},
		{
			allServices: []*v1.Service{
				createServiceWithNamespace("service1", "non-default", map[string]string{"app": "test-app"}),
			},
			expectedServices: []*v1.Service{},
			pod: createPod(&podSpec{
				labels: map[string]string{"app": "test-app"},
			}),
		},
		{
			allServices: []*v1.Service{
				createService("service1", map[string]string{"app": "prod-app"}),
				createService("service2", map[string]string{"app": "test-app"}),
				createService("service3", map[string]string{"version": "v1"}),
			},
			expectedServices: []*v1.Service{
				createService("service2", map[string]string{"app": "test-app"}),
				createService("service3", map[string]string{"version": "v1"}),
			},
			pod: createPod(&podSpec{
				labels: map[string]string{
					"app":     "test-app",
					"version": "v1",
				},
			}),
		},
	}

	for ind, testCase := range cases {
		cs := fake.NewSimpleClientset()
		snc := NewSecureNamingController(cs.CoreV1())

		for _, service := range testCase.allServices {
			err := snc.serviceIndexer.Add(service)
			if err != nil {
				t.Errorf("Failed adding service to the indexer (error: %v)", err)
			}
		}

		actualServices := snc.getPodServices(testCase.pod)
		sort.Sort(sortableServices(actualServices))

		if !reflect.DeepEqual(actualServices, testCase.expectedServices) {
			t.Errorf("Case %d failed: Actual services does not match expected services\n", ind)
		}
	}
}

func createService(name string, selector map[string]string) *v1.Service {
	return createServiceWithNamespace(name, "default", selector)
}

func createServiceWithNamespace(name, namespace string, selector map[string]string) *v1.Service {
	return &v1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
		Spec:       v1.ServiceSpec{Selector: selector},
	}
}

type podSpec struct {
	labels             map[string]string
	name               string
	namespace          string
	serviceAccountName string
}

func createPod(ps *podSpec) *v1.Pod {
	return &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      getOrDefault(ps.name, "default-name"),
			Labels:    ps.labels,
			Namespace: getOrDefault(ps.namespace, "default"),
		},
		Spec: v1.PodSpec{
			ServiceAccountName: getOrDefault(ps.serviceAccountName, "default"),
		},
	}
}

func getOrDefault(value, defaultValue string) string {
	if value != "" {
		return value
	}
	return defaultValue
}

type sortableServices []*v1.Service

func (ss sortableServices) Len() int      { return len(ss) }
func (ss sortableServices) Swap(i, j int) { ss[i], ss[j] = ss[j], ss[i] }
func (ss sortableServices) Less(i, j int) bool {
	return strings.Compare(ss[i].GetName(), ss[j].GetName()) < 0
}
