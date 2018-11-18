// Copyright 2018 Istio Authors
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

package config

import (
	"io/ioutil"
	"testing"

	"istio.io/istio/mixer/adapter/list/config"
	"istio.io/istio/mixer/pkg/runtime/testing/data"
)

func TestDynamicHandlerCorruption(t *testing.T) {
	adapters := data.BuildAdapters(nil)
	templates := data.BuildTemplates(nil)

	// need an adapter with config
	listbackend, err := ioutil.ReadFile("../../../test/listbackend/nosession.yaml")
	if err != nil {
		t.Fatal(err)
	}

	cfg := string(listbackend)

	listentry, err := ioutil.ReadFile("../../../template/listentry/template.yaml")
	if err != nil {
		t.Fatal(err)
	}
	cfg = cfg + "\n---\n" + string(listentry)

	cfg = cfg + `
---
apiVersion: config.istio.io/v1alpha2
kind: handler
metadata:
  name: h1
  namespace: istio-system
spec:
  adapter: listbackend-nosession
  connection:
    address: 127.0.0.1:8080
  params:
    provider_url: google.com
    overrides:
    - a
    - b
    caching_interval: 5s
---
apiVersion: config.istio.io/v1alpha2
kind: handler
metadata:
  name: h2
  namespace: istio-system
spec:
  adapter: listbackend-nosession
  connection:
    address: 127.0.0.1:8080
  params:
    refresh_interval: 5s
    caching_use_count: 50
---
apiVersion: config.istio.io/v1alpha2
kind: handler
metadata:
  name: h3
  namespace: istio-system
spec:
  adapter: listbackend-nosession
  connection:
    address: 127.0.0.1:8080
  params:
    blacklist: true
---
`
	s, err := GetSnapshotForTest(templates, adapters, data.ServiceConfig, cfg)
	if err != nil {
		t.Fatal(err)
	}
	handlers := s.HandlersDynamic
	if len(handlers) != 3 {
		t.Errorf("got %d, want 3 handlers", len(handlers))
	}

	for _, handler := range handlers {
		bytes := handler.AdapterConfig.Value
		params := config.Params{}
		if err = params.Unmarshal(bytes); err != nil {
			t.Errorf("got corrupt handler %#v", bytes)
		}
	}
}
