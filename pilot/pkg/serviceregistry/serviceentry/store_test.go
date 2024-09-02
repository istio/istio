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
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/ptr"
	"istio.io/istio/pkg/test/util/assert"
	"istio.io/istio/pkg/util/sets"
)

func TestServiceInstancesStore(t *testing.T) {
	store := serviceInstancesStore{
		ip2instance:            map[string][]*model.ServiceInstance{},
		instances:              map[instancesKey]map[configKeyWithParent][]*model.ServiceInstance{},
		instancesBySE:          map[types.NamespacedName]map[configKey][]*model.ServiceInstance{},
		instancesByHostAndPort: sets.Set[hostPort]{},
	}
	instances := []*model.ServiceInstance{
		makeInstance(selector, []string{"1.1.1.1"}, 444, selector.Spec.(*networking.ServiceEntry).Ports[0], nil, PlainText),
		makeInstance(selector, []string{"1.1.1.1"}, 445, selector.Spec.(*networking.ServiceEntry).Ports[1], nil, PlainText),
		makeInstance(dnsSelector, []string{"1.1.1.1"}, 444, dnsSelector.Spec.(*networking.ServiceEntry).Ports[0], nil, PlainText),
		makeInstance(selector, []string{"1.1.1.1", "2001:1::1"}, 444, selector.Spec.(*networking.ServiceEntry).Ports[0], nil, PlainText),
		makeInstance(selector, []string{"1.1.1.1", "2001:1::1"}, 445, selector.Spec.(*networking.ServiceEntry).Ports[1], nil, PlainText),
	}
	cKey := configKey{
		namespace: "default",
		name:      "test-wle",
	}
	cpKey := configKeyWithParent{configKey: cKey, parent: config.NamespacedName(selector)}
	store.addInstances(cpKey, instances)

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
		makeInstance(selector, []string{"1.1.1.1"}, 444, selector.Spec.(*networking.ServiceEntry).Ports[0], nil, PlainText),
		makeInstance(selector, []string{"1.1.1.1"}, 445, selector.Spec.(*networking.ServiceEntry).Ports[1], nil, PlainText),
		makeInstance(selector, []string{"1.1.1.1", "2001:1::1"}, 444, selector.Spec.(*networking.ServiceEntry).Ports[0], nil, PlainText),
		makeInstance(selector, []string{"1.1.1.1", "2001:1::1"}, 445, selector.Spec.(*networking.ServiceEntry).Ports[1], nil, PlainText),
	}
	if !reflect.DeepEqual(gotInstances, expected) {
		t.Errorf("got unexpected instances : %v", gotInstances)
	}

	// 4. test getServiceEntryInstances
	expectedSeInstances := map[configKey][]*model.ServiceInstance{cKey: {
		makeInstance(selector, []string{"1.1.1.1"}, 444, selector.Spec.(*networking.ServiceEntry).Ports[0], nil, PlainText),
		makeInstance(selector, []string{"1.1.1.1"}, 445, selector.Spec.(*networking.ServiceEntry).Ports[1], nil, PlainText),
		makeInstance(selector, []string{"1.1.1.1", "2001:1::1"}, 444, selector.Spec.(*networking.ServiceEntry).Ports[0], nil, PlainText),
		makeInstance(selector, []string{"1.1.1.1", "2001:1::1"}, 445, selector.Spec.(*networking.ServiceEntry).Ports[1], nil, PlainText),
	}}
	key := selector.NamespacedName()
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

	// 7. test deleteInstanceKeys
	store.deleteInstanceKeys(cpKey, instances)
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
		makeService(
			"*.istio.io", "httpDNSRR", "httpDNSRR", []string{constants.UnspecifiedIP},
			"", "", map[string]int{"http-port": 80, "http-alt-port": 8080}, true, model.DNSRoundRobinLB),
		makeService("*.istio.io", "httpDNSRR", "httpDNSRR", []string{constants.UnspecifiedIP},
			"", "", map[string]int{"http-port": 80, "http-alt-port": 8080}, true, model.DNSLB),
	}

	store.updateServices(httpDNSRR.NamespacedName(), expectedServices)
	got := store.getServices(httpDNSRR.NamespacedName())
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
	store.deleteServices(httpDNSRR.NamespacedName())
	got = store.getAllServices()
	if len(got) != 0 {
		t.Errorf("got unexpected services %v", got)
	}
	if store.allocateNeeded {
		t.Errorf("expected no allocate needed")
	}
}

