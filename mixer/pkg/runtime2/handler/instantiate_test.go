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

package handler

import (
	"testing"

	"istio.io/istio/mixer/pkg/adapter"
	"istio.io/istio/mixer/pkg/runtime2/config/testutil"
)

func TestEmptyConfig(t *testing.T) {
	adapters := buildAdapters(nil)
	templates := buildTemplates(nil)

	s := testutil.GetSnapshot(templates, adapters, serviceConfig, globalConfig)

	table := Instantiate(Empty(), s, &fakeEnv{})
	h, found := table.GetHealthyHandler("h1.a1.istio-system")
	if !found {
		t.Fatal("not found")
	}

	if h == nil {
		t.Fatal("nil handler")
	}
}

func TestReuse(t *testing.T) {
	adapters := buildAdapters(nil)
	templates := buildTemplates(nil)

	s := testutil.GetSnapshot(templates, adapters, serviceConfig, globalConfig)

	table := Instantiate(Empty(), s, &fakeEnv{})

	// Instantiate again using the same config, but add fault to the adapter to detect change.
	adapters = buildAdapters(&adapter.Info{
		SupportedTemplates: []string{},
	})
	s = testutil.GetSnapshot(templates, adapters, serviceConfig, globalConfig)

	table2 := Instantiate(table, s, &fakeEnv{})

	if len(table2.entries) != 1 {
		t.Fatal("size")
	}

	if table2.entries["h1.a1.istio-system"] != table.entries["h1.a1.istio-system"] {
		t.Fail()
	}
}

func TestNoReuse_DifferentConfig(t *testing.T) {
	adapters := buildAdapters(nil)
	templates := buildTemplates(nil)

	s := testutil.GetSnapshot(templates, adapters, serviceConfig, globalConfig)

	table := Instantiate(Empty(), s, &fakeEnv{})

	// Instantiate again using the slightly different config
	s = testutil.GetSnapshot(templates, adapters, serviceConfig, globalConfigI2)

	table2 := Instantiate(table, s, &fakeEnv{})

	if len(table2.entries) != 1 {
		t.Fatal("size")
	}

	if table2.entries["h1.a1.istio-system"] == table.entries["h1.a1.istio-system"] {
		t.Fail()
	}
}
