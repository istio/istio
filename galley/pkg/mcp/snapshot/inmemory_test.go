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

package snapshot

import (
	"reflect"
	"testing"

	"github.com/gogo/protobuf/types"

	mcp "istio.io/api/mcp/v1alpha1"
)

func TestNewInMemory(t *testing.T) {
	sn := NewInMemory()
	if sn.versions == nil || len(sn.versions) != 0 {
		t.Fatalf("Unexpected initial versions: %v", sn.versions)
	}
	if sn.envelopes == nil || len(sn.envelopes) != 0 {
		t.Fatalf("Unexpected initial envelopes: %v", sn.envelopes)
	}
}

func TestInMemory_Set(t *testing.T) {
	sn := NewInMemory()
	items := []*mcp.Envelope{{Resource: &types.Any{}, Metadata: &mcp.Metadata{Name: "foo"}}}
	sn.Set("type", "version", items)

	if v, ok := sn.versions["type"]; !ok || v != "version" {
		t.Fatalf("Unexpected version table after set: %+v", sn.versions)
	}

	if r, ok := sn.envelopes["type"]; !ok || !reflect.DeepEqual(r, items) {
		t.Fatalf("Unexpected resource table after set: %+v", sn.envelopes)
	}
}

func TestInMemory_Resources(t *testing.T) {
	sn := NewInMemory()
	items := []*mcp.Envelope{{Resource: &types.Any{}, Metadata: &mcp.Metadata{Name: "foo"}}}
	sn.Set("type", "version", items)

	r := sn.Resources("type")
	if !reflect.DeepEqual(items, r) {
		t.Fatalf("Unexpected envelopes: %+v, expected: %+v", r, items)
	}
}

func TestInMemory_Version(t *testing.T) {
	sn := NewInMemory()
	items := []*mcp.Envelope{{Resource: &types.Any{}, Metadata: &mcp.Metadata{Name: "foo"}}}
	sn.Set("type", "version", items)

	v := sn.Version("type")
	if v != "version" {
		t.Fatalf("Unexpected version: %s, expected: 'version'", v)
	}
}

func TestInMemory_Freeze(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("set should have panicked")
		}
	}()

	sn := NewInMemory()
	sn.Freeze()
	items := []*mcp.Envelope{{Resource: &types.Any{}, Metadata: &mcp.Metadata{Name: "foo"}}}
	sn.Set("type", "version", items)
}
