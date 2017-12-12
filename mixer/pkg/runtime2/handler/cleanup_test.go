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
	"istio.io/istio/mixer/pkg/runtime2/testing/data"
	"istio.io/istio/mixer/pkg/runtime2/testing/util"
	"istio.io/istio/mixer/pkg/template"
)

func TestCleanup_Basic(t *testing.T) {
	f := &data.FakeHandlerBuilder{}
	adapters := data.BuildAdapters(&adapter.Info{NewBuilder: func() adapter.HandlerBuilder {
		return f
	}})
	templates := data.BuildTemplates(nil)

	s := util.GetSnapshot(templates, adapters, data.ServiceConfig, data.GlobalConfig)

	table := Instantiate(Empty(), s, &data.FakeEnv{})
	s = config.Empty()

	table2 := Instantiate(table, s, &data.FakeEnv{})

	Cleanup(table2, table)

	if !f.Handler.CloseCalled {
		t.Fail()
	}
}

func TestCleanup_NoChange(t *testing.T) {
	f := &data.FakeHandlerBuilder{}
	adapters := data.BuildAdapters(&adapter.Info{NewBuilder: func() adapter.HandlerBuilder {
		return f
	}})
	templates := data.BuildTemplates(nil)

	s := util.GetSnapshot(templates, adapters, data.ServiceConfig, data.GlobalConfig)

	table := Instantiate(Empty(), s, &data.FakeEnv{})

	// use same config again.
	table2 := Instantiate(table, s, &data.FakeEnv{})

	Cleanup(table2, table)

	if f.Handler.CloseCalled {
		t.Fail()
	}
}

func TestCleanup_WithStartupError(t *testing.T) {
	adapters := data.BuildAdapters(&adapter.Info{NewBuilder: func() adapter.HandlerBuilder {
		return nil
	}})
	templates := data.BuildTemplates(&template.Info{
		HandlerSupportsTemplate: func(hndlr adapter.Handler) bool {
			// Inject an error during startup
			return false
		},
	})

	s := util.GetSnapshot(templates, adapters, data.ServiceConfig, data.GlobalConfig)
	table := Instantiate(Empty(), s, &data.FakeEnv{})

	if table.entries["h1.a1.istio-system"].StartupError == nil {
		t.Fail()
	}

	// use different config to force cleanup
	table2 := Instantiate(table, config.Empty(), &data.FakeEnv{})

	Cleanup(table2, table)

	// Close shouldn't be called.
	if len(table2.entries) != 0 {
		t.Fail()
	}
}

func TestCleanup_CloseError(t *testing.T) {
	f := &data.FakeHandlerBuilder{
		ErrorOnHandlerClose: true,
	}
	adapters := data.BuildAdapters(&adapter.Info{NewBuilder: func() adapter.HandlerBuilder {
		return f
	}})
	templates := data.BuildTemplates(nil)

	s := util.GetSnapshot(templates, adapters, data.ServiceConfig, data.GlobalConfig)
	table := Instantiate(Empty(), s, &data.FakeEnv{})

	// use different config to force cleanup
	table2 := Instantiate(table, config.Empty(), &data.FakeEnv{})

	Cleanup(table2, table)

	// Close shouldn't be called.
	if len(table2.entries) != 0 {
		t.Fail()
	}
}
