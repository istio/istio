//  Copyright 2018 Istio Authors
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
	"istio.io/istio/galley/pkg/runtime/resource"
)

var (
	fakeCreateTime0 time.Time
	fakeCreateTime1 time.Time
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

func checkCreateTime(e *mcp.Envelope, want time.Time) error {
	got, err := types.TimestampFromProto(e.Metadata.CreateTime)
	if err != nil {
		return fmt.Errorf("failed to decode: %v", err)
	}
	if !got.Equal(want) {
		return fmt.Errorf("wrong time: got %q want %q", got, want)
	}
	return nil
}

func TestState_Apply_Add(t *testing.T) {

	s := newState(testSchema)

	e := resource.Event{
		Kind: resource.Added,
		ID: resource.VersionedKey{
			Version:    "v1",
			Key:        resource.Key{TypeURL: emptyInfo.TypeURL, FullName: "fn"},
			CreateTime: fakeCreateTime0,
		},
		Item: &types.Any{},
	}

	changed := s.apply(e)
	if !changed {
		t.Fatal("calling apply should have changed state.")
	}

	sn := s.buildSnapshot()
	r := sn.Resources(emptyInfo.TypeURL.String())
	if len(r) != 1 {
		t.Fatal("Entry should have been registered in snapshot")
	}
	if err := checkCreateTime(r[0], fakeCreateTime0); err != nil {
		t.Fatalf("Bad create time: %v", err)
	}
	v := sn.Version(emptyInfo.TypeURL.String())
	if v == "" {
		t.Fatal("Version should have been available")
	}
}

func TestState_Apply_Update(t *testing.T) {
	s := newState(testSchema)

	e := resource.Event{
		Kind: resource.Added,
		ID: resource.VersionedKey{
			Version:    "v1",
			Key:        resource.Key{TypeURL: emptyInfo.TypeURL, FullName: "fn"},
			CreateTime: fakeCreateTime0,
		},
		Item: &types.Any{},
	}

	changed := s.apply(e)
	if !changed {
		t.Fatal("calling apply should have changed state.")
	}

	e = resource.Event{
		Kind: resource.Updated,
		ID: resource.VersionedKey{
			Version:    "v2",
			Key:        resource.Key{TypeURL: emptyInfo.TypeURL, FullName: "fn"},
			CreateTime: fakeCreateTime1,
		},
		Item: &types.Any{},
	}
	changed = s.apply(e)
	if !changed {
		t.Fatal("calling apply should have changed state.")
	}

	sn := s.buildSnapshot()
	r := sn.Resources(emptyInfo.TypeURL.String())
	if len(r) != 1 {
		t.Fatal("Entry should have been registered in snapshot")
	}
	if err := checkCreateTime(r[0], fakeCreateTime1); err != nil {
		t.Fatalf("Bad create time: %v", err)
	}
	v := sn.Version(emptyInfo.TypeURL.String())
	if v == "" {
		t.Fatal("Version should have been available")
	}
}

func TestState_Apply_Update_SameVersion(t *testing.T) {
	s := newState(testSchema)

	e := resource.Event{
		Kind: resource.Added,
		ID: resource.VersionedKey{
			Version:    "v1",
			Key:        resource.Key{TypeURL: emptyInfo.TypeURL, FullName: "fn"},
			CreateTime: fakeCreateTime0,
		},
		Item: &types.Any{},
	}

	changed := s.apply(e)
	if !changed {
		t.Fatal("calling apply should have changed state.")
	}

	e = resource.Event{
		Kind: resource.Updated,
		ID: resource.VersionedKey{
			Version:    "v1",
			Key:        resource.Key{TypeURL: emptyInfo.TypeURL, FullName: "fn"},
			CreateTime: fakeCreateTime1,
		},
		Item: &types.Any{},
	}
	s.apply(e)

	changed = s.apply(e)
	if changed {
		t.Fatal("calling apply should not have changed state.")
	}
}

func TestState_Apply_Delete(t *testing.T) {
	s := newState(testSchema)

	e := resource.Event{
		Kind: resource.Added,
		ID:   resource.VersionedKey{Version: "v1", Key: resource.Key{TypeURL: emptyInfo.TypeURL, FullName: "fn"}},
		Item: &types.Any{},
	}

	changed := s.apply(e)
	if !changed {
		t.Fatal("calling apply should have changed state.")
	}

	e = resource.Event{
		Kind: resource.Deleted,
		ID:   resource.VersionedKey{Version: "v2", Key: resource.Key{TypeURL: emptyInfo.TypeURL, FullName: "fn"}},
	}
	s.apply(e)

	changed = s.apply(e)
	if !changed {
		t.Fatal("calling apply should have changed state.")
	}

	sn := s.buildSnapshot()
	r := sn.Resources("mn")
	if len(r) != 0 {
		t.Fatal("Entry should have not been in snapshot")
	}
}

func TestState_Apply_UnknownEventKind(t *testing.T) {
	s := newState(testSchema)

	e := resource.Event{
		Kind: resource.EventKind(42),
		ID:   resource.VersionedKey{Version: "v1", Key: resource.Key{TypeURL: emptyInfo.TypeURL, FullName: "fn"}},
		Item: &types.Any{},
	}
	changed := s.apply(e)
	if changed {
		t.Fatal("calling apply should not have changed state.")
	}

	sn := s.buildSnapshot()
	r := sn.Resources("mn")
	if len(r) != 0 {
		t.Fatal("Entry should have not been in snapshot")
	}
}

func TestState_Apply_BrokenProto(t *testing.T) {
	s := newState(testSchema)

	e := resource.Event{
		Kind: resource.Added,
		ID:   resource.VersionedKey{Version: "v1", Key: resource.Key{TypeURL: emptyInfo.TypeURL, FullName: "fn"}},
		Item: nil,
	}
	changed := s.apply(e)
	if changed {
		t.Fatal("calling apply should not have changed state.")
	}

	sn := s.buildSnapshot()
	r := sn.Resources(emptyInfo.TypeURL.String())
	if len(r) != 0 {
		t.Fatal("Entry should have not been in snapshot")
	}
}

func TestState_String(t *testing.T) {
	s := newState(testSchema)

	e := resource.Event{
		Kind: resource.Added,
		ID:   resource.VersionedKey{Version: "v1", Key: resource.Key{TypeURL: emptyInfo.TypeURL, FullName: "fn"}},
		Item: nil,
	}
	_ = s.apply(e)

	// Should not crash
	_ = s.String()
}
