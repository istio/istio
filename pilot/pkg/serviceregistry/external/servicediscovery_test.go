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

package external

import (
	"fmt"
	"reflect"
	"sort"
	"testing"

	"time"

	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/config/memory"
	"istio.io/istio/pilot/pkg/model"
)

func createServiceEntries(serviceEntries []*networking.ServiceEntry, store model.IstioConfigStore, t *testing.T, creationTime time.Time) {
	t.Helper()
	for _, svc := range serviceEntries {
		config := model.Config{
			ConfigMeta: model.ConfigMeta{
				Type:              model.ServiceEntry.Type,
				Name:              svc.Hosts[0],
				Namespace:         "default",
				Domain:            "cluster.local",
				CreationTimestamp: meta_v1.Time{creationTime},
			},
			Spec: svc,
		}

		_, err := store.Create(config)
		if err != nil {
			t.Errorf("error occurred crearting ServiceEntry config: %v", err)
		}
	}
}

type channelTerminal struct {
}

func initServiceDiscovery() (model.IstioConfigStore, *ServiceEntryStore, func()) {
	store := memory.Make(model.IstioConfigTypes)
	configController := memory.NewController(store)

	stop := make(chan struct{})
	go configController.Run(stop)

	istioStore := model.MakeIstioStore(configController)
	serviceController := NewServiceDiscovery(configController, istioStore)
	return istioStore, serviceController, func() {
		stop <- channelTerminal{}
	}
}

func TestServiceDiscoveryServices(t *testing.T) {
	store, sd, stopFn := initServiceDiscovery()
	defer stopFn()

	tnow := time.Now()
	expectedServices := []*model.Service{
		makeService("*.google.com", model.UnspecifiedIP, map[string]int{"http-port": 80, "http-alt-port": 8080}, true, model.DNSLB, tnow),
		makeService("tcpstatic.com", "172.217.0.1", map[string]int{"tcp-444": 444}, true, model.ClientSideLB, tnow),
	}

	createServiceEntries([]*networking.ServiceEntry{httpDNS, tcpStatic}, store, t, tnow)

	services, err := sd.Services()
	if err != nil {
		t.Errorf("Services() encountered unexpected error: %v", err)
	}
	sortServices(services)
	sortServices(expectedServices)
	if err := compare(t, services, expectedServices); err != nil {
		t.Error(err)
	}
}

func TestServiceDiscoveryGetService(t *testing.T) {
	host := "*.google.com"
	hostDNE := "does.not.exist.local"

	store, sd, stopFn := initServiceDiscovery()
	defer stopFn()

	tnow := time.Now()
	createServiceEntries([]*networking.ServiceEntry{httpDNS, tcpStatic}, store, t, tnow)

	service, err := sd.GetService(model.Hostname(hostDNE))
	if err != nil {
		t.Errorf("GetService() encountered unexpected error: %v", err)
	}
	if service != nil {
		t.Errorf("GetService(%q) => should not exist, got %s", hostDNE, service.Hostname)
	}

	service, err = sd.GetService(model.Hostname(host))
	if err != nil {
		t.Errorf("GetService(%q) encountered unexpected error: %v", host, err)
	}
	if service == nil {
		t.Errorf("GetService(%q) => should exist", host)
	}
	if service.Hostname != model.Hostname(host) {
		t.Errorf("GetService(%q) => %q, want %q", host, service.Hostname, host)
	}
}

func TestServiceDiscoveryGetServiceAttributes(t *testing.T) {
	store, sd, stopFn := initServiceDiscovery()
	defer stopFn()

	tnow := time.Now()
	createServiceEntries([]*networking.ServiceEntry{httpDNS, tcpStatic}, store, t, tnow)

	tcpStaticSvc := convertServices(tcpStatic, tnow)
	for _, svc := range tcpStaticSvc {
		expect := model.ServiceAttributes{
			Name:      svc.Hostname.String(),
			Namespace: model.IstioDefaultConfigNamespace,
		}
		if attr, _ := sd.GetServiceAttributes(svc.Hostname); !reflect.DeepEqual(*attr, expect) {
			t.Errorf("GetServiceAttributes(%q) => %v, want %v", svc.Hostname, *attr, expect)
		}
	}
	tcpDNSSvc := convertServices(tcpDNS, tnow)
	for _, svc := range tcpDNSSvc {
		if attr, _ := sd.GetServiceAttributes(svc.Hostname); attr != nil {
			t.Errorf(`GetServiceAttributes("tcpDNSSvc") => %q, want nil`, attr.Namespace)
		}
	}
}

