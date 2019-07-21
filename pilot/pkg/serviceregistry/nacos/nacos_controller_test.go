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

package nacos

import (
	"encoding/json"
	"fmt"
	nacos_model "github.com/nacos-group/nacos-sdk-go/model"
	"istio.io/istio/pilot/pkg/model"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sync"
	"testing"
	"time"
)

var (
	productpage = []nacos_model.Instance{
		{
			Valid:       true,
			Ip:          "172.19.0.11",
			Port:        9080,
			ServiceName: "nacos_istio_groupname@@productpage@@nacos_istio_cluster",
			Ephemeral:   true,
			Metadata:    map[string]string{SERVICE_TAGS: "version|v1"},
		},
	}

	producetServices = nacos_model.Service{
		Hosts: productpage,
		Name:  "nacos_istio_groupname@@productpage@@nacos_istio_cluster",
	}

	reviews = []nacos_model.Instance{
		{
			Valid:       true,
			Ip:          "172.19.0.6",
			Port:        9080,
			ServiceName: "nacos_istio_groupname@@reviews@@nacos_istio_cluster",
			Ephemeral:   true,
			Metadata:    map[string]string{SERVICE_TAGS: "version|v1"},
		},
		{
			Valid:       true,
			Ip:          "172.19.0.7",
			Port:        9081,
			ServiceName: "nacos_istio_groupname@@reviews@@nacos_istio_cluster",
			Ephemeral:   true,
			Metadata:    map[string]string{SERVICE_TAGS: "version|v2"},
		},
		{
			Valid:       true,
			Ip:          "172.19.0.8",
			Port:        9081,
			ServiceName: "nacos_istio_groupname@@reviews@@nacos_istio_cluster",
			Ephemeral:   true,
			Metadata:    map[string]string{SERVICE_TAGS: "version|v3", PROTOCOL_NAME: "tcp"},
		},
	}

	reviewsServices = nacos_model.Service{
		Hosts: reviews,
		Name:  "nacos_istio_groupname@@reviews@@nacos_istio_cluster",
	}
	rating = []nacos_model.Instance{
		{
			Valid:       true,
			Ip:          "172.19.0.12",
			Port:        9080,
			ServiceName: "nacos_istio_groupname@@rating@@nacos_istio_cluster",
			Ephemeral:   true,
			Metadata:    map[string]string{SERVICE_TAGS: "version|v1"},
		},
	}
	ratingServices = nacos_model.Service{
		Hosts: rating,
		Name:  "nacos_istio_groupname@@rating@@nacos_istio_cluster",
	}
)

type mockServer struct {
	Server           *httptest.Server
	producetServices nacos_model.Service
	reviewsServices  nacos_model.Service
	ratingServices   nacos_model.Service
	services         []nacos_model.Service
	Lock             sync.Mutex
}

func newServer() *mockServer {
	m := mockServer{
		producetServices: nacos_model.Service{},
		reviewsServices:  nacos_model.Service{},
		ratingServices:   nacos_model.Service{},
		services:         make([]nacos_model.Service, 0),
	}

	m.producetServices = producetServices
	m.reviewsServices = reviewsServices
	m.ratingServices = ratingServices
	m.services = append(m.services, producetServices, ratingServices, reviewsServices)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/nacos/v1/ns/service/getAll" {
			m.Lock.Lock()
			data, _ := json.Marshal(&m.services)
			m.Lock.Unlock()
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprintln(w, string(data))
		} else if r.URL.Path == "/nacos/v1/ns/instance/list" {
			m.Lock.Lock()
			serviceName := r.Form.Get("serviceName")
			var data []byte
			if serviceName == "reviews" {
				data, _ = json.Marshal(&m.reviewsServices)
			} else if serviceName == "productpage" {
				data, _ = json.Marshal(&m.producetServices)
			} else if serviceName == "rating" {
				data, _ = json.Marshal(&m.ratingServices)
			}
			m.Lock.Unlock()
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprintln(w, string(data))
		} else {
			data, _ := json.Marshal([]nacos_model.Service{})
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprintln(w, string(data))
		}
	}))

	m.Server = server
	return &m
}

func TestInstances(t *testing.T) {
	ts := newServer()
	defer ts.Server.Close()
	controller, err := NewController(ts.Server.URL, 3*time.Second)
	if err != nil {
		t.Errorf("could not create Nacos Controller: %v", err)
	}

	hostname := serviceHostname("reviews")
	instances, err := controller.InstancesByPort(hostname, 0, model.LabelsCollection{})
	if err != nil {
		t.Errorf("client encountered error during Instances(): %v", err)
	}
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
	instances, err = controller.InstancesByPort(hostname, 0, model.LabelsCollection{
		model.Labels{filterTagKey: filterTagVal},
	})
	if err != nil {
		t.Errorf("client encountered error during Instances(): %v", err)
	}
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
			t.Errorf("Instances() did not match by tag {%q:%q}", filterTagKey, filterTagVal)
		}
	}

	filterPort := 9081
	instances, err = controller.InstancesByPort(hostname, filterPort, model.LabelsCollection{})
	if err != nil {
		t.Errorf("client encountered error during Instances(): %v", err)
	}
	if len(instances) != 3 {
		fmt.Println(instances)
		t.Errorf("Instances() did not filter by port => %q, want 2", len(instances))
	}
	for _, inst := range instances {
		if inst.Endpoint.ServicePort.Port != filterPort {
			t.Errorf("Instances() did not filter by port => %q, want %q",
				inst.Endpoint.ServicePort.Name, filterPort)
		}
	}
}