// Tests that when multiple service entries with "DNSRounbRobinLB" resolution type
// are created with different/same endpoints, we only consider the first service because
// Envoy's LogicalDNS type of cluster does not allow more than one locality LB Endpoint.
func TestServiceInstancesForDnsRoundRobinLB(t *testing.T) {
	otherNs := ptr.Of(dnsRoundRobinLBSE1.DeepCopy())
	otherNs.Namespace = "other"
	store := serviceInstancesStore{
		ip2instance:            map[string][]*model.ServiceInstance{},
		instances:              map[instancesKey]map[configKeyWithParent][]*model.ServiceInstance{},
		instancesBySE:          map[types.NamespacedName]map[configKey][]*model.ServiceInstance{},
		instancesByHostAndPort: sets.Set[hostPort]{},
	}
	instances := []*model.ServiceInstance{
		makeInstance(dnsRoundRobinLBSE1, []string{"1.1.1.1"}, 444, dnsRoundRobinLBSE1.Spec.(*networking.ServiceEntry).Ports[0], nil, PlainText),
		makeInstance(dnsRoundRobinLBSE1, []string{"1.1.1.1"}, 445, dnsRoundRobinLBSE1.Spec.(*networking.ServiceEntry).Ports[1], nil, PlainText),
		makeInstance(dnsRoundRobinLBSE3, []string{"3.3.3.3", "2001:1::3"}, 444, dnsRoundRobinLBSE3.Spec.(*networking.ServiceEntry).Ports[0], nil, PlainText),
	}
	cKey := configKey{
		namespace: "dns",
		name:      "dns-round-robin-1",
	}
	cpKey := configKeyWithParent{configKey: cKey, parent: config.NamespacedName(selector)}
	// Add instance related to first Service Entry and validate they are added correctly.
	store.addInstances(cpKey, instances)

	expected := instances
	gotInstances := []*model.ServiceInstance{}
	gotInstances = append(gotInstances, store.getByKey(instancesKey{
		hostname:  "example.com",
		namespace: "dns",
	})...)
	gotInstances = append(gotInstances, store.getByKey(instancesKey{
		hostname:  "muladdrs.example.com",
		namespace: "dns",
	})...)

	assert.Equal(t, gotInstances, expected)

	store.addInstances(
		configKeyWithParent{
			configKey: configKey{namespace: otherNs.Namespace, name: otherNs.Name},
		},
		[]*model.ServiceInstance{
			makeInstance(otherNs, []string{"1.1.1.1"}, 444, otherNs.Spec.(*networking.ServiceEntry).Ports[0], nil, PlainText),
			makeInstance(otherNs, []string{"1.1.1.1"}, 445, otherNs.Spec.(*networking.ServiceEntry).Ports[1], nil, PlainText),
		},
	)

	expected = []*model.ServiceInstance{
		makeInstance(dnsRoundRobinLBSE1, []string{"1.1.1.1"}, 444, dnsRoundRobinLBSE1.Spec.(*networking.ServiceEntry).Ports[0], nil, PlainText),
		makeInstance(dnsRoundRobinLBSE1, []string{"1.1.1.1"}, 445, dnsRoundRobinLBSE1.Spec.(*networking.ServiceEntry).Ports[1], nil, PlainText),
	}

	assert.Equal(t, store.getByKey(instancesKey{
		hostname:  "example.com",
		namespace: "dns",
	}), expected)

	otherNsExpected := []*model.ServiceInstance{
		makeInstance(otherNs, []string{"1.1.1.1"}, 444, otherNs.Spec.(*networking.ServiceEntry).Ports[0], nil, PlainText),
		makeInstance(otherNs, []string{"1.1.1.1"}, 445, otherNs.Spec.(*networking.ServiceEntry).Ports[1], nil, PlainText),
	}
	assert.Equal(t, store.getByKey(instancesKey{
		hostname:  "example.com",
		namespace: otherNs.Namespace,
	}), otherNsExpected)

	// Add instance related to second Service Entry and validate it is ignored.
	instances = []*model.ServiceInstance{
		makeInstance(dnsRoundRobinLBSE2, []string{"2.2.2.2"}, 444, dnsRoundRobinLBSE2.Spec.(*networking.ServiceEntry).Ports[0], nil, PlainText),
	}
	cpKey = configKeyWithParent{
		configKey: configKey{
			namespace: "dns",
			name:      "dns-round-robin-2",
		},
	}
	store.addInstances(cpKey, instances)

	assert.Equal(t, store.getByKey(instancesKey{
		hostname:  "example.com",
		namespace: "dns",
	}), expected)
	assert.Equal(t, store.getByKey(instancesKey{
		hostname:  "example.com",
		namespace: otherNs.Namespace,
	}), otherNsExpected)
}

func TestUpdateInstances(t *testing.T) {
	store := serviceInstancesStore{
		ip2instance:            map[string][]*model.ServiceInstance{},
		instances:              map[instancesKey]map[configKeyWithParent][]*model.ServiceInstance{},
		instancesBySE:          map[types.NamespacedName]map[configKey][]*model.ServiceInstance{},
		instancesByHostAndPort: sets.Set[hostPort]{},
	}
	expectedInstances := []*model.ServiceInstance{
		makeInstance(selector, []string{"1.1.1.1"}, 444, selector.Spec.(*networking.ServiceEntry).Ports[0], nil, PlainText),
	}
	cKey := configKey{
		namespace: "default",
		name:      "test-wle",
	}
	cpKey := configKeyWithParent{configKey: cKey, parent: config.NamespacedName(selector)}
	store.addInstances(cpKey, expectedInstances)
	gotInstances := store.getAll()
	if !reflect.DeepEqual(gotInstances, expectedInstances) {
		t.Errorf("got unexpected instances : %v", gotInstances)
	}

	// Verify that updates to service instances are correctly applied
	expectedSeInstances := []*model.ServiceInstance{
		makeInstance(selector, []string{"1.1.1.1", "2001:1::2"}, 444, selector.Spec.(*networking.ServiceEntry).Ports[0], nil, PlainText),
	}
	store.updateInstances(cpKey, expectedSeInstances)

	gotSeInstances := store.getAll()
	if !reflect.DeepEqual(gotSeInstances, expectedSeInstances) {
		t.Errorf("got unexpected se instances : %v", gotSeInstances)
	}
}
