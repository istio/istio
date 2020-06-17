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

package consul

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sort"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/hashicorp/consul/api"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/config/labels"
)

const (
	clusterID = ""
)

type mockServer struct {
	server      *httptest.Server
	services    map[string][]string
	productpage []*api.CatalogService
	reviews     []*api.CatalogService
	rating      []*api.CatalogService
	lock        sync.Mutex
	consulIndex int
}

func newServer() *mockServer {
	m := mockServer{
		productpage: []*api.CatalogService{
			{
				Node:           "istio-node",
				Address:        "172.19.0.5",
				ID:             "istio-node-id",
				ServiceID:      "productpage",
				ServiceName:    "productpage",
				ServiceTags:    []string{"version|v1"},
				ServiceAddress: "172.19.0.11",
				ServicePort:    9080,
			},
		},
		reviews: []*api.CatalogService{
			{
				Node:           "istio-node",
				Address:        "172.19.0.5",
				ID:             "istio-node-id",
				ServiceID:      "reviews-id",
				ServiceName:    "reviews",
				ServiceTags:    []string{"version|v1"},
				ServiceAddress: "172.19.0.6",
				ServicePort:    9081,
			},
			{
				Node:           "istio-node",
				Address:        "172.19.0.5",
				ID:             "istio-node-id",
				ServiceID:      "reviews-id",
				ServiceName:    "reviews",
				ServiceTags:    []string{"version|v2"},
				ServiceAddress: "172.19.0.7",
				ServicePort:    9081,
			},
			{
				Node:           "istio-node",
				Address:        "172.19.0.5",
				ID:             "istio-node-id",
				ServiceID:      "reviews-id",
				ServiceName:    "reviews",
				ServiceTags:    []string{"version|v3"},
				ServiceAddress: "172.19.0.8",
				ServicePort:    9080,
				ServiceMeta:    map[string]string{protocolTagName: "tcp"},
			},
		},
		rating: []*api.CatalogService{
			{
				Node:           "istio-node",
				Address:        "172.19.0.6",
				ID:             "istio-node-id",
				ServiceID:      "rating-id",
				ServiceName:    "rating",
				ServiceTags:    []string{"version|v1"},
				ServiceAddress: "172.19.0.12",
				ServicePort:    9080,
			},
		},
		services: map[string][]string{
			"productpage": {"version|v1"},
			"reviews":     {"version|v1", "version|v2", "version|v3"},
			"rating":      {"version|v1"},
		},
		consulIndex: 1,
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/catalog/services" {
			m.lock.Lock()
			data, _ := json.Marshal(&m.services)
			w.Header().Set("X-Consul-Index", strconv.Itoa(m.consulIndex))
			m.lock.Unlock()
			w.Header().Set("Content-Type", "application/json")
			_, _ = fmt.Fprintln(w, string(data))
		} else if r.URL.Path == "/v1/catalog/service/reviews" {
			m.lock.Lock()
			data, _ := json.Marshal(&m.reviews)
			w.Header().Set("X-Consul-Index", strconv.Itoa(m.consulIndex))
			m.lock.Unlock()
			w.Header().Set("Content-Type", "application/json")
			_, _ = fmt.Fprintln(w, string(data))
		} else if r.URL.Path == "/v1/catalog/service/productpage" {
			m.lock.Lock()
			data, _ := json.Marshal(&m.productpage)
			w.Header().Set("X-Consul-Index", strconv.Itoa(m.consulIndex))
			m.lock.Unlock()
			w.Header().Set("Content-Type", "application/json")
			_, _ = fmt.Fprintln(w, string(data))
		} else if r.URL.Path == "/v1/catalog/service/rating" {
			m.lock.Lock()
			data, _ := json.Marshal(&m.rating)
			w.Header().Set("X-Consul-Index", strconv.Itoa(m.consulIndex))
			m.lock.Unlock()
			w.Header().Set("Content-Type", "application/json")
			_, _ = fmt.Fprintln(w, string(data))
		} else {
			m.lock.Lock()
			data, _ := json.Marshal(&[]*api.CatalogService{})
			w.Header().Set("X-Consul-Index", strconv.Itoa(m.consulIndex))
			m.lock.Unlock()
			w.Header().Set("Content-Type", "application/json")
			_, _ = fmt.Fprintln(w, string(data))
		}
	}))

	m.server = server
	return &m
}