func TestInstancesBadHostname(t *testing.T) {
	ts := newServer()
	defer ts.Server.Close()
	controller, err := NewController(ts.Server.URL, 3*time.Second)
	if err != nil {
		t.Errorf("could not create Consul Controller: %v", err)
	}

	instances, err := controller.InstancesByPort("", 0, model.LabelsCollection{})
	if err == nil {
		t.Error("Instances() should return error when provided bad hostname")
	}
	if len(instances) != 0 {
		t.Errorf("Instances() returned wrong # of service instances => %q, want 0", len(instances))
	}
}

func TestInstancesError(t *testing.T) {
	ts := newServer()
	controller, err := NewController(ts.Server.URL, 3*time.Second)
	if err != nil {
		ts.Server.Close()
		t.Errorf("could not create Consul Controller: %v", err)
	}

	ts.Server.Close()
	instances, err := controller.InstancesByPort(serviceHostname("reviews"), 0, model.LabelsCollection{})
	if err == nil {
		t.Error("Instances() should return error when client experiences connection problem")
	}
	if len(instances) != 0 {
		t.Errorf("Instances() returned wrong # of instances: %q, want 0", len(instances))
	}
}

func TestGetService(t *testing.T) {
	ts := newServer()
	defer ts.Server.Close()
	controller, err := NewController(ts.Server.URL, 3*time.Second)
	if err != nil {
		t.Errorf("could not create Consul Controller: %v", err)
	}

	service, err := controller.GetService("productpage")
	if err != nil {
		t.Errorf("client encountered error during GetService(): %v", err)
	}
	if service == nil {
		t.Error("service should exist")
	}

	if service.Hostname != serviceHostname("productpage") {
		t.Errorf("GetService() incorrect service returned => %q, want %q",
			service.Hostname, serviceHostname("productpage"))
	}
}

func TestGetServiceError(t *testing.T) {
	ts := newServer()
	controller, err := NewController(ts.Server.URL, 3*time.Second)
	if err != nil {
		ts.Server.Close()
		t.Errorf("could not create Consul Controller: %v", err)
	}

	ts.Server.Close()
	service, err := controller.GetService("productpage.service.consul")
	if err == nil {
		t.Error("GetService() should return error when client experiences connection problem")
	}
	if service != nil {
		t.Error("GetService() should return nil when client experiences connection problem")
	}
}

func TestGetServiceBadHostname(t *testing.T) {
	ts := newServer()
	defer ts.Server.Close()
	controller, err := NewController(ts.Server.URL, 3*time.Second)
	if err != nil {
		t.Errorf("could not create Consul Controller: %v", err)
	}

	service, err := controller.GetService("")
	if err == nil {
		t.Error("GetService() should thow error for bad hostnames")
	}
	if service != nil {
		t.Error("service should not exist")
	}
}

func TestGetServiceNoInstances(t *testing.T) {
	ts := newServer()
	defer ts.Server.Close()
	controller, err := NewController(ts.Server.URL, 3*time.Second)
	if err != nil {
		t.Errorf("could not create Consul Controller: %v", err)
	}

	ts.services = []nacos_model.Service{}

	service, err := controller.GetService("productpage.service.consul")
	if err != nil {
		t.Errorf("GetService() encountered unexpected error: %v", err)
	}
	if service != nil {
		t.Error("service should not exist")
	}
}

func TestServices(t *testing.T) {
	ts := newServer()
	defer ts.Server.Close()
	controller, err := NewController(ts.Server.URL, 3*time.Second)
	if err != nil {
		t.Errorf("could not create Consul Controller: %v", err)
	}

	services, err := controller.Services()
	if err != nil {
		t.Errorf("client encountered error during Services(): %v", err)
	}
	serviceMap := make(map[string]*model.Service)
	for _, svc := range services {
		name, err := parseHostname(svc.Hostname)
		if err != nil {
			t.Errorf("Services() error parsing hostname: %v", err)
		}
		serviceMap[name] = svc
	}

	for _, name := range []string{"productpage", "reviews", "rating"} {
		if _, exists := serviceMap[name]; !exists {
			t.Errorf("Services() missing: %q", name)
		}
	}
	if len(services) != 3 {
		t.Errorf("Services() returned wrong # of services: %q, want 3", len(services))
	}
}

