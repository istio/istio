//  Copyright 2018 Istio Authors
//
//  Licensed under the Apache License, Version 2.0 (the "License");
//  you may not use this file except in compliance with the License.
//  You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
//  Unless required by applicable law or agreed to in writing, software
//  distributed under the License is distributed on an "AS IS" BASIS,
//  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//  See the License for the specific language governing permissions and
//  limitations under the License.

package runtime

import (
	"testing"

	"github.com/gogo/protobuf/types"

	"istio.io/istio/galley/pkg/runtime/resource"
	// Pull in gogo & golang Empty
	_ "github.com/golang/protobuf/ptypes/empty"
)

func TestState_Apply_Add(t *testing.T) {
	schema := resource.NewSchema()
	schema.Register("type.googleapis.com/google.protobuf.Empty", false)
	einfo, _ := schema.Lookup("type.googleapis.com/google.protobuf.Empty")

	s := newState(schema)

	e := resource.Event{
		Kind: resource.Added,
		ID:   resource.VersionedKey{Version: "v1", Key: resource.Key{TypeURL: einfo.TypeURL, FullName: "fn"}},
		Item: &types.Any{},
	}

	changed := s.apply(e)
	if !changed {
		t.Fatal("calling apply should have changed state.")
	}

	sn := s.buildSnapshot()
	r := sn.Resources(einfo.TypeURL.String())
	if len(r) != 1 {
		t.Fatal("Entry should have been registered in snapshot")
	}
	v := sn.Version(einfo.TypeURL.String())
	if v == "" {
		t.Fatal("Version should have been available")
	}
}

func TestState_Apply_Update(t *testing.T) {
	schema := resource.NewSchema()
	schema.Register("type.googleapis.com/google.protobuf.Empty", false)
	einfo, _ := schema.Lookup("type.googleapis.com/google.protobuf.Empty")

	s := newState(schema)

	e := resource.Event{
		Kind: resource.Added,
		ID:   resource.VersionedKey{Version: "v1", Key: resource.Key{TypeURL: einfo.TypeURL, FullName: "fn"}},
		Item: &types.Any{},
	}

	changed := s.apply(e)
	if !changed {
		t.Fatal("calling apply should have changed state.")
	}

	e = resource.Event{
		Kind: resource.Updated,
		ID:   resource.VersionedKey{Version: "v2", Key: resource.Key{TypeURL: einfo.TypeURL, FullName: "fn"}},
		Item: &types.Any{},
	}
	changed = s.apply(e)
	if !changed {
		t.Fatal("calling apply should have changed state.")
	}

	sn := s.buildSnapshot()
	r := sn.Resources(einfo.TypeURL.String())
	if len(r) != 1 {
		t.Fatal("Entry should have been registered in snapshot")
	}
	v := sn.Version(einfo.TypeURL.String())
	if v == "" {
		t.Fatal("Version should have been available")
	}
}

func TestState_Apply_Update_SameVersion(t *testing.T) {
	schema := resource.NewSchema()
	schema.Register("type.googleapis.com/google.protobuf.Empty", false)
	einfo, _ := schema.Lookup("type.googleapis.com/google.protobuf.Empty")

	s := newState(schema)

	e := resource.Event{
		Kind: resource.Added,
		ID:   resource.VersionedKey{Version: "v1", Key: resource.Key{TypeURL: einfo.TypeURL, FullName: "fn"}},
		Item: &types.Any{},
	}

	changed := s.apply(e)
	if !changed {
		t.Fatal("calling apply should have changed state.")
	}

	e = resource.Event{
		Kind: resource.Updated,
		ID:   resource.VersionedKey{Version: "v1", Key: resource.Key{TypeURL: einfo.TypeURL, FullName: "fn"}},
		Item: &types.Any{},
	}
	s.apply(e)

	changed = s.apply(e)
	if changed {
		t.Fatal("calling apply should not have changed state.")
	}
}

func TestState_Apply_Delete(t *testing.T) {
	schema := resource.NewSchema()
	schema.Register("type.googleapis.com/google.protobuf.Empty", false)
	einfo, _ := schema.Lookup("type.googleapis.com/google.protobuf.Empty")

	s := newState(schema)

	e := resource.Event{
		Kind: resource.Added,
		ID:   resource.VersionedKey{Version: "v1", Key: resource.Key{TypeURL: einfo.TypeURL, FullName: "fn"}},
		Item: &types.Any{},
	}

	changed := s.apply(e)
	if !changed {
		t.Fatal("calling apply should have changed state.")
	}

	e = resource.Event{
		Kind: resource.Deleted,
		ID:   resource.VersionedKey{Version: "v2", Key: resource.Key{TypeURL: einfo.TypeURL, FullName: "fn"}},
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
	schema := resource.NewSchema()
	schema.Register("type.googleapis.com/google.protobuf.Empty", false)
	einfo, _ := schema.Lookup("type.googleapis.com/google.protobuf.Empty")

	s := newState(schema)

	e := resource.Event{
		Kind: resource.EventKind(42),
		ID:   resource.VersionedKey{Version: "v1", Key: resource.Key{TypeURL: einfo.TypeURL, FullName: "fn"}},
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
	schema := resource.NewSchema()
	schema.Register("type.googleapis.com/google.protobuf.Empty", false)
	einfo, _ := schema.Lookup("type.googleapis.com/google.protobuf.Empty")

	s := newState(schema)

	e := resource.Event{
		Kind: resource.Added,
		ID:   resource.VersionedKey{Version: "v1", Key: resource.Key{TypeURL: einfo.TypeURL, FullName: "fn"}},
		Item: nil,
	}
	changed := s.apply(e)
	if changed {
		t.Fatal("calling apply should not have changed state.")
	}

	sn := s.buildSnapshot()
	r := sn.Resources(einfo.TypeURL.String())
	if len(r) != 0 {
		t.Fatal("Entry should have not been in snapshot")
	}
}

func TestState_String(t *testing.T) {
	schema := resource.NewSchema()
	schema.Register("type.googleapis.com/google.protobuf.Empty", false)
	einfo, _ := schema.Lookup("type.googleapis.com/google.protobuf.Empty")

	s := newState(schema)

	e := resource.Event{
		Kind: resource.Added,
		ID:   resource.VersionedKey{Version: "v1", Key: resource.Key{TypeURL: einfo.TypeURL, FullName: "fn"}},
		Item: nil,
	}
	_ = s.apply(e)

	// Should not crash
	_ = s.String()
}