func TestInstances(t *testing.T) {
	ts := newServer()
	defer ts.server.Close()
	controller, err := NewController(ts.server.URL, clusterID)
	if err != nil {
		t.Errorf("could not create Consul Controller: %v", err)
	}
	hostname := serviceHostname("reviews")
	svc := &model.Service{
		Hostname: hostname,
		Attributes: model.ServiceAttributes{
			Name:      "reviews",
			Namespace: model.IstioDefaultConfigNamespace,
		},
	}

	instances, err := controller.InstancesByPort(svc, 0, labels.Collection{})
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
	instances, err = controller.InstancesByPort(svc, 0, labels.Collection{
		labels.Instance{filterTagKey: filterTagVal},
	})
	if err != nil {
		t.Errorf("client encountered error during Instances(): %v", err)
	}
	if len(instances) != 1 {
		t.Errorf("Instances() did not filter by tags => %q, want 1", len(instances))
	}
	for _, inst := range instances {
		found := false
		for key, val := range inst.Endpoint.Labels {
			if key == filterTagKey && val == filterTagVal {
				found = true
			}
		}
		if !found {
			t.Errorf("Instances() did not match by tag {%q:%q}", filterTagKey, filterTagVal)
		}
	}

	filterPort := 9081
	instances, err = controller.InstancesByPort(svc, filterPort, labels.Collection{})
	if err != nil {
		t.Errorf("client encountered error during Instances(): %v", err)
	}
	if len(instances) != 2 {
		fmt.Println(instances)
		t.Errorf("Instances() did not filter by port => %q, want 2", len(instances))
	}
	for _, inst := range instances {
		if inst.ServicePort.Port != filterPort {
			t.Errorf("Instances() did not filter by port => %q, want %q",
				inst.ServicePort.Name, filterPort)
		}
	}
}

func TestInstancesBadHostname(t *testing.T) {
	ts := newServer()
	defer ts.server.Close()
	controller, err := NewController(ts.server.URL, clusterID)
	if err != nil {
		t.Errorf("could not create Consul Controller: %v", err)
	}
	svc := &model.Service{
		Hostname: "",
		Attributes: model.ServiceAttributes{
			Name:      "reviews",
			Namespace: model.IstioDefaultConfigNamespace,
		},
	}
	instances, err := controller.InstancesByPort(svc, 0, labels.Collection{})
	if err == nil {
		t.Error("Instances() should return error when provided bad hostname")
	}
	if len(instances) != 0 {
		t.Errorf("Instances() returned wrong # of service instances => %q, want 0", len(instances))
	}
}

func TestInstancesError(t *testing.T) {
	ts := newServer()
	controller, err := NewController(ts.server.URL, clusterID)
	if err != nil {
		ts.server.Close()
		t.Errorf("could not create Consul Controller: %v", err)
	}
	hostname := serviceHostname("reviews")
	svc := &model.Service{
		Hostname: hostname,
		Attributes: model.ServiceAttributes{
			Name:      "reviews",
			Namespace: model.IstioDefaultConfigNamespace,
		},
	}
	ts.server.Close()
	instances, err := controller.InstancesByPort(svc, 0, labels.Collection{})
	if err == nil {
		t.Error("Instances() should return error when client experiences connection problem")
	}
	if len(instances) != 0 {
		t.Errorf("Instances() returned wrong # of instances: %q, want 0", len(instances))
	}
}

func TestGetService(t *testing.T) {
	ts := newServer()
	defer ts.server.Close()
	controller, err := NewController(ts.server.URL, clusterID)
	if err != nil {
		t.Errorf("could not create Consul Controller: %v", err)
	}

	service, err := controller.GetService("productpage.service.consul")
	if err != nil {
		t.Errorf("client encountered error during GetService(): %v", err)
	}
	if service == nil {
		t.Error("service should exist")
		return
	}

	if service.Hostname != serviceHostname("productpage") {
		t.Errorf("GetService() incorrect service returned => %q, want %q",
			service.Hostname, serviceHostname("productpage"))
	}
}

func TestGetServiceError(t *testing.T) {
	ts := newServer()
	controller, err := NewController(ts.server.URL, clusterID)
	if err != nil {
		ts.server.Close()
		t.Errorf("could not create Consul Controller: %v", err)
	}

	ts.server.Close()
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
	defer ts.server.Close()
	controller, err := NewController(ts.server.URL, clusterID)
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
	defer ts.server.Close()
	controller, err := NewController(ts.server.URL, clusterID)
	if err != nil {
		t.Errorf("could not create Consul Controller: %v", err)
	}

	service, err := controller.GetService("details.service.consul")
	if err != nil {
		t.Errorf("GetService() encountered unexpected error: %v", err)
	}
	if service != nil {
		t.Error("service should not exist")
	}
}

func TestServices(t *testing.T) {
	ts := newServer()
	defer ts.server.Close()
	controller, err := NewController(ts.server.URL, clusterID)
	if err != nil {
		t.Errorf("could not create Consul Controller: %v", err)
	}

	services, err := controller.Services()
	if err != nil {
		t.Errorf("client encountered error during services(): %v", err)
	}
	serviceMap := make(map[string]*model.Service)
	for _, svc := range services {
		name, err := parseHostname(svc.Hostname)
		if err != nil {
			t.Errorf("services() error parsing hostname: %v", err)
		}
		serviceMap[name] = svc
	}

	for _, name := range []string{"productpage", "reviews", "rating"} {
		if _, exists := serviceMap[name]; !exists {
			t.Errorf("services() missing: %q", name)
		}
	}
	if len(services) != 3 {
		t.Errorf("services() returned wrong # of services: %q, want 3", len(services))
	}
}