func TestServicesError(t *testing.T) {
	ts := newServer()
	controller, err := NewController(ts.Server.URL, 3*time.Second)
	if err != nil {
		ts.Server.Close()
		t.Errorf("could not create Consul Controller: %v", err)
	}

	ts.Server.Close()
	services, err := controller.Services()
	if err == nil {
		t.Error("Services() should return error when client experiences connection problem")
	}
	if len(services) != 0 {
		t.Errorf("Services() returned wrong # of services: %q, want 0", len(services))
	}
}

func TestGetProxyServiceInstances(t *testing.T) {
	ts := newServer()
	defer ts.Server.Close()
	controller, err := NewController(ts.Server.URL, 3*time.Second)
	if err != nil {
		t.Errorf("could not create Consul Controller: %v", err)
	}

	services, err := controller.GetProxyServiceInstances(&model.Proxy{IPAddresses: []string{"172.19.0.11"}})
	if err != nil {
		t.Errorf("client encountered error during GetProxyServiceInstances(): %v", err)
	}
	if len(services) != 1 {
		t.Errorf("GetProxyServiceInstances() returned wrong # of endpoints => %q, want 1", len(services))
	}

	if services[0].Service.Hostname != serviceHostname("productpage") {
		t.Errorf("GetProxyServiceInstances() wrong service instance returned => hostname %q, want %q",
			services[0].Service.Hostname, serviceHostname("productpage"))
	}
}

func TestGetProxyServiceInstancesError(t *testing.T) {
	ts := newServer()
	controller, err := NewController(ts.Server.URL, 3*time.Second)
	if err != nil {
		ts.Server.Close()
		t.Errorf("could not create Consul Controller: %v", err)
	}

	ts.Server.Close()
	instances, err := controller.GetProxyServiceInstances(&model.Proxy{IPAddresses: []string{"172.19.0.11"}})
	if err == nil {
		t.Error("GetProxyServiceInstances() should return error when client experiences connection problem")
	}
	if len(instances) != 0 {
		t.Errorf("GetProxyServiceInstances() returned wrong # of instances: %q, want 0", len(instances))
	}
}

func TestGetProxyServiceInstancesWithMultiIPs(t *testing.T) {
	ts := newServer()
	defer ts.Server.Close()
	controller, err := NewController(ts.Server.URL, 3*time.Second)
	if err != nil {
		t.Errorf("could not create Consul Controller: %v", err)
	}

	services, err := controller.GetProxyServiceInstances(&model.Proxy{IPAddresses: []string{"10.78.11.18", "172.19.0.12"}})
	if err != nil {
		t.Errorf("client encountered error during GetProxyServiceInstances(): %v", err)
	}
	if len(services) != 1 {
		t.Errorf("GetProxyServiceInstances() returned wrong # of endpoints => %q, want 1", len(services))
	}

	if services[0].Service.Hostname != serviceHostname("rating") {
		t.Errorf("GetProxyServiceInstances() wrong service instance returned => hostname %q, want %q",
			services[0].Service.Hostname, serviceHostname("productpage"))
	}
}

func TestGetProxyWorkloadLabels(t *testing.T) {
	ts := newServer()
	defer ts.Server.Close()
	controller, err := NewController(ts.Server.URL, 3*time.Second)
	if err != nil {
		t.Errorf("could not create Consul Controller: %v", err)
	}

	tests := []struct {
		name     string
		ips      []string
		expected model.LabelsCollection
	}{
		{
			name:     "Rating",
			ips:      []string{"10.78.11.18", "172.19.0.12"},
			expected: model.LabelsCollection{{"version": "v1"}},
		},
		{
			name:     "No proxy ip",
			ips:      nil,
			expected: model.LabelsCollection{},
		},
		{
			name:     "No match",
			ips:      []string{"1.2.3.4", "2.3.4.5"},
			expected: model.LabelsCollection{},
		},
		{
			name:     "Only match on Service Address",
			ips:      []string{"172.19.0.5"},
			expected: model.LabelsCollection{},
		},
		{
			name:     "Match multiple services",
			ips:      []string{"172.19.0.7", "172.19.0.8"},
			expected: model.LabelsCollection{{"version": "v2"}, {"version": "v3"}},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			labels, err := controller.GetProxyWorkloadLabels(&model.Proxy{IPAddresses: test.ips})

			if err != nil {
				t.Errorf("client encountered error during GetProxyWorkloadLabels(): %v", err)
			}
			if labels == nil {
				t.Error("labels should exist")
			}

			if !reflect.DeepEqual(labels, test.expected) {
				t.Errorf("GetProxyWorkloadLabels() wrong labels => returned %#v, want %#v", labels, test.expected)
			}
		})
	}
}
