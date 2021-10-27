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
	"istio.io/istio/pkg/config/labels"
	"istio.io/istio/pkg/spiffe"
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

func TestWorkloadInstancesStore(t *testing.T) {
	// Setup a couple of workload instances for test. These will be selected by the `selector` SE
	wi1 := &model.WorkloadInstance{
		Name:      selector.Name,
		Namespace: selector.Namespace,
		Endpoint: &model.IstioEndpoint{
			Address:        "2.2.2.2",
			Labels:         map[string]string{"app": "wle"},
			ServiceAccount: spiffe.MustGenSpiffeURI(selector.Name, "default"),
			TLSMode:        model.IstioMutualTLSModeLabel,
		},
	}

	wi2 := &model.WorkloadInstance{
		Name:      "some-other-name",
		Namespace: selector.Namespace,
		Endpoint: &model.IstioEndpoint{
			Address:        "3.3.3.3",
			Labels:         map[string]string{"app": "wle"},
			ServiceAccount: spiffe.MustGenSpiffeURI(selector.Name, "default"),
			TLSMode:        model.IstioMutualTLSModeLabel,
		},
	}

	wi3 := &model.WorkloadInstance{
		Name:      "another-name",
		Namespace: dnsSelector.Namespace,
		Endpoint: &model.IstioEndpoint{
			Address:        "2.2.2.2",
			Labels:         map[string]string{"app": "dns-wle"},
			ServiceAccount: spiffe.MustGenSpiffeURI(dnsSelector.Name, "default"),
			TLSMode:        model.IstioMutualTLSModeLabel,
		},
	}
	store := workloadInstancesStore{
		instancesByKey: map[string]*model.WorkloadInstance{},
	}

	// test update
	store.update(wi1)
	store.update(wi2)
	store.update(wi3)
	// test get
	got := store.get(keyFunc(wi1.Namespace, wi1.Name))
	if !reflect.DeepEqual(got, wi1) {
		t.Errorf("got unexpected workloadinstance %v", got)
	}
	workloadinstances := store.list(selector.Namespace, labels.Collection{{"app": "wle"}})
	expected := map[string]*model.WorkloadInstance{
		wi1.Name: wi1,
		wi2.Name: wi2,
	}
	if len(workloadinstances) != 2 {
		t.Errorf("got unexpected workload instance %v", workloadinstances)
	}
	for _, wi := range workloadinstances {
		if !reflect.DeepEqual(expected[wi.Name], wi) {
			t.Errorf("got unexpected workload instance %v", wi)
		}
	}

	store.delete(keyFunc(wi1.Namespace, wi1.Name))
	got = store.get(keyFunc(wi1.Namespace, wi1.Name))
	if got != nil {
		t.Errorf("workloadInstance %v was not deleted", got)
	}
}

func TestServiceStore(t *testing.T) {
	store := serviceStore{
		servicesBySE:      map[types.NamespacedName][]*model.Service{},
		seByWorkloadEntry: map[configKey][]types.NamespacedName{},
	}

	expectedServices := []*model.Service{
		makeService("*.istio.io", "httpDNSRR", constants.UnspecifiedIP, map[string]int{"http-port": 80, "http-alt-port": 8080}, true, model.DNSRoundRobinLB),
		makeService("*.istio.io", "httpDNSRR", constants.UnspecifiedIP, map[string]int{"http-port": 80, "http-alt-port": 8080}, true, model.DNSLB),
	}

	store.updateServices(types.NamespacedName{Namespace: httpDNSRR.Namespace, Name: httpDNSRR.Name}, expectedServices)
	got := store.getServices(types.NamespacedName{Namespace: httpDNSRR.Namespace, Name: httpDNSRR.Name})
	if !reflect.DeepEqual(got, expectedServices) {
		t.Fatalf("got unexpected services %v", got)
	}

	got = store.getAllServices()
	if !reflect.DeepEqual(got, expectedServices) {
		t.Fatalf("got unexpected services %v", got)
	}
	store.deleteServices(types.NamespacedName{Namespace: httpDNSRR.Namespace, Name: httpDNSRR.Name})
	got = store.getAllServices()
	if got != nil {
		t.Fatalf("got unexpected services %v", got)
	}

	cKey := configKey{
		kind:      workloadEntryConfigType,
		name:      "test-wle",
		namespace: "default",
	}
	expectedSes := []types.NamespacedName{{Name: "se-1", Namespace: "default"}, {Name: "se-2", Namespace: "default"}}
	store.updateServiceEntry(cKey, expectedSes)
	gotSes := store.getServiceEntries(cKey)
	if !reflect.DeepEqual(gotSes, expectedSes) {
		t.Errorf("got unexpected service entries %v", gotSes)
	}
	store.deleteServiceEntry(cKey)
	gotSes = store.getServiceEntries(cKey)
	if gotSes != nil {
		t.Errorf("got unexpected service entries %v", gotSes)
	}
}
