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

package serviceentry

import (
	"reflect"
	"testing"

	"k8s.io/apimachinery/pkg/types"

	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/config/constants"
)

func TestServiceInstancesStore(t *testing.T) {
	store := serviceInstancesStore{
		ip2instance:   map[string][]*model.ServiceInstance{},
		instances:     map[instancesKey]map[configKey][]*model.ServiceInstance{},
		instancesBySE: map[types.NamespacedName]map[configKey][]*model.ServiceInstance{},
	}
	instances := []*model.ServiceInstance{
		makeInstance(selector, "1.1.1.1", 444, selector.Spec.(*networking.ServiceEntry).Ports[0], nil, PlainText),
		makeInstance(selector, "1.1.1.1", 445, selector.Spec.(*networking.ServiceEntry).Ports[1], nil, PlainText),
		makeInstance(dnsSelector, "1.1.1.1", 444, dnsSelector.Spec.(*networking.ServiceEntry).Ports[0], nil, PlainText),
	}
	cKey := configKey{
		namespace: "default",
		name:      "test-wle",
	}
	store.addInstances(cKey, instances)

	// 1. test getByIP
	gotInstances := store.getByIP("1.1.1.1")
	if !reflect.DeepEqual(instances, gotInstances) {
		t.Errorf("got unexpected instances : %v", gotInstances)
	}

	// 2. test getAll
	gotInstances = store.getAll()
	if !reflect.DeepEqual(instances, gotInstances) {
		t.Errorf("got unexpected instances : %v", gotInstances)
	}

	// 3. test getByKey
	gotInstances = store.getByKey(instancesKey{
		hostname:  "selector.com",
		namespace: "selector",
	})
	expected := []*model.ServiceInstance{
		makeInstance(selector, "1.1.1.1", 444, selector.Spec.(*networking.ServiceEntry).Ports[0], nil, PlainText),
		makeInstance(selector, "1.1.1.1", 445, selector.Spec.(*networking.ServiceEntry).Ports[1], nil, PlainText),
	}
	if !reflect.DeepEqual(gotInstances, expected) {
		t.Errorf("got unexpected instances : %v", gotInstances)
	}

	// 4. test getServiceEntryInstances
	expectedSeInstances := map[configKey][]*model.ServiceInstance{cKey: {
		makeInstance(selector, "1.1.1.1", 444, selector.Spec.(*networking.ServiceEntry).Ports[0], nil, PlainText),
		makeInstance(selector, "1.1.1.1", 445, selector.Spec.(*networking.ServiceEntry).Ports[1], nil, PlainText),
	}}
	key := types.NamespacedName{Namespace: selector.Namespace, Name: selector.Name}
	store.updateServiceEntryInstances(key, expectedSeInstances)

	gotSeInstances := store.getServiceEntryInstances(key)
	if !reflect.DeepEqual(gotSeInstances, expectedSeInstances) {
		t.Errorf("got unexpected se instances : %v", gotSeInstances)
	}

	// 5. test deleteServiceEntryInstances
	store.deleteServiceEntryInstances(key, cKey)
	gotSeInstances = store.getServiceEntryInstances(key)
	if len(gotSeInstances) != 0 {
		t.Errorf("got unexpected instances %v", gotSeInstances)
	}

	// 6. test deleteAllServiceEntryInstances
	store.deleteAllServiceEntryInstances(key)
	gotSeInstances = store.getServiceEntryInstances(key)
	if len(gotSeInstances) != 0 {
		t.Errorf("got unexpected instances %v", gotSeInstances)
	}

	// 7. test deleteInstances
	store.deleteInstances(cKey, instances)
	gotInstances = store.getAll()
	if len(gotInstances) != 0 {
		t.Errorf("got unexpected instances %v", gotSeInstances)
	}
}

func TestServiceStore(t *testing.T) {
	store := serviceStore{
		servicesBySE: map[types.NamespacedName][]*model.Service{},
	}

	expectedServices := []*model.Service{
		makeService("*.istio.io", "httpDNSRR", constants.UnspecifiedIP, map[string]int{"http-port": 80, "http-alt-port": 8080}, true, model.DNSRoundRobinLB),
		makeService("*.istio.io", "httpDNSRR", constants.UnspecifiedIP, map[string]int{"http-port": 80, "http-alt-port": 8080}, true, model.DNSLB),
	}

	store.updateServices(types.NamespacedName{Namespace: httpDNSRR.Namespace, Name: httpDNSRR.Name}, expectedServices)
	got := store.getServices(types.NamespacedName{Namespace: httpDNSRR.Namespace, Name: httpDNSRR.Name})
	if !reflect.DeepEqual(got, expectedServices) {
		t.Errorf("got unexpected services %v", got)
	}

	got = store.getAllServices()
	if !reflect.DeepEqual(got, expectedServices) {
		t.Errorf("got unexpected services %v", got)
	}
	if !store.allocateNeeded {
		t.Errorf("expected allocate needed")
	}
	store.allocateNeeded = false
	store.deleteServices(types.NamespacedName{Namespace: httpDNSRR.Namespace, Name: httpDNSRR.Name})
	got = store.getAllServices()
	if got != nil {
		t.Errorf("got unexpected services %v", got)
	}
	if store.allocateNeeded {
		t.Errorf("expected no allocate needed")
	}
}
