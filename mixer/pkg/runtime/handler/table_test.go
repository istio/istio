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

package handler

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"istio.io/istio/mixer/pkg/adapter"
	"istio.io/istio/mixer/pkg/runtime/config"
	"istio.io/istio/mixer/pkg/runtime/testing/data"
	"istio.io/istio/mixer/pkg/template"
	"istio.io/pkg/pool"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Create a standard global config with Handler H1, Instance I1 and rule R1 referencing I1 and H1.
// Most of the tests use this config.
var globalCfg = data.JoinConfigs(data.HandlerACheck1, data.InstanceCheck1, data.RuleCheck1)

//  Alternate configuration where R2 references H1, but uses instances I1 and I2.
var globalCfgI2 = data.JoinConfigs(data.HandlerACheck1, data.InstanceCheck1, data.InstanceCheck2, data.RuleCheck1WithInstance1And2)

func TestNew_EmptyConfig(t *testing.T) {
	s := config.Empty()

	table := NewTable(Empty(), s, nil, []string{metav1.NamespaceAll})
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

	s, _ := config.GetSnapshotForTest(templates, adapters, data.ServiceConfig, globalCfg)

	table := NewTable(Empty(), s, nil, []string{metav1.NamespaceAll})
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

	s, _ := config.GetSnapshotForTest(templates, adapters, data.ServiceConfig, globalCfg)

	table := NewTable(Empty(), s, nil, []string{metav1.NamespaceAll})

	// NewTable again using the same config, but add fault to the adapter to detect change.
	adapters = data.BuildAdapters(nil, data.FakeAdapterSettings{Name: "tcheck", ErrorAtBuild: true})
	s, _ = config.GetSnapshotForTest(templates, adapters, data.ServiceConfig, globalCfg)

	table2 := NewTable(table, s, nil, []string{metav1.NamespaceAll})

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

	s, _ := config.GetSnapshotForTest(templates, adapters, data.ServiceConfig, globalCfg)

	table := NewTable(Empty(), s, nil, []string{metav1.NamespaceAll})

	// NewTable again using the slightly different config
	s, _ = config.GetSnapshotForTest(templates, adapters, data.ServiceConfig, globalCfgI2)

	table2 := NewTable(table, s, nil, []string{metav1.NamespaceAll})

	if len(table2.entries) != 1 {
		t.Fatal("size")
	}

	if table2.entries[data.FqnACheck1] == table.entries[data.FqnACheck1] {
		t.Fail()
	}
}