func TestServiceDiscoveryGetProxyServiceInstances(t *testing.T) {
	store, sd, stopFn := initServiceDiscovery()
	defer stopFn()

	tnow := time.Now()
	createServiceEntries([]*networking.ServiceEntry{httpStatic, tcpStatic}, store, t, tnow)

	_, err := sd.GetProxyServiceInstances(&model.Proxy{IPAddress: "2.2.2.2"})
	if err != nil {
		t.Errorf("GetProxyServiceInstances() encountered unexpected error: %v", err)
	}
}

// Keeping this test for legacy - but it never happens in real life.
func TestServiceDiscoveryInstances(t *testing.T) {
	store, sd, stopFn := initServiceDiscovery()
	defer stopFn()

	tnow := time.Now()
	createServiceEntries([]*networking.ServiceEntry{httpDNS, tcpStatic}, store, t, tnow)

	expectedInstances := []*model.ServiceInstance{
		makeInstance(httpDNS, "us.google.com", 7080, httpDNS.Ports[0], nil, tnow),
		makeInstance(httpDNS, "us.google.com", 18080, httpDNS.Ports[1], nil, tnow),
		makeInstance(httpDNS, "uk.google.com", 1080, httpDNS.Ports[0], nil, tnow),
		makeInstance(httpDNS, "uk.google.com", 8080, httpDNS.Ports[1], nil, tnow),
		makeInstance(httpDNS, "de.google.com", 80, httpDNS.Ports[0], map[string]string{"foo": "bar"}, tnow),
		makeInstance(httpDNS, "de.google.com", 8080, httpDNS.Ports[1], map[string]string{"foo": "bar"}, tnow),
	}

	instances, err := sd.InstancesByPort("*.google.com", 0, nil)
	if err != nil {
		t.Errorf("Instances() encountered unexpected error: %v", err)
	}
	sortServiceInstances(instances)
	sortServiceInstances(expectedInstances)
	if err := compare(t, instances, expectedInstances); err != nil {
		t.Error(err)
	}
}

// Keeping this test for legacy - but it never happens in real life.
func TestServiceDiscoveryInstances1Port(t *testing.T) {
	store, sd, stopFn := initServiceDiscovery()
	defer stopFn()

	tnow := time.Now()
	createServiceEntries([]*networking.ServiceEntry{httpDNS, tcpStatic}, store, t, tnow)

	expectedInstances := []*model.ServiceInstance{
		makeInstance(httpDNS, "us.google.com", 7080, httpDNS.Ports[0], nil, tnow),
		makeInstance(httpDNS, "uk.google.com", 1080, httpDNS.Ports[0], nil, tnow),
		makeInstance(httpDNS, "de.google.com", 80, httpDNS.Ports[0], map[string]string{"foo": "bar"}, tnow),
	}

	instances, err := sd.InstancesByPort("*.google.com", 80, nil)
	if err != nil {
		t.Errorf("Instances() encountered unexpected error: %v", err)
	}
	sortServiceInstances(instances)
	sortServiceInstances(expectedInstances)
	if err := compare(t, instances, expectedInstances); err != nil {
		t.Error(err)
	}
}

func TestNonServiceConfig(t *testing.T) {
	store, sd, stopFn := initServiceDiscovery()
	defer stopFn()

	// Create a non-service configuration element. This should not affect the service registry at all.
	tnow := time.Now()
	config := model.Config{
		ConfigMeta: model.ConfigMeta{
			Type:              model.DestinationRule.Type,
			Name:              "fakeDestinationRule",
			Namespace:         "default",
			Domain:            "cluster.local",
			CreationTimestamp: meta_v1.Time{tnow},
		},
		Spec: &networking.DestinationRule{
			Host: "fakehost",
		},
	}
	_, err := store.Create(config)
	if err != nil {
		t.Errorf("error occurred crearting ServiceEntry config: %v", err)
	}

	// Now create some service entries and verify that it's added to the registry.
	createServiceEntries([]*networking.ServiceEntry{httpDNS, tcpStatic}, store, t, tnow)
	expectedInstances := []*model.ServiceInstance{
		makeInstance(httpDNS, "us.google.com", 7080, httpDNS.Ports[0], nil, tnow),
		makeInstance(httpDNS, "uk.google.com", 1080, httpDNS.Ports[0], nil, tnow),
		makeInstance(httpDNS, "de.google.com", 80, httpDNS.Ports[0], map[string]string{"foo": "bar"}, tnow),
	}
	instances, err := sd.InstancesByPort("*.google.com", 80, nil)
	if err != nil {
		t.Errorf("Instances() encountered unexpected error: %v", err)
	}
	sortServiceInstances(instances)
	sortServiceInstances(expectedInstances)
	if err := compare(t, instances, expectedInstances); err != nil {
		t.Error(err)
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