func TestServicesError(t *testing.T) {
	ts := newServer()
	controller, err := NewController(ts.server.URL, clusterID)
	if err != nil {
		ts.server.Close()
		t.Errorf("could not create Consul Controller: %v", err)
	}

	ts.server.Close()
	services, err := controller.Services()
	if err == nil {
		t.Error("services() should return error when client experiences connection problem")
	}
	if len(services) != 0 {
		t.Errorf("services() returned wrong # of services: %q, want 0", len(services))
	}
}

func TestGetProxyServiceInstances(t *testing.T) {
	ts := newServer()
	defer ts.server.Close()
	controller, err := NewController(ts.server.URL, clusterID)
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
	controller, err := NewController(ts.server.URL, clusterID)
	if err != nil {
		ts.server.Close()
		t.Errorf("could not create Consul Controller: %v", err)
	}

	ts.server.Close()
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
	defer ts.server.Close()
	controller, err := NewController(ts.server.URL, clusterID)
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
	defer ts.server.Close()
	controller, err := NewController(ts.server.URL, clusterID)
	if err != nil {
		t.Errorf("could not create Consul Controller: %v", err)
	}

	tests := []struct {
		name     string
		ips      []string
		expected labels.Collection
	}{
		{
			name:     "rating",
			ips:      []string{"10.78.11.18", "172.19.0.12"},
			expected: labels.Collection{{"version": "v1"}},
		},
		{
			name:     "No proxy ip",
			ips:      nil,
			expected: labels.Collection{},
		},
		{
			name:     "No match",
			ips:      []string{"1.2.3.4", "2.3.4.5"},
			expected: labels.Collection{},
		},
		{
			name:     "Only match on Service Address",
			ips:      []string{"172.19.0.5"},
			expected: labels.Collection{},
		},
		{
			name:     "Match multiple services",
			ips:      []string{"172.19.0.7", "172.19.0.8"},
			expected: labels.Collection{{"version": "v2"}, {"version": "v3"}},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			wlLabels, err := controller.GetProxyWorkloadLabels(&model.Proxy{IPAddresses: test.ips})
			sort.Slice(wlLabels, func(i, j int) bool {
				return wlLabels[i].String() < wlLabels[j].String()
			})
			if err != nil {
				t.Errorf("client encountered error during GetProxyWorkloadLabels(): %v", err)
			}
			if wlLabels == nil {
				t.Error("labels should exist")
			}

			if !reflect.DeepEqual(wlLabels, test.expected) {
				t.Errorf("GetProxyWorkloadLabels() wrong labels => returned %#v, want %#v", wlLabels, test.expected)
			}
		})
	}
}

func TestGetServiceByCache(t *testing.T) {
	ts := newServer()
	controller, err := NewController(ts.server.URL, clusterID)
	if err != nil {
		t.Errorf("could not create Consul Controller: %v", err)
	}
	_, _ = controller.GetService("productpage.service.consul")
	ts.server.Close()
	service, err := controller.GetService("productpage.service.consul")
	if err != nil {
		t.Errorf("client encountered error during GetService(): %v", err)
	}
	if service == nil {
		t.Fatalf("service should exist")
	}

	if service.Hostname != serviceHostname("productpage") {
		t.Errorf("GetService() incorrect service returned => %q, want %q",
			service.Hostname, serviceHostname("productpage"))
	}
}

func TestGetInstanceByCacheAfterChanged(t *testing.T) {
	ts := newServer()
	defer ts.server.Close()
	controller, err := NewController(ts.server.URL, clusterID)
	if err != nil {
		t.Errorf("could not create Consul Controller: %v", err)
	}
	go controller.Run(make(chan struct{}))

	hostname := serviceHostname("reviews")
	svc := &model.Service{
		Hostname: hostname,
		Attributes: model.ServiceAttributes{
			Name:      "reviews",
			Namespace: model.IstioDefaultConfigNamespace,
		},
	}
	instances, err := controller.InstancesByPort(svc, 0, labels.Collection{})
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

	ts.lock.Lock()
	ts.reviews = []*api.CatalogService{
		{
			Node:           "istio-node",
			Address:        "172.19.0.5",
			ID:             "istio-node-id",
			ServiceID:      "reviews-id",
			ServiceName:    "reviews",
			ServiceTags:    []string{"version|v1"},
			ServiceAddress: "172.19.0.7",
			ServicePort:    9081,
		},
	}
	ts.consulIndex++
	ts.lock.Unlock()

	time.Sleep(notifyThreshold)
	instances, err = controller.InstancesByPort(svc, 0, labels.Collection{})
	if err != nil {
		t.Errorf("client encountered error during Instances(): %v", err)
	}
	if len(instances) != 1 {
		t.Errorf("Instances() returned wrong # of service instances => %q, want 1", len(instances))
	}
	for _, inst := range instances {
		if inst.Service.Hostname != hostname {
			t.Errorf("Instances() returned wrong service instance => %v, want %q",
				inst.Service.Hostname, hostname)
		}
	}
}
