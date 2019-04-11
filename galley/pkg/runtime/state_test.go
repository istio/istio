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
	"fmt"
	"testing"
	"time"

	"github.com/gogo/protobuf/types"

	mcp "istio.io/api/mcp/v1alpha1"
	"istio.io/istio/galley/pkg/meshconfig"
	"istio.io/istio/galley/pkg/runtime/processing"
	"istio.io/istio/galley/pkg/runtime/publish"
	"istio.io/istio/galley/pkg/runtime/resource"
	"istio.io/istio/galley/pkg/testing/resources"
)

var (
	fakeCreateTime0 time.Time
	fakeCreateTime1 time.Time

	cfg = &Config{Mesh: meshconfig.NewInMemory()}
	fn  = resource.FullNameFromNamespaceAndName("", "fn")
)

func init() {
	var err error
	fakeCreateTime0, err = time.Parse(time.RFC3339, "2009-02-04T21:00:57-08:00")
	if err != nil {
		panic("could not create fake create_time for test")
	}
	fakeCreateTime1, err = time.Parse(time.RFC3339, "2009-02-04T21:00:58-08:00")
	if err != nil {
		panic("could not create fake create_time for test")
	}
}

func checkCreateTime(e *mcp.Resource, want time.Time) error {
	got, err := types.TimestampFromProto(e.Metadata.CreateTime)
	if err != nil {
		return fmt.Errorf("failed to decode: %v", err)
	}
	if !got.Equal(want) {
		return fmt.Errorf("wrong time: got %q want %q", got, want)
	}
	return nil
}

func TestState_DefaultSnapshot(t *testing.T) {
	s := newTestState()
	sn := s.buildSnapshot()

	for _, collection := range []string{resources.EmptyInfo.Collection.String(), resources.StructInfo.Collection.String()} {
		if r := sn.Resources(collection); len(r) != 0 {
			t.Fatalf("%s entry should have been registered in snapshot", collection)
		}
		if v := sn.Version(collection); v == "" {
			t.Fatalf("%s version should have been available", collection)
		}
	}

	e := resource.Event{
		Kind: resource.Added,
		Entry: resource.Entry{
			ID: resource.VersionedKey{
				Version: "v1",
				Key:     resource.Key{Collection: resources.EmptyInfo.Collection, FullName: fn},
			},
			Metadata: resource.Metadata{
				CreateTime: fakeCreateTime0,
			},
			Item: &types.Any{},
		},
	}

	s.Handle(e)
	if s.pendingEvents != 1 {
		t.Fatal("calling apply should have changed State.")
	}

	sn = s.buildSnapshot()
	r := sn.Resources(resources.EmptyInfo.Collection.String())
	if len(r) != 1 {
		t.Fatal("Entry should have been registered in snapshot")
	}
	if err := checkCreateTime(r[0], fakeCreateTime0); err != nil {
		t.Fatalf("Bad create time: %v", err)
	}
	v := sn.Version(resources.EmptyInfo.Collection.String())
	if v == "" {
		t.Fatal("Version should have been available")
	}
}

func TestState_Apply_Update(t *testing.T) {
	s := newTestState()

	e := resource.Event{
		Kind: resource.Added,
		Entry: resource.Entry{
			ID: resource.VersionedKey{
				Version: "v1",
				Key:     resource.Key{Collection: resources.EmptyInfo.Collection, FullName: fn},
			},
			Metadata: resource.Metadata{
				CreateTime: fakeCreateTime0,
			},
			Item: &types.Any{},
		},
	}

	s.Handle(e)
	if s.pendingEvents != 1 {
		t.Fatal("calling apply should have changed State.")
	}

	e = resource.Event{
		Kind: resource.Updated,
		Entry: resource.Entry{
			ID: resource.VersionedKey{
				Version: "v2",
				Key:     resource.Key{Collection: resources.EmptyInfo.Collection, FullName: fn},
			},
			Metadata: resource.Metadata{
				CreateTime: fakeCreateTime1,
			},
			Item: &types.Any{},
		},
	}
	s.Handle(e)
	if s.pendingEvents != 2 {
		t.Fatal("calling apply should have changed State.")
	}

	sn := s.buildSnapshot()
	r := sn.Resources(resources.EmptyInfo.Collection.String())
	if len(r) != 1 {
		t.Fatal("Entry should have been registered in snapshot")
	}
	if err := checkCreateTime(r[0], fakeCreateTime1); err != nil {
		t.Fatalf("Bad create time: %v", err)
	}
	v := sn.Version(resources.EmptyInfo.Collection.String())
	if v == "" {
		t.Fatal("Version should have been available")
	}
}

