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
	"istio.io/istio/mixer/pkg/runtime2/config"
	"istio.io/istio/mixer/pkg/runtime2/config/testutil"
	"istio.io/istio/mixer/pkg/template"
)

func TestCleanup_Basic(t *testing.T) {
	f := &fakeHandlerBuilder{}
	adapters := buildAdapters(&adapter.Info{NewBuilder: func() adapter.HandlerBuilder {
		return f
	}})
	templates := buildTemplates(nil)

	s := testutil.GetSnapshot(templates, adapters, serviceConfig, globalConfig)

	table := Instantiate(Empty(), s, &fakeEnv{})
	s = config.Empty()

	table2 := Instantiate(table, s, &fakeEnv{})

	Cleanup(table2, table)

	if !f.handler.closeCalled {
		t.Fail()
	}
}

func TestCleanup_NoChange(t *testing.T) {
	f := &fakeHandlerBuilder{}
	adapters := buildAdapters(&adapter.Info{NewBuilder: func() adapter.HandlerBuilder {
		return f
	}})
	templates := buildTemplates(nil)

	s := testutil.GetSnapshot(templates, adapters, serviceConfig, globalConfig)

	table := Instantiate(Empty(), s, &fakeEnv{})

	// use same config again.
	table2 := Instantiate(table, s, &fakeEnv{})

	Cleanup(table2, table)

	if f.handler.closeCalled {
		t.Fail()
	}
}

func TestCleanup_WithStartupError(t *testing.T) {
	adapters := buildAdapters(&adapter.Info{NewBuilder: func() adapter.HandlerBuilder {
		return nil
	}})
	templates := buildTemplates(&template.Info{
		HandlerSupportsTemplate: func(hndlr adapter.Handler) bool {
			// Inject an error during startup
			return false
		},
	})

	s := testutil.GetSnapshot(templates, adapters, serviceConfig, globalConfig)
	table := Instantiate(Empty(), s, &fakeEnv{})

	if table.entries["h1.a1.istio-system"].StartupError == nil {
		t.Fail()
	}

	// use different config to force cleanup
	table2 := Instantiate(table, config.Empty(), &fakeEnv{})

	Cleanup(table2, table)

	// Close shouldn't be called.
	if len(table2.entries) != 0 {
		t.Fail()
	}
}

func TestCleanup_CloseError(t *testing.T) {
	f := &fakeHandlerBuilder{
		errorOnHandlerClose: true,
	}
	adapters := buildAdapters(&adapter.Info{NewBuilder: func() adapter.HandlerBuilder {
		return f
	}})
	templates := buildTemplates(nil)

	s := testutil.GetSnapshot(templates, adapters, serviceConfig, globalConfig)
	table := Instantiate(Empty(), s, &fakeEnv{})

	// use different config to force cleanup
	table2 := Instantiate(table, config.Empty(), &fakeEnv{})

	Cleanup(table2, table)

	// Close shouldn't be called.
	if len(table2.entries) != 0 {
		t.Fail()
	}
}
