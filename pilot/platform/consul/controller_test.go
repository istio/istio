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
	"sync"
	"testing"

	"github.com/hashicorp/consul/api"

	"istio.io/istio/pilot/model"
	"istio.io/istio/pilot/platform/test"
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
			ID:             "111-111-111",
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
			ID:             "222-222-222",
			ServiceName:    "reviews",
			ServiceTags:    []string{"version|v1"},
			ServiceAddress: "172.19.0.6",
			ServicePort:    9080,
		},
		{
			Node:           "istio",
			Address:        "172.19.0.5",
			ID:             "333-333-333",
			ServiceName:    "reviews",
			ServiceTags:    []string{"version|v2"},
			ServiceAddress: "172.19.0.7",
			ServicePort:    9080,
		},
		{
			Node:           "istio",
			Address:        "172.19.0.5",
			ID:             "444-444-444",
			ServiceName:    "reviews",
			ServiceTags:    []string{"version|v3"},
			ServiceAddress: "172.19.0.8",
			ServicePort:    9080,
			NodeMeta:       map[string]string{protocolTagName: "tcp"},
		},
	}
)

type mockServer struct {
	Server      *httptest.Server
	Services    map[string][]string
	Productpage []*api.CatalogService
	Reviews     []*api.CatalogService
	Lock        sync.Mutex
}

func newServer() *mockServer {
	m := mockServer{
		Productpage: make([]*api.CatalogService, len(productpage)),
		Reviews:     make([]*api.CatalogService, len(reviews)),
		Services:    make(map[string][]string),
	}

	copy(m.Reviews, reviews)
	copy(m.Productpage, productpage)
	for k, v := range services {
		m.Services[k] = v
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/catalog/services" {
			m.Lock.Lock()
			data, _ := json.Marshal(&m.Services)
			m.Lock.Unlock()
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprintln(w, string(data))
		} else if r.URL.Path == "/v1/catalog/service/reviews" {
			m.Lock.Lock()
			data, _ := json.Marshal(&m.Reviews)
			m.Lock.Unlock()
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprintln(w, string(data))
		} else if r.URL.Path == "/v1/catalog/service/productpage" {
			m.Lock.Lock()
			data, _ := json.Marshal(&m.Productpage)
			m.Lock.Unlock()
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprintln(w, string(data))
		} else {
			data, _ := json.Marshal(&[]*api.CatalogService{})
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprintln(w, string(data))
		}
	}))

	m.Server = server
	return &m
}

func buildExpectedControllerView() *model.ControllerView {
    modelSvcs := []*model.Service{
        convertService(reviews),
        convertService(productpage),
    }
    modelInsts := make([]*model.ServiceInstance, len(reviews) + len(productpage))
    instIdx := 0
    for _, endpoint := range reviews {
        modelInsts[instIdx] = convertInstance(endpoint)
        instIdx++
    }
    for _, endpoint := range productpage {
        modelInsts[instIdx] = convertInstance(endpoint)
        instIdx++
    }
    return test.BuildExpectedControllerView(modelSvcs, modelInsts)
}

// The only thing being tested are the public interfaces of the controller
// namely: Handle() and Run(). Everything else is implementation detail.
func TestController(t *testing.T) {
	ts := newServer()
	defer ts.Server.Close()
	
	mockHandler := test.NewMockControllerViewHandler()
	controller, err := NewController(ts.Server.URL, "datacenter", *mockHandler.GetTicker())
	if err != nil {
		t.Fatalf("could not create Consul Controller: %v", err)
	}
	controller.Handle(test.MockControllerPath, mockHandler.ToModelControllerViewHandler())
	go controller.Run(mockHandler.GetStopChannel())
	defer mockHandler.StopController()
	actualView := mockHandler.GetReconciledView()
	test.AssertControllerViewEquals(t, buildExpectedControllerView(), actualView)
}
