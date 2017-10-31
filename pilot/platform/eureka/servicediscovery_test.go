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
	"errors"
	"fmt"
	"sort"
	"testing"

	"istio.io/istio/pilot/model"
)

type mockClient struct {
	apps      []*application
	clientErr error
}

func (m *mockClient) Applications() ([]*application, error) {
	return m.apps, m.clientErr
}

var _ Client = (*mockClient)(nil)

func TestServiceDiscoveryServices(t *testing.T) {
	cl := &mockClient{
		apps: []*application{
			{
				Name: appName("a.default.svc.local"),
				Instances: []*instance{
					makeInstance("a.default.svc.local", "10.0.0.1", 9090, 8080, nil),
					makeInstance("b.default.svc.local", "10.0.0.2", 7070, -1, nil),
				},
			},
		},
	}
	sd := NewServiceDiscovery(cl)
	expectedServices := []*model.Service{
		makeService("a.default.svc.local", []int{8080, 9090}, nil),
		makeService("b.default.svc.local", []int{7070}, nil),
	}

	services, err := sd.Services()
	if err != nil {
		t.Errorf("Services() encountered unexpected error: %v", err)
	}
	sortServices(services)
	if err := compare(t, services, expectedServices); err != nil {
		t.Error(err)
	}
}

func TestServiceDiscoveryClientError(t *testing.T) {
	cl := &mockClient{
		clientErr: errors.New("client Applications() error"),
	}
	sd := NewServiceDiscovery(cl)

	services, err := sd.Services()
	if err == nil {
		t.Error("Services() should return error")
	}
	if services != nil {
		t.Error("Services() should return nil on error")
	}

	service, err := sd.GetService("hostname")
	if err == nil {
		t.Error("GetService() should return error")
	}
	if service != nil {
		t.Error("GetService() should return nil on error")
	}

	instances, err := sd.Instances("hostname", nil, nil)
	if err == nil {
		t.Error("Instances() should return error")
	}
	if instances != nil {
		t.Error("Instances() should return nil on error")
	}

	hostInstances, err := sd.HostInstances(make(map[string]bool))
	if err == nil {
		t.Error("HostInstances() should return error")
	}
	if hostInstances != nil {
		t.Error("HostInstances() should return nil on error")
	}
}

func TestServiceDiscoveryGetService(t *testing.T) {
	host := "hello.world.local"
	hostAlt := "foo.bar.local"
	hostDNE := "does.not.exist.local"

	cl := &mockClient{
		apps: []*application{
			{
				Name: "APP",
				Instances: []*instance{
					makeInstance(host, "10.0.0.1", 9090, 8080, nil),
					makeInstance(hostAlt, "10.0.0.2", 7070, -1, nil),
				},
			},
		},
	}
	sd := NewServiceDiscovery(cl)

	service, err := sd.GetService(hostDNE)
	if err != nil {
		t.Errorf("GetService() encountered unexpected error: %v", err)
	}
	if service != nil {
		t.Errorf("GetService(%q) => should not exist, got %s", hostDNE, service.Hostname)
	}

	service, err = sd.GetService(host)
	if err != nil {
		t.Errorf("GetService(%q) encountered unexpected error: %v", host, err)
	}
	if service == nil {
		t.Errorf("GetService(%q) => should exist", host)
	}
	if service.Hostname != host {
		t.Errorf("GetService(%q) => %q, want %q", host, service.Hostname, host)
	}
}

func TestServiceDiscoveryHostInstances(t *testing.T) {
	cl := &mockClient{
		apps: []*application{
			{
				Name: appName("a.default.svc.local"),
				Instances: []*instance{
					makeInstance("a.default.svc.local", "10.0.0.1", 9090, -1, nil),
					makeInstance("a.default.svc.local", "10.0.0.2", 8080, -1, nil),
					makeInstance("b.default.svc.local", "10.0.0.1", 7070, -1, nil),
				},
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
		instances, err := sd.HostInstances(tt.addrs)
		if err != nil {
			t.Errorf("HostInstances() encountered unexpected error: %v", err)
		}
		sortServiceInstances(instances)
		if err := compare(t, instances, tt.instances); err != nil {
			t.Error(err)
		}
	}
}

func TestServiceDiscoveryInstances(t *testing.T) {
	cl := &mockClient{
		apps: []*application{
			{
				Name: appName("a.default.svc.local"),
				Instances: []*instance{
					makeInstance("a.default.svc.local", "10.0.0.1", 9090, -1, metadata{"spam": "coolaid"}),
					makeInstance("a.default.svc.local", "10.0.0.2", 8080, -1, metadata{"kit": "kat"}),
					makeInstance("b.default.svc.local", "10.0.0.1", 7070, -1, nil),
				},
			},
		},
	}
	sd := NewServiceDiscovery(cl)
	serviceA := makeService("a.default.svc.local", []int{9090, 8080}, nil)
	serviceB := makeService("b.default.svc.local", []int{7070}, nil)
	spamCoolaidLabels := model.Labels{"spam": "coolaid"}
	kitKatLabels := model.Labels{"kit": "kat"}

	serviceInstanceTests := []struct {
		hostname  string
		ports     []string
		labels    model.LabelsCollection
		instances []*model.ServiceInstance
	}{
		{
			// filter by hostname
			hostname: "a.default.svc.local",
			instances: []*model.ServiceInstance{
				makeServiceInstance(serviceA, "10.0.0.2", 8080, kitKatLabels),
				makeServiceInstance(serviceA, "10.0.0.1", 9090, spamCoolaidLabels),
			},
		},
		{
			// filter by hostname and labels
			hostname: "a.default.svc.local",
			labels:   model.LabelsCollection{{"spam": "coolaid"}},
			instances: []*model.ServiceInstance{
				makeServiceInstance(serviceA, "10.0.0.1", 9090, spamCoolaidLabels),
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
		instances, err := sd.Instances(c.hostname, c.ports, c.labels)
		if err != nil {
			t.Errorf("Instances() encountered unexpected error: %v", err)
		}
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
	labelsToSlice := func(labels model.Labels) []string {
		out := make([]string, 0, len(labels))
		for k, v := range labels {
			out = append(out, fmt.Sprintf("%s=%s", k, v))
		}
		sort.Strings(out)
		return out
	}

	sort.Slice(instances, func(i, j int) bool {
		if instances[i].Service.Hostname == instances[j].Service.Hostname {
			if instances[i].Endpoint.Port == instances[j].Endpoint.Port {
				if instances[i].Endpoint.Address == instances[j].Endpoint.Address {
					if len(instances[i].Labels) == len(instances[j].Labels) {
						iLabels := labelsToSlice(instances[i].Labels)
						jLabels := labelsToSlice(instances[j].Labels)
						for k := range iLabels {
							if iLabels[k] < jLabels[k] {
								return true
							}
						}
					}
					return len(instances[i].Labels) < len(instances[j].Labels)
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
