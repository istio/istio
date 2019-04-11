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

package runtime

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/gogo/protobuf/types"

	"istio.io/istio/galley/pkg/meshconfig"
	"istio.io/istio/galley/pkg/runtime/groups"
	runtimeLog "istio.io/istio/galley/pkg/runtime/log"
	"istio.io/istio/galley/pkg/runtime/publish"
	"istio.io/istio/galley/pkg/runtime/resource"
	"istio.io/istio/galley/pkg/testing/resources"
	"istio.io/istio/pkg/log"
	"istio.io/istio/pkg/mcp/snapshot"
	"istio.io/istio/pkg/util/wait"
)

const (
	timeout = 5 * time.Second
)

func TestProcessor_Start(t *testing.T) {
	// Set the log level to debug for codecov.
	prevLevel := setDebugLogLevel()
	defer restoreLogLevel(prevLevel)

	src := NewInMemorySource()
	distributor := snapshot.New(groups.IndexFunction)
	p := NewProcessor(src, distributor, cfg)

	err := p.Start()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Try starting again
	err = p.Start()
	if err == nil {
		t.Fatal("second start should have caused an error")
	}
}

type erroneousSource struct{}

func (e *erroneousSource) Start(_ resource.EventHandler) error {
	return errors.New("cheese not found")
}
func (e *erroneousSource) Stop() {}

func TestProcessor_Start_Error(t *testing.T) {
	// Set the log level to debug for codecov.
	prevLevel := setDebugLogLevel()
	defer restoreLogLevel(prevLevel)

	distributor := snapshot.New(groups.IndexFunction)
	cfg := &Config{Mesh: meshconfig.NewInMemory()}
	p := NewProcessor(&erroneousSource{}, distributor, cfg)

	err := p.Start()
	if err == nil {
		t.Fatal("expected error not found")
	}
}

func TestProcessor_Stop(t *testing.T) {
	// Set the log level to debug for codecov.
	prevLevel := setDebugLogLevel()
	defer restoreLogLevel(prevLevel)

	src := NewInMemorySource()
	distributor := snapshot.New(groups.IndexFunction)
	stateStrategy := publish.NewStrategyWithDefaults()

	p := newTestProcessor(src, stateStrategy, distributor, nil)

	err := p.Start()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	p.Stop()

	// Second reset shouldn't crash
	p.Stop()
}

func TestProcessor_EventAccumulation(t *testing.T) {
	// Set the log level to debug for codecov.
	prevLevel := setDebugLogLevel()
	defer restoreLogLevel(prevLevel)

	src := NewInMemorySource()
	distributor := NewInMemoryDistributor()
	// Do not quiesce/timeout for an hour
	stateStrategy := publish.NewStrategy(time.Hour, time.Hour, time.Millisecond)

	p := newTestProcessor(src, stateStrategy, distributor, nil)
	err := p.Start()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer p.Stop()

	awaitFullSync(t, p)

	k1 := resource.Key{Collection: resources.EmptyInfo.Collection, FullName: resource.FullNameFromNamespaceAndName("", "r1")}
	src.Set(k1, resource.Metadata{}, &types.Empty{})

	// Wait "long enough"
	time.Sleep(time.Second * 1)

	if distributor.NumSnapshots() != 0 {
		t.Fatalf("snapshot shouldn't have been distributed: %+v", distributor)
	}
}

func TestProcessor_EventAccumulation_WithFullSync(t *testing.T) {
	// Set the log level to debug for codecov.
	prevLevel := setDebugLogLevel()
	defer restoreLogLevel(prevLevel)

	info, _ := resources.TestSchema.Lookup("empty")

	src := NewInMemorySource()
	distributor := NewInMemoryDistributor()
	// Do not quiesce/timeout for an hour
	stateStrategy := publish.NewStrategy(time.Hour, time.Hour, time.Millisecond)

	p := newTestProcessor(src, stateStrategy, distributor, nil)
	err := p.Start()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer p.Stop()

	awaitFullSync(t, p)

	k1 := resource.Key{Collection: info.Collection, FullName: resource.FullNameFromNamespaceAndName("", "r1")}
	src.Set(k1, resource.Metadata{}, &types.Empty{})

	// Wait "long enough"
	time.Sleep(time.Second * 1)

	if distributor.NumSnapshots() != 0 {
		t.Fatalf("snapshot shouldn't have been distributed: %+v", distributor)
	}
}

func TestProcessor_Publishing(t *testing.T) {
	// Set the log level to debug for codecov.
	prevLevel := setDebugLogLevel()
	defer restoreLogLevel(prevLevel)

	info, _ := resources.TestSchema.Lookup("empty")

	src := NewInMemorySource()
	distributor := NewInMemoryDistributor()
	stateStrategy := publish.NewStrategy(time.Millisecond, time.Millisecond, time.Microsecond)

	processCallCount := &sync.WaitGroup{}
	hookFn := func() {
		processCallCount.Done()
	}
	processCallCount.Add(3) // 1 for add, 1 for sync, 1 for publish trigger

	p := newTestProcessor(src, stateStrategy, distributor, hookFn)
	err := p.Start()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer p.Stop()

	awaitFullSync(t, p)

	k1 := resource.Key{Collection: info.Collection, FullName: resource.FullNameFromNamespaceAndName("", "r1")}
	src.Set(k1, resource.Metadata{}, &types.Empty{})

	// Wait for up to 5 seconds.
	if err := wait.WithTimeout(processCallCount.Wait, timeout); err != nil {
		t.Fatal(err)
	}

	if distributor.NumSnapshots() != 1 {
		t.Fatalf("snapshot should have been distributed: %+v", distributor)
	}
}

func awaitFullSync(t *testing.T, p *Processor) {
	if err := p.AwaitFullSync(timeout); err != nil {
		t.Fatal(err)
	}
}

func newTestProcessor(src Source, stateStrategy *publish.Strategy,
	distributor Distributor, hookFn postProcessHookFn) *Processor {
	return newProcessor(src, cfg, resources.TestSchema, stateStrategy, distributor, hookFn)
}

func setDebugLogLevel() log.Level {
	prev := runtimeLog.Scope.GetOutputLevel()
	runtimeLog.Scope.SetOutputLevel(log.DebugLevel)
	return prev
}

func restoreLogLevel(level log.Level) {
	runtimeLog.Scope.SetOutputLevel(level)
}
