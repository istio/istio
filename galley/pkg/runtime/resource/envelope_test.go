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

package resource

import (
	"errors"
	"math"
	"reflect"
	"testing"
	"time"

	"github.com/gogo/protobuf/proto"
	"github.com/gogo/protobuf/types"
	"github.com/golang/protobuf/ptypes/empty"
)

func TestEnvelope_Basic(t *testing.T) {
	e := Entry{
		ID: VersionedKey{
			Version: "v1",
			Key: Key{
				TypeURL:  TypeURL{"type.googleapis.com/google.protobuf.Empty"},
				FullName: FullNameFromNamespaceAndName("ns1", "res1"),
			},
		},
		Metadata: Metadata{
			CreateTime: time.Unix(1, 1).UTC(),
		},
		Item: &types.Empty{},
	}

	env, err := ToMcpResource(e)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if env.Metadata.Name != e.ID.FullName.String() {
		t.Fatalf("unexpected name: %v", env.Metadata.Name)
	}

	if env.Metadata.Version != string(e.ID.Version) {
		t.Fatalf("unexpected version: %v", env.Metadata.Version)
	}

	if env.Metadata.CreateTime == nil {
		t.Fatal("CreateTime is nil")
	}

	if env.Body == nil {
		t.Fatal("Resource is nil is nil")
	}

	b := NewSchemaBuilder()
	b.Register("type.googleapis.com/google.protobuf.Empty")
	s := b.Build()
	ext, err := FromMcpResource(s, env)
	if err != nil {
		t.Fatalf("Unexpected error when extracting: %v", err)
	}

	if !reflect.DeepEqual(ext.ID, e.ID) {
		t.Fatalf("mismatch: got:%v, wanted: %v", ext, e)
	}
}

func TestEnvelope_MarshalError(t *testing.T) {
	e := Entry{
		ID: VersionedKey{
			Version: "v1",
			Key: Key{
				TypeURL:  TypeURL{"type.googleapis.com/google.protobuf.Empty"},
				FullName: FullNameFromNamespaceAndName("ns1", "res1"),
			},
		},
		Metadata: Metadata{
			CreateTime: time.Unix(1, 1).UTC(),
		},
		Item: &invalidProto{},
	}

	_, err := ToMcpResource(e)
	if err == nil {
		t.Fatal("expected error not found")
	}
}

func TestEnvelope_TimestampError(t *testing.T) {
	e := Entry{
		ID: VersionedKey{
			Version: "v1",
			Key: Key{
				TypeURL:  TypeURL{"type.googleapis.com/google.protobuf.Empty"},
				FullName: FullNameFromNamespaceAndName("ns1", "res1"),
			},
		},
		Metadata: Metadata{
			CreateTime: time.Unix(math.MinInt64, math.MinInt64).UTC(),
		},
		Item: &types.Empty{},
	}
	_, err := ToMcpResource(e)
	if err == nil {
		t.Fatal("expected error not found")
	}
}

func TestExtract_NoRegisteredUrl(t *testing.T) {
	e := Entry{
		ID: VersionedKey{
			Version: "v1",
			Key: Key{
				TypeURL:  TypeURL{"type.googleapis.com/google.protobuf.Empty"},
				FullName: FullNameFromNamespaceAndName("ns1", "res1"),
			},
		},
		Metadata: Metadata{
			CreateTime: time.Unix(1, 1).UTC(),
		},
		Item: &types.Empty{},
	}

	env, err := ToMcpResource(e)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	b := NewSchemaBuilder()
	s := b.Build()
	if _, err = FromMcpResource(s, env); err == nil {
		t.Fatalf("expected error not found")
	}
}

func TestExtract_InvalidTimestamp(t *testing.T) {
	e := Entry{
		ID: VersionedKey{
			Version: "v1",
			Key: Key{
				TypeURL:  TypeURL{"type.googleapis.com/google.protobuf.Empty"},
				FullName: FullNameFromNamespaceAndName("ns1", "res1"),
			},
		},
		Metadata: Metadata{
			CreateTime: time.Unix(1, 1).UTC(),
		},
		Item: &types.Empty{},
	}

	env, err := ToMcpResource(e)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	env.Metadata.CreateTime.Seconds = 253402300800 + 1

	b := NewSchemaBuilder()
	b.Register("type.googleapis.com/google.protobuf.Empty")
	s := b.Build()
	if _, err = FromMcpResource(s, env); err == nil {
		t.Fatalf("expected error not found")
	}
}

