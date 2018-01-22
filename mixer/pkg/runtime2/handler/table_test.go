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

package handler

import (
	"strings"
	"testing"

	"istio.io/istio/mixer/pkg/runtime2/config"
	"istio.io/istio/mixer/pkg/runtime2/testing/data"
	"istio.io/istio/mixer/pkg/runtime2/testing/util"
)

// Create a standard global config with Handler H1, Instance I1 and rule R1 referencing I1 and H1.
// Most of the tests use this config.
var globalCfg = data.JoinConfigs(data.HandlerACheck1, data.InstanceCheck1, data.RuleCheck1)

//  Alternate configuration where R2 references H1, but uses instances I1 and I2.
var globalCfgI2 = data.JoinConfigs(data.HandlerACheck1, data.InstanceCheck1, data.InstanceCheck2, data.RuleCheck1WithInstance1And2)

func TestNew_EmptyConfig(t *testing.T) {
	s := config.Empty()

	table := NewTable(Empty(), s, nil)
	e, found := table.Get(data.FqnACheck1)
	if found {
		t.Fatal("found")
	}

	if e.Name != "" {
		t.Fatal("non-empty handler")
	}
}

func TestNew_EmptyOldTable(t *testing.T) {
	adapters := data.BuildAdapters(nil)
	templates := data.BuildTemplates(nil)

	s := util.GetSnapshot(templates, adapters, data.ServiceConfig, globalCfg)

	table := NewTable(Empty(), s, nil)
	e, found := table.Get(data.FqnACheck1)
	if !found {
		t.Fatal("not found")
	}

	if e.Name == "" {
		t.Fatal("empty handler")
	}
}

func TestNew_Reuse(t *testing.T) {
	adapters := data.BuildAdapters(nil)
	templates := data.BuildTemplates(nil)

	s := util.GetSnapshot(templates, adapters, data.ServiceConfig, globalCfg)

	table := NewTable(Empty(), s, nil)

	// NewTable again using the same config, but add fault to the adapter to detect change.
	adapters = data.BuildAdapters(nil, data.FakeAdapterSettings{Name: "tcheck", ErrorAtBuild: true})
	s = util.GetSnapshot(templates, adapters, data.ServiceConfig, globalCfg)

	table2 := NewTable(table, s, nil)

	if len(table2.entries) != 1 {
		t.Fatal("size")
	}

	if newEntry, found := table2.entries[data.FqnACheck1]; !found || newEntry != table.entries[data.FqnACheck1] {
		t.Fail()
	}
}

func TestNew_NoReuse_DifferentConfig(t *testing.T) {
	adapters := data.BuildAdapters(nil)
	templates := data.BuildTemplates(nil)

	s := util.GetSnapshot(templates, adapters, data.ServiceConfig, globalCfg)

	table := NewTable(Empty(), s, nil)

	// NewTable again using the slightly different config
	s = util.GetSnapshot(templates, adapters, data.ServiceConfig, globalCfgI2)

	table2 := NewTable(table, s, nil)

	if len(table2.entries) != 1 {
		t.Fatal("size")
	}

	if table2.entries[data.FqnACheck1] == table.entries[data.FqnACheck1] {
		t.Fail()
	}
}

func TestTable_Get(t *testing.T) {
	table := &Table{
		entries: make(map[string]Entry),
	}

	table.entries["h1"] = Entry{
		Name:    "h1",
		Handler: &data.FakeHandler{},
	}

	e, found := table.Get("h1")
	if !found {
		t.Fail()
	}

	if e.Name == "" {
		t.Fail()
	}
}

func TestTable_Get_Empty(t *testing.T) {
	table := &Table{
		entries: make(map[string]Entry),
	}

	_, found := table.Get("h1")
	if found {
		t.Fail()
	}
}

func TestEmpty(t *testing.T) {
	table := Empty()

	if len(table.entries) > 0 {
		t.Fail()
	}
}

func TestCleanup_Basic(t *testing.T) {
	l := &data.Logger{}
	adapters := data.BuildAdapters(l, data.FakeAdapterSettings{Name: "acheck"})
	templates := data.BuildTemplates(nil)

	s := util.GetSnapshot(templates, adapters, data.ServiceConfig, globalCfg)

	table := NewTable(Empty(), s, nil)

	s = config.Empty()

	table2 := NewTable(table, s, nil)

	table.Cleanup(table2)

	expected := `
[acheck] NewBuilder =>
[acheck] NewBuilder <=
[acheck] HandlerBuilder.SetAdapterConfig => '&Struct{Fields:map[string]*Value{},}'
[acheck] HandlerBuilder.SetAdapterConfig <=
[acheck] HandlerBuilder.Validate =>
[acheck] HandlerBuilder.Validate <= (SUCCESS)
[acheck] HandlerBuilder.Build =>
[acheck] HandlerBuilder.Build <= (SUCCESS)
[acheck] Handler.Close =>
[acheck] Handler.Close <= (SUCCESS)
`
	if strings.TrimSpace(l.String()) != strings.TrimSpace(expected) {
		t.Fatalf("Adapter log mismatch: '%v' != '%v'", l.String(), expected)
	}
}

