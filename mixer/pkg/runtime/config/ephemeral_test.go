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

package config

import (
	"testing"

	"istio.io/istio/mixer/adapter/list/config"
	"istio.io/istio/mixer/pkg/runtime/testing/data"
)

func TestDynamicHandlerCorruption(t *testing.T) {
	adapters := data.BuildAdapters(nil)
	templates := data.BuildTemplates(nil)
	cfg, err := data.ReadConfigs("../../../test/listbackend/nosession.yaml", "../../../template/listentry/template.yaml")
	if err != nil {
		t.Fatal(err)
	}

	cfg = data.JoinConfigs(cfg, data.ListHandler1, data.ListHandler2, data.ListHandler3)
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
