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

package consul

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/hashicorp/consul/api"

	"istio.io/pilot/model"
)

var (
	services = map[string][]string{
		"productpage": {"version|v1"},
		"reviews":     {"version|v1", "version|v2", "version|v3"},
	}
	productpage = []*api.CatalogService{
		{
			Node:           "istio",
			Address:        "172.19.0.5",
			ServiceID:      "111-111-111",
			ServiceName:    "productpage",
			ServiceTags:    []string{"version|v1"},
			ServiceAddress: "172.19.0.11",
			ServicePort:    9080,
		},
	}
	reviews = []*api.CatalogService{
		{
			Node:           "istio",
			Address:        "172.19.0.5",
			ServiceID:      "222-222-222",
			ServiceName:    "reviews",
			ServiceTags:    []string{"version|v1"},
			ServiceAddress: "172.19.0.6",
			ServicePort:    9080,
		},
		{
			Node:           "istio",
			Address:        "172.19.0.5",
			ServiceID:      "333-333-333",
			ServiceName:    "reviews",
			ServiceTags:    []string{"version|v2"},
			ServiceAddress: "172.19.0.7",
			ServicePort:    9080,
		},
		{
			Node:           "istio",
			Address:        "172.19.0.5",
			ServiceID:      "444-444-444",
			ServiceName:    "reviews",
			ServiceTags:    []string{"version|v3"},
			ServiceAddress: "172.19.0.8",
			ServicePort:    9080,
			NodeMeta:       map[string]string{protocolTagName: "tcp"},
		},
	}
)

func newServer() *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/catalog/services" {
			data, _ := json.Marshal(&services)
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprintln(w, string(data))
		} else if r.URL.Path == "/v1/catalog/service/reviews" {
			data, _ := json.Marshal(&reviews)
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprintln(w, string(data))
		} else if r.URL.Path == "/v1/catalog/service/productpage" {
			data, _ := json.Marshal(&productpage)
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprintln(w, string(data))
		} else {
			fmt.Println(r.URL.Path)
		}
	}))
}

func TestInstances(t *testing.T) {
	ts := newServer()
	defer ts.Close()
	controller, err := NewController(ts.URL, "datacenter", 3*time.Second)
	if err != nil {
		t.Errorf("could not create Consul Controller: %v", err)
	}

	hostname := serviceHostname("reviews")
	instances := controller.Instances(hostname, []string{}, model.LabelsCollection{})
	if len(instances) != 3 {
		t.Errorf("Instances() returned wrong # of service instances => %q, want 3", len(instances))
	}
	for _, inst := range instances {
		if inst.Service.Hostname != hostname {
			t.Errorf("Instances() returned wrong service instance => %v, want %q",
				inst.Service.Hostname, hostname)
		}
	}

	filterTagKey := "version"
	filterTagVal := "v3"
	instances = controller.Instances(hostname, []string{}, model.LabelsCollection{
		model.Labels{filterTagKey: filterTagVal},
	})
	if len(instances) != 1 {
		t.Errorf("Instances() did not filter by tags => %q, want 1", len(instances))
	}
	for _, inst := range instances {
		found := false
		for key, val := range inst.Labels {
			if key == filterTagKey && val == filterTagVal {
				found = true
			}
		}
		if !found {
			t.Errorf("Instances() did not match by tag => %q, want tag {%q:%q}",
				inst, filterTagKey, filterTagVal)
		}
	}

	filterPort := "http"
	instances = controller.Instances(hostname, []string{filterPort}, model.LabelsCollection{})
	if len(instances) != 2 {
		t.Errorf("Instances() did not filter by port => %q, want 2", len(instances))
	}
	for _, inst := range instances {
		if inst.Endpoint.ServicePort.Name != filterPort {
			t.Errorf("Instances() did not filter by port => %q, want %q",
				inst.Endpoint.ServicePort.Name, filterPort)
		}
	}
}

func TestGetService(t *testing.T) {
	ts := newServer()
	defer ts.Close()
	controller, err := NewController(ts.URL, "datacenter", 3*time.Second)
	if err != nil {
		t.Errorf("could not create Consul Controller: %v", err)
	}

	service, exists := controller.GetService("productpage.service.consul")
	if !exists {
		t.Error("service should exist")
	}

	if service.Hostname != serviceHostname("productpage") {
		t.Errorf("GetService() incorrect service returned => %q, want %q",
			service.Hostname, serviceHostname("productpage"))
	}
}

func TestServices(t *testing.T) {
	ts := newServer()
	defer ts.Close()
	controller, err := NewController(ts.URL, "datacenter", 3*time.Second)
	if err != nil {
		t.Errorf("could not create Consul Controller: %v", err)
	}

	services := controller.Services()
	serviceMap := make(map[string]*model.Service)
	for _, svc := range services {
		name, err := parseHostname(svc.Hostname)
		if err != nil {
			t.Errorf("Services() error parsing hostname: %v", err)
		}
		serviceMap[name] = svc
	}

	if serviceMap["productpage"] == nil || serviceMap["reviews"] == nil || len(services) != 2 {
		t.Errorf("Services() missing or incorrect # of services returned: %q", services)
	}
}

func TestHostInstances(t *testing.T) {
	ts := newServer()
	defer ts.Close()
	controller, err := NewController(ts.URL, "datacenter", 3*time.Second)
	if err != nil {
		t.Errorf("could not create Consul Controller: %v", err)
	}

	services := controller.HostInstances(map[string]bool{"172.19.0.11": true})
	if len(services) != 1 {
		t.Errorf("HostInstances() returned wrong # of endpoints => %q, want 1", len(services))
	}

	if services[0].Service.Hostname != serviceHostname("productpage") {
		t.Errorf("HostInstances() wrong service instance returned => %q, want productpage", services[0])
	}
}