func TestCleanup_NoChange(t *testing.T) {
	l := &data.Logger{}
	adapters := data.BuildAdapters(l, data.FakeAdapterSettings{Name: "acheck"})
	templates := data.BuildTemplates(nil)

	s := util.GetSnapshot(templates, adapters, data.ServiceConfig, globalCfg)

	table := NewTable(Empty(), s, nil)

	// use same config again.
	table2 := NewTable(table, s, nil)

	table.Cleanup(table2)

	// Close is not called
	expected := `
[acheck] NewBuilder =>
[acheck] NewBuilder <=
[acheck] HandlerBuilder.SetAdapterConfig => '&Struct{Fields:map[string]*Value{},}'
[acheck] HandlerBuilder.SetAdapterConfig <=
[acheck] HandlerBuilder.Validate =>
[acheck] HandlerBuilder.Validate <= (SUCCESS)
[acheck] HandlerBuilder.Build =>
[acheck] HandlerBuilder.Build <= (SUCCESS)
`
	if strings.TrimSpace(l.String()) != strings.TrimSpace(expected) {
		t.Fatalf("Adapter log mismatch: '%v' != '%v'", l.String(), expected)
	}
}

func TestCleanup_EmptyNewTable(t *testing.T) {
	adapters := data.BuildAdapters(nil)
	templates := data.BuildTemplates(nil)

	s := util.GetSnapshot(templates, adapters, data.ServiceConfig, globalCfg)

	table := NewTable(Empty(), s, nil)

	// Use an empty table as current.
	table.Cleanup(Empty())
}

func TestCleanup_WithStartupError(t *testing.T) {
	adapters := data.BuildAdapters(nil)

	templates := data.BuildTemplates(nil, data.FakeTemplateSettings{Name: "tcheck", HandlerDoesNotSupportTemplate: true})

	s := util.GetSnapshot(templates, adapters, data.ServiceConfig, globalCfg)
	table := NewTable(Empty(), s, nil)

	if _, found := table.Get(data.FqnACheck1); found {
		t.Fail()
	}

	// use different config to force cleanup
	table2 := NewTable(table, config.Empty(), nil)

	table.Cleanup(table2)

	// Close shouldn't be called.
	if len(table2.entries) != 0 {
		t.Fail()
	}
}

func TestCleanup_CloseError(t *testing.T) {
	l := &data.Logger{}
	adapters := data.BuildAdapters(l, data.FakeAdapterSettings{Name: "acheck", ErrorAtHandlerClose: true})
	templates := data.BuildTemplates(nil)

	s := util.GetSnapshot(templates, adapters, data.ServiceConfig, globalCfg)
	table := NewTable(Empty(), s, nil)

	// use different config to force cleanup
	table2 := NewTable(table, config.Empty(), nil)

	table.Cleanup(table2)

	if len(table2.entries) != 0 {
		t.Fail()
	}

	// Close is called, error is returned.
	expected := `
[acheck] NewBuilder =>
[acheck] NewBuilder <=
[acheck] HandlerBuilder.SetAdapterConfig => '&Struct{Fields:map[string]*Value{},}'
[acheck] HandlerBuilder.SetAdapterConfig <=
[acheck] HandlerBuilder.Validate =>
[acheck] HandlerBuilder.Validate <= (SUCCESS)
[acheck] HandlerBuilder.Build =>
[acheck] HandlerBuilder.Build <= (SUCCESS)
[acheck] Handler.Close =>
[acheck] Handler.Close <= (ERROR)
`
	if strings.TrimSpace(l.String()) != strings.TrimSpace(expected) {
		t.Fatalf("Adapter log mismatch: '%v' != '%v'", l.String(), expected)
	}
}

func TestCleanup_ClosePanic(t *testing.T) {
	l := &data.Logger{}
	adapters := data.BuildAdapters(l, data.FakeAdapterSettings{Name: "acheck", PanicAtHandlerClose: true})
	templates := data.BuildTemplates(nil)

	s := util.GetSnapshot(templates, adapters, data.ServiceConfig, globalCfg)
	table := NewTable(Empty(), s, nil)

	// use different config to force cleanup
	table2 := NewTable(table, config.Empty(), nil)

	table.Cleanup(table2)

	if len(table2.entries) != 0 {
		t.Fail()
	}

	// Close is called, error is returned.
	expected := `
[acheck] NewBuilder =>
[acheck] NewBuilder <=
[acheck] HandlerBuilder.SetAdapterConfig => '&Struct{Fields:map[string]*Value{},}'
[acheck] HandlerBuilder.SetAdapterConfig <=
[acheck] HandlerBuilder.Validate =>
[acheck] HandlerBuilder.Validate <= (SUCCESS)
[acheck] HandlerBuilder.Build =>
[acheck] HandlerBuilder.Build <= (SUCCESS)
[acheck] Handler.Close =>
[acheck] Handler.Close <= (PANIC)
`
	if strings.TrimSpace(l.String()) != strings.TrimSpace(expected) {
		t.Fatalf("Adapter log mismatch: '%v' != '%v'", l.String(), expected)
	}
}