func TestNew_NoReuse_DifferentConnectionConfig(t *testing.T) {
	templates := map[string]*template.Info{}
	adapters := map[string]*adapter.Info{}

	// Load base dynamic config, which includes listentry template and listbackend adapter config
	dynamicConfig, err := data.ReadConfigs("../../../template/listentry/template.yaml", "../../../test/listbackend/nosession.yaml")
	dynamicConfig = data.JoinConfigs(dynamicConfig, data.InstanceDynamic, data.RuleDynamic)

	// Join base dynamic config with dynamic handler
	config1 := data.JoinConfigs(dynamicConfig, data.ListHandler3)
	if err != nil {
		t.Fatalf("fail to load dynamic config: %v", err)
	}
	s, _ := config.GetSnapshotForTest(templates, adapters, data.ServiceConfig, config1)
	table := NewTable(Empty(), s, nil, []string{metav1.NamespaceAll})

	if len(table.entries) != 1 {
		t.Fatalf("got %v entries in route table, want 1", len(table.entries))
	}

	// Join base dynamic config with dynamic handler which has different connection address
	config2 := data.JoinConfigs(dynamicConfig, data.ListHandler3Addr)
	// NewTable again using the slightly different config
	s, _ = config.GetSnapshotForTest(templates, adapters, data.ServiceConfig, config2)

	table2 := NewTable(table, s, nil, []string{metav1.NamespaceAll})

	if len(table2.entries) != 1 {
		t.Fatalf("got %v entries in route table, want 1", len(table2.entries))
	}

	if table2.entries[data.FqdnListHandler3] == table.entries[data.FqdnListHandler3] {
		t.Fatalf("got same entry %+v in route table after handler config change, want different entries", table2.entries[data.FqdnListHandler3])
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

	s, _ := config.GetSnapshotForTest(templates, adapters, data.ServiceConfig, globalCfg)

	table := NewTable(Empty(), s, nil, []string{metav1.NamespaceAll})

	s = config.Empty()

	table2 := NewTable(table, s, nil, []string{metav1.NamespaceAll})

	table.Cleanup(table2)

	expected := `
[acheck] NewBuilder =>
[acheck] NewBuilder <=
[acheck] HandlerBuilder.SetAdapterConfig => '&Struct{Fields:map[string]*Value{},XXX_unrecognized:[],}'
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

func TestCleanup_WorkerNotClosed(t *testing.T) {
	tests := []struct {
		SpawnWorker            bool
		SpawnDaemon            bool
		CloseGoRoutines        bool
		wantWorkerStrayRoutine int64
		wantDaemonStrayRoutine int64
	}{
		{
			SpawnWorker:            true,
			SpawnDaemon:            true,
			CloseGoRoutines:        false,
			wantWorkerStrayRoutine: 1,
			wantDaemonStrayRoutine: 1,
		},
		{
			SpawnWorker:            true,
			SpawnDaemon:            true,
			CloseGoRoutines:        true,
			wantWorkerStrayRoutine: 0,
			wantDaemonStrayRoutine: 0,
		},
		{
			SpawnWorker:            true,
			SpawnDaemon:            false,
			CloseGoRoutines:        false,
			wantWorkerStrayRoutine: 1,
			wantDaemonStrayRoutine: 0,
		},
		{
			SpawnWorker:            false,
			SpawnDaemon:            true,
			CloseGoRoutines:        false,
			wantWorkerStrayRoutine: 0,
			wantDaemonStrayRoutine: 1,
		},
	}
	for idx, tt := range tests {
		t.Run(fmt.Sprintf("[%d]", idx), func(t *testing.T) {
			adapters := data.BuildAdapters(nil, data.FakeAdapterSettings{Name: "acheck", SpawnWorker: tt.SpawnWorker,
				SpawnDaemon: tt.SpawnDaemon, CloseGoRoutines: tt.CloseGoRoutines})
			templates := data.BuildTemplates(nil)

			s, _ := config.GetSnapshotForTest(templates, adapters, data.ServiceConfig, globalCfg)
			s.ID = int64(idx * 2)

			gp := pool.NewGoroutinePool(5, false)
			gp.AddWorkers(5)
			oldTable := NewTable(Empty(), s, gp, []string{metav1.NamespaceAll})
			oldTable.strayWorkersRetryDuration = 5 * time.Millisecond

			s = config.Empty()
			// Every iteration of this test is working with two different config snapshot (old and new). We need to
			// allocate distinct config ids to each of the config snapshot for all the iterations. Multiplying
			// by 2 and adding 1, give unique ids per iteration [(0,1), (1,2), ...]
			s.ID = int64(idx*2 + 1)

			newTable := NewTable(oldTable, s, nil, []string{metav1.NamespaceAll})

			oldTable.Cleanup(newTable)

			// give time for counters to get updated before validating them.
			time.Sleep(500 * time.Millisecond)

			gotWorkers := oldTable.entries["hcheck1.acheck.istio-system"].env.Workers()
			if gotWorkers != tt.wantWorkerStrayRoutine {
				t.Fatalf("got %v worker stray routines; wanted %v", gotWorkers, tt.wantWorkerStrayRoutine)
			}

			gotDaemons := oldTable.entries[data.FqnACheck1].env.Daemons()
			if gotDaemons != tt.wantDaemonStrayRoutine {
				t.Fatalf("got %v daemon stray routines; wanted %v", gotDaemons, tt.wantDaemonStrayRoutine)
			}
		})
	}
}

func TestCleanup_NoChange(t *testing.T) {
	l := &data.Logger{}
	adapters := data.BuildAdapters(l, data.FakeAdapterSettings{Name: "acheck"})
	templates := data.BuildTemplates(nil)

	s, _ := config.GetSnapshotForTest(templates, adapters, data.ServiceConfig, globalCfg)

	table := NewTable(Empty(), s, nil, []string{metav1.NamespaceAll})

	// use same config again.
	table2 := NewTable(table, s, nil, []string{metav1.NamespaceAll})

	table.Cleanup(table2)

	// Close is not called
	expected := `
[acheck] NewBuilder =>
[acheck] NewBuilder <=
[acheck] HandlerBuilder.SetAdapterConfig => '&Struct{Fields:map[string]*Value{},XXX_unrecognized:[],}'
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

	s, _ := config.GetSnapshotForTest(templates, adapters, data.ServiceConfig, globalCfg)

	table := NewTable(Empty(), s, nil, []string{metav1.NamespaceAll})

	// Use an empty table as current.
	table.Cleanup(Empty())
}

func TestCleanup_WithStartupError(t *testing.T) {
	adapters := data.BuildAdapters(nil)

	templates := data.BuildTemplates(nil, data.FakeTemplateSettings{Name: "tcheck", HandlerDoesNotSupportTemplate: true})

	s, _ := config.GetSnapshotForTest(templates, adapters, data.ServiceConfig, globalCfg)
	table := NewTable(Empty(), s, nil, []string{metav1.NamespaceAll})

	if _, found := table.Get(data.FqnACheck1); found {
		t.Fail()
	}

	// use different config to force cleanup
	table2 := NewTable(table, config.Empty(), nil, []string{metav1.NamespaceAll})

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

	s, _ := config.GetSnapshotForTest(templates, adapters, data.ServiceConfig, globalCfg)
	table := NewTable(Empty(), s, nil, []string{metav1.NamespaceAll})

	// use different config to force cleanup
	table2 := NewTable(table, config.Empty(), nil, []string{metav1.NamespaceAll})

	table.Cleanup(table2)

	if len(table2.entries) != 0 {
		t.Fail()
	}

	// Close is called, error is returned.
	expected := `
[acheck] NewBuilder =>
[acheck] NewBuilder <=
[acheck] HandlerBuilder.SetAdapterConfig => '&Struct{Fields:map[string]*Value{},XXX_unrecognized:[],}'
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

	s, _ := config.GetSnapshotForTest(templates, adapters, data.ServiceConfig, globalCfg)
	table := NewTable(Empty(), s, nil, []string{metav1.NamespaceAll})

	// use different config to force cleanup
	table2 := NewTable(table, config.Empty(), nil, []string{metav1.NamespaceAll})

	table.Cleanup(table2)

	if len(table2.entries) != 0 {
		t.Fail()
	}

	// Close is called, error is returned.
	expected := `
[acheck] NewBuilder =>
[acheck] NewBuilder <=
[acheck] HandlerBuilder.SetAdapterConfig => '&Struct{Fields:map[string]*Value{},XXX_unrecognized:[],}'
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
