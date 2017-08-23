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

package eureka

import (
	"fmt"
	"sort"
	"testing"

	"istio.io/pilot/model"
)

type mockClient []*application

func (apps *mockClient) Applications() ([]*application, error) {
	return *apps, nil
}

var _ Client = (*mockClient)(nil)

func TestServiceDiscoveryServices(t *testing.T) {
	cl := &mockClient{
		{
			Name: appName("a.default.svc.local"),
			Instances: []*instance{
				makeInstance("a.default.svc.local", "10.0.0.1", 9090, 8080, nil),
				makeInstance("b.default.svc.local", "10.0.0.2", 7070, -1, nil),
			},
		},
	}
	sd := NewServiceDiscovery(cl)
	expectedServices := []*model.Service{
		makeService("a.default.svc.local", []int{8080, 9090}, nil),
		makeService("b.default.svc.local", []int{7070}, nil),
	}

	services := sd.Services()
	sortServices(services)
	if err := compare(t, services, expectedServices); err != nil {
		t.Error(err)
	}
}

func TestServiceDiscoveryGetService(t *testing.T) {
	host := "hello.world.local"
	hostAlt := "foo.bar.local"
	hostDNE := "does.not.exist.local"

	cl := &mockClient{
		{
			Name: "APP",
			Instances: []*instance{
				makeInstance(host, "10.0.0.1", 9090, 8080, nil),
				makeInstance(hostAlt, "10.0.0.2", 7070, -1, nil),
			},
		},
	}
	sd := NewServiceDiscovery(cl)

	_, exists := sd.GetService(hostDNE)
	if exists {
		t.Errorf("GetService(%q) => %t, want false", hostDNE, exists)
	}

	service, exists := sd.GetService(host)
	if !exists {
		t.Errorf("GetService(%q) => %t, want true", host, exists)
	}
	if service.Hostname != host {
		t.Errorf("GetService(%q) => %q, want %q", host, service.Hostname, host)
	}
}

func TestServiceDiscoveryHostInstances(t *testing.T) {
	cl := &mockClient{
		{
			Name: appName("a.default.svc.local"),
			Instances: []*instance{
				makeInstance("a.default.svc.local", "10.0.0.1", 9090, -1, nil),
				makeInstance("a.default.svc.local", "10.0.0.2", 8080, -1, nil),
				makeInstance("b.default.svc.local", "10.0.0.1", 7070, -1, nil),
			},
		},
	}
	sd := NewServiceDiscovery(cl)

	serviceA := makeService("a.default.svc.local", []int{9090, 8080}, nil)
	serviceB := makeService("b.default.svc.local", []int{7070}, nil)

	instanceTests := []struct {
		addrs     map[string]bool
		instances []*model.ServiceInstance
	}{
		{
			addrs: map[string]bool{
				"10.0.0.1": true,
			},
			instances: []*model.ServiceInstance{
				makeServiceInstance(serviceA, "10.0.0.1", 9090, nil),
				makeServiceInstance(serviceB, "10.0.0.1", 7070, nil),
			},
		},
	}

	for _, tt := range instanceTests {
		instances := sd.HostInstances(tt.addrs)
		sortServiceInstances(instances)
		if err := compare(t, instances, tt.instances); err != nil {
			t.Error(err)
		}
	}
}

func TestServiceDiscoveryInstances(t *testing.T) {
	cl := &mockClient{
		{
			Name: appName("a.default.svc.local"),
			Instances: []*instance{
				makeInstance("a.default.svc.local", "10.0.0.1", 9090, -1, metadata{"spam": "coolaid"}),
				makeInstance("a.default.svc.local", "10.0.0.2", 8080, -1, metadata{"kit": "kat"}),
				makeInstance("b.default.svc.local", "10.0.0.1", 7070, -1, nil),
			},
		},
	}
	sd := NewServiceDiscovery(cl)
	serviceA := makeService("a.default.svc.local", []int{9090, 8080}, nil)
	serviceB := makeService("b.default.svc.local", []int{7070}, nil)
	spamCoolaidTags := model.Tags{"spam": "coolaid"}
	kitKatTags := model.Tags{"kit": "kat"}

	serviceInstanceTests := []struct {
		hostname  string
		ports     []string
		tags      model.TagsList
		instances []*model.ServiceInstance
	}{
		{
			// filter by hostname
			hostname: "a.default.svc.local",
			instances: []*model.ServiceInstance{
				makeServiceInstance(serviceA, "10.0.0.2", 8080, kitKatTags),
				makeServiceInstance(serviceA, "10.0.0.1", 9090, spamCoolaidTags),
			},
		},
		{
			// filter by hostname and tags
			hostname: "a.default.svc.local",
			tags:     model.TagsList{{"spam": "coolaid"}},
			instances: []*model.ServiceInstance{
				makeServiceInstance(serviceA, "10.0.0.1", 9090, spamCoolaidTags),
			},
		},
		{
			// filter by hostname and port
			hostname: "b.default.svc.local",
			ports:    []string{"7070"},
			instances: []*model.ServiceInstance{
				makeServiceInstance(serviceB, "10.0.0.1", 7070, nil),
			},
		},
	}

	for _, c := range serviceInstanceTests {
		instances := sd.Instances(c.hostname, c.ports, c.tags)
		sortServiceInstances(instances)
		if err := compare(t, instances, c.instances); err != nil {
			t.Error(err)
		}
	}
}

func sortServices(services []*model.Service) {
	sort.Slice(services, func(i, j int) bool { return services[i].Hostname < services[j].Hostname })
	for _, service := range services {
		sortPorts(service.Ports)
	}
}

func sortServiceInstances(instances []*model.ServiceInstance) {
	tagsToSlice := func(tags model.Tags) []string {
		out := make([]string, 0, len(tags))
		for k, v := range tags {
			out = append(out, fmt.Sprintf("%s=%s", k, v))
		}
		sort.Strings(out)
		return out
	}

	sort.Slice(instances, func(i, j int) bool {
		if instances[i].Service.Hostname == instances[j].Service.Hostname {
			if instances[i].Endpoint.Port == instances[j].Endpoint.Port {
				if instances[i].Endpoint.Address == instances[j].Endpoint.Address {
					if len(instances[i].Tags) == len(instances[j].Tags) {
						iTags := tagsToSlice(instances[i].Tags)
						jTags := tagsToSlice(instances[j].Tags)
						for k := range iTags {
							if iTags[k] < jTags[k] {
								return true
							}
						}
					}
					return len(instances[i].Tags) < len(instances[j].Tags)
				}
				return instances[i].Endpoint.Address < instances[j].Endpoint.Address
			}
			return instances[i].Endpoint.Port < instances[j].Endpoint.Port
		}
		return instances[i].Service.Hostname < instances[j].Service.Hostname
	})
}

func sortPorts(ports []*model.Port) {
	sort.Slice(ports, func(i, j int) bool {
		if ports[i].Port == ports[j].Port {
			if ports[i].Name == ports[j].Name {
				return ports[i].Protocol < ports[j].Protocol
			}
			return ports[i].Name < ports[j].Name
		}
		return ports[i].Port < ports[j].Port
	})
}