func TestExtract_UnmarshalError(t *testing.T) {
	e := Entry{
		ID: VersionedKey{
			Version: "v1",
			Key: Key{
				TypeURL:  TypeURL{"type.googleapis.com/google.protobuf.Empty"},
				FullName: FullNameFromNamespaceAndName("ns1", "res1"),
			},
		},
		Metadata: Metadata{
			CreateTime: time.Unix(1, 1).UTC(),
		},
		Item: &types.Empty{},
	}

	env, err := ToMcpResource(e)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	b := NewSchemaBuilder()
	b.Register("type.googleapis.com/google.protobuf.Empty")
	s := b.Build()
	i := s.byURL["type.googleapis.com/google.protobuf.Empty"]
	i.goType = reflect.TypeOf(invalidProto{})
	s.byURL["type.googleapis.com/google.protobuf.Empty"] = i
	if _, err = FromMcpResource(s, env); err == nil {
		t.Fatalf("expected error not found")
	}
}

func TestEnvelopeAll(t *testing.T) {
	entries := []Entry{
		{
			ID: VersionedKey{
				Version: "v1",
				Key: Key{
					TypeURL:  TypeURL{"type.googleapis.com/google.protobuf.Empty"},
					FullName: FullNameFromNamespaceAndName("ns1", "res1"),
				},
			},
			Metadata: Metadata{
				CreateTime: time.Unix(1, 1).UTC(),
			},
			Item: &types.Empty{},
		},
		{
			ID: VersionedKey{
				Version: "v2",
				Key: Key{
					TypeURL:  TypeURL{"type.googleapis.com/google.protobuf.Empty"},
					FullName: FullNameFromNamespaceAndName("ns2", "res2"),
				},
			},
			Metadata: Metadata{
				CreateTime: time.Unix(2, 2).UTC(),
			},
			Item: &types.Empty{},
		},
	}

	envs, err := ToMcpResourceAll(entries)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	b := NewSchemaBuilder()
	b.Register("type.googleapis.com/google.protobuf.Empty")
	s := b.Build()

	actual, err := FromMcpResourceAll(s, envs)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !reflect.DeepEqual(entries, actual) {
		t.Fatalf("mismatch: got:%+v, wanted:%+v", actual, entries)
	}
}

func TestEnvelopeAll_Error(t *testing.T) {
	entries := []Entry{
		{
			ID: VersionedKey{
				Version: "v1",
				Key: Key{
					TypeURL:  TypeURL{"type.googleapis.com/google.protobuf.Empty"},
					FullName: FullNameFromNamespaceAndName("ns1", "res1"),
				},
			},
			Metadata: Metadata{
				CreateTime: time.Unix(1, 1).UTC(),
			},
			Item: &invalidProto{},
		},
		{
			ID: VersionedKey{
				Version: "v2",
				Key: Key{
					TypeURL:  TypeURL{"type.googleapis.com/google.protobuf.Empty"},
					FullName: FullNameFromNamespaceAndName("ns2", "res2"),
				},
			},
			Metadata: Metadata{
				CreateTime: time.Unix(2, 2).UTC(),
			},
			Item: &types.Empty{},
		}}

	if _, err := ToMcpResourceAll(entries); err == nil {
		t.Fatal("expected error not found")
	}
}

func TestExtractAll_Error(t *testing.T) {
	entries := []Entry{
		{
			ID: VersionedKey{
				Version: "v1",
				Key: Key{
					TypeURL:  TypeURL{"type.googleapis.com/google.protobuf.Empty"},
					FullName: FullNameFromNamespaceAndName("ns1", "res1"),
				},
			},
			Metadata: Metadata{
				CreateTime: time.Unix(1, 1).UTC(),
			},
			Item: &empty.Empty{},
		},
		{
			ID: VersionedKey{
				Version: "v2",
				Key: Key{
					TypeURL:  TypeURL{"type.googleapis.com/google.protobuf.Empty"},
					FullName: FullNameFromNamespaceAndName("ns2", "res2"),
				},
			},
			Metadata: Metadata{
				CreateTime: time.Unix(2, 2).UTC(),
			},
			Item: &types.Empty{},
		},
	}

	env, err := ToMcpResourceAll(entries)
	if err != nil {
		t.Fatalf("unexpected eror: %v", err)
	}

	b := NewSchemaBuilder()
	s := b.Build()

	if _, err = FromMcpResourceAll(s, env); err == nil {
		t.Fatal("expected error not found")
	}
}

type invalidProto struct {
}

var _ proto.Message = &invalidProto{}
var _ proto.Marshaler = &invalidProto{}
var _ proto.Unmarshaler = &invalidProto{}

func (i *invalidProto) Reset()                   {}
func (i *invalidProto) String() string           { return "" }
func (i *invalidProto) ProtoMessage()            {}
func (i *invalidProto) Unmarshal([]byte) error   { return errors.New("unmarshal error") }
func (i *invalidProto) Marshal() ([]byte, error) { return nil, errors.New("marshal error") }