func TestState_Apply_Update_SameVersion(t *testing.T) {
	s := newTestState()

	e := resource.Event{
		Kind: resource.Added,
		Entry: resource.Entry{
			ID: resource.VersionedKey{
				Version: "v1",
				Key:     resource.Key{Collection: resources.EmptyInfo.Collection, FullName: fn},
			},
			Metadata: resource.Metadata{
				CreateTime: fakeCreateTime0,
			},
			Item: &types.Any{},
		},
	}

	s.Handle(e)
	if s.pendingEvents != 1 {
		t.Fatal("calling apply should have changed State.")
	}

	e = resource.Event{
		Kind: resource.Updated,
		Entry: resource.Entry{
			ID: resource.VersionedKey{
				Version: "v1",
				Key:     resource.Key{Collection: resources.EmptyInfo.Collection, FullName: fn},
			},
			Metadata: resource.Metadata{
				CreateTime: fakeCreateTime1,
			},
			Item: &types.Any{},
		},
	}
	s.Handle(e)

	s.Handle(e)
	if s.pendingEvents != 1 {
		t.Fatal("calling apply should not have changed State.")
	}
}

func TestState_Apply_Delete(t *testing.T) {
	s := newTestState()

	e := resource.Event{
		Kind: resource.Added,
		Entry: resource.Entry{
			ID:   resource.VersionedKey{Version: "v1", Key: resource.Key{Collection: resources.EmptyInfo.Collection, FullName: fn}},
			Item: &types.Any{},
		},
	}

	s.Handle(e)
	if s.pendingEvents != 1 {
		t.Fatal("calling apply should have changed State.")
	}

	e = resource.Event{
		Kind: resource.Deleted,
		Entry: resource.Entry{
			ID: resource.VersionedKey{Version: "v2", Key: resource.Key{Collection: resources.EmptyInfo.Collection, FullName: fn}},
		},
	}
	s.Handle(e)

	s.Handle(e)
	if s.pendingEvents != 3 {
		t.Fatal("calling apply should have changed State.")
	}

	sn := s.buildSnapshot()
	r := sn.Resources("mn")
	if len(r) != 0 {
		t.Fatal("Entry should have not been in snapshot")
	}
}

func TestState_Apply_UnknownEventKind(t *testing.T) {
	s := newTestState()

	e := resource.Event{
		Kind: resource.EventKind(42),
		Entry: resource.Entry{
			ID:   resource.VersionedKey{Version: "v1", Key: resource.Key{Collection: resources.EmptyInfo.Collection, FullName: fn}},
			Item: &types.Any{},
		},
	}
	s.Handle(e)
	if s.pendingEvents > 0 {
		t.Fatal("calling apply should not have changed State.")
	}

	sn := s.buildSnapshot()
	r := sn.Resources("mn")
	if len(r) != 0 {
		t.Fatal("Entry should have not been in snapshot")
	}
}

func TestState_Apply_BrokenProto(t *testing.T) {
	s := newTestState()

	e := resource.Event{
		Kind: resource.Added,
		Entry: resource.Entry{
			ID:   resource.VersionedKey{Version: "v1", Key: resource.Key{Collection: resources.EmptyInfo.Collection, FullName: fn}},
			Item: nil,
		},
	}
	s.Handle(e)
	if s.pendingEvents > 0 {
		t.Fatal("calling apply should not have changed State.")
	}

	sn := s.buildSnapshot()
	r := sn.Resources(resources.EmptyInfo.Collection.String())
	if len(r) != 0 {
		t.Fatal("Entry should have not been in snapshot")
	}
}

func TestState_String(t *testing.T) {
	s := newTestState()

	e := resource.Event{
		Kind: resource.Added,
		Entry: resource.Entry{
			ID:   resource.VersionedKey{Version: "v1", Key: resource.Key{Collection: resources.EmptyInfo.Collection, FullName: fn}},
			Item: nil,
		},
	}
	s.Handle(e)

	// Should not crash
	_ = s.String()
}

func newTestState() *State {
	stateStrategy := publish.NewStrategyWithDefaults()
	stateListener := processing.ListenerFromFn(func(c resource.Collection) {
		// When the state indicates a change occurred, update the publishing strategy
		stateStrategy.OnChange()
	})
	return newState(resources.TestSchema, cfg, stateListener)
}
