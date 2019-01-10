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

	"istio.io/istio/galley/pkg/runtime/resource"
	"istio.io/istio/pkg/mcp/snapshot"
)

var testSchema = func() *resource.Schema {
	b := resource.NewSchemaBuilder()
	b.Register("empty", "type.googleapis.com/google.protobuf.Empty")
	b.Register("struct", "type.googleapis.com/google.protobuf.Struct")
	return b.Build()
}()

var (
	emptyInfo  = testSchema.Get("empty")
	structInfo = testSchema.Get("struct")
)

func TestProcessor_Start(t *testing.T) {
	src := NewInMemorySource()
	distributor := snapshot.New(snapshot.DefaultGroupIndex)
	cfg := &Config{Mesh: meshconfig.NewInMemory()}
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

func (e *erroneousSource) Start() (chan resource.Event, error) {
	return nil, errors.New("cheese not found")
}
func (e *erroneousSource) Stop() {}

func TestProcessor_Start_Error(t *testing.T) {
	distributor := snapshot.New(snapshot.DefaultGroupIndex)
	cfg := &Config{Mesh: meshconfig.NewInMemory()}
	p := NewProcessor(&erroneousSource{}, distributor, cfg)

	err := p.Start()
	if err == nil {
		t.Fatal("expected error not found")
	}
}

func TestProcessor_Stop(t *testing.T) {
	src := NewInMemorySource()
	distributor := snapshot.New(snapshot.DefaultGroupIndex)
	strategy := newPublishingStrategyWithDefaults()
	cfg := &Config{Mesh: meshconfig.NewInMemory()}

	p := newProcessor(src, distributor, cfg, strategy, testSchema, nil)

	err := p.Start()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	p.Stop()

	// Second reset shouldn't crash
	p.Stop()
}

func TestProcessor_EventAccumulation(t *testing.T) {
	src := NewInMemorySource()
	distributor := NewInMemoryDistributor()
	// Do not quiesce/timeout for an hour
	strategy := newPublishingStrategy(time.Hour, time.Hour, time.Millisecond)
	cfg := &Config{Mesh: meshconfig.NewInMemory()}

	p := newProcessor(src, distributor, cfg, strategy, testSchema, nil)
	err := p.Start()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	k1 := resource.Key{Collection: emptyInfo.Collection, FullName: resource.FullNameFromNamespaceAndName("", "r1")}
	src.Set(k1, &types.Empty{})

	// Wait "long enough"
	time.Sleep(time.Millisecond * 10)

	if len(distributor.snapshots) != 0 {
		t.Fatalf("snapshot shouldn't have been distributed: %+v", distributor.snapshots)
	}
}

func TestProcessor_EventAccumulation_WithFullSync(t *testing.T) {
	info, _ := testSchema.Lookup("type.googleapis.com/google.protobuf.Empty")

	src := NewInMemorySource()
	distributor := NewInMemoryDistributor()
	// Do not quiesce/timeout for an hour
	strategy := newPublishingStrategy(time.Hour, time.Hour, time.Millisecond)
	cfg := &Config{Mesh: meshconfig.NewInMemory()}

	p := newProcessor(src, distributor, cfg, strategy, testSchema, nil)
	err := p.Start()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	k1 := resource.Key{Collection: info.Collection, FullName: resource.FullNameFromNamespaceAndName("", "r1")}
	src.Set(k1, &types.Empty{})

	// Wait "long enough"
	time.Sleep(time.Millisecond * 10)

	if len(distributor.snapshots) != 0 {
		t.Fatalf("snapshot shouldn't have been distributed: %+v", distributor.snapshots)
	}
}

func TestProcessor_Publishing(t *testing.T) {
	info, _ := testSchema.Lookup("type.googleapis.com/google.protobuf.Empty")

	src := NewInMemorySource()
	distributor := NewInMemoryDistributor()
	strategy := newPublishingStrategy(time.Millisecond, time.Millisecond, time.Microsecond)
	cfg := &Config{Mesh: meshconfig.NewInMemory()}

	processCallCount := sync.WaitGroup{}
	hookFn := func() {
		processCallCount.Done()
	}
	processCallCount.Add(3) // 1 for add, 1 for sync, 1 for publish trigger

	p := newProcessor(src, distributor, cfg, strategy, testSchema, hookFn)
	err := p.Start()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	k1 := resource.Key{Collection: info.Collection, FullName: resource.FullNameFromNamespaceAndName("", "r1")}
	src.Set(k1, &types.Empty{})

	processCallCount.Wait()

	if len(distributor.snapshots) != 1 {
		t.Fatalf("snapshot should have been distributed: %+v", distributor.snapshots)
	}
}
