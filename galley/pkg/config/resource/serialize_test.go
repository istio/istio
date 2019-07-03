// Copyright 2019 Istio Authors
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

package resource

import (
	"bytes"
	"errors"
	"fmt"
	"math"
	"reflect"
	"testing"
	"time"

	"github.com/gogo/protobuf/jsonpb"
	"github.com/gogo/protobuf/proto"
	"github.com/gogo/protobuf/types"
)

func TestSerialization_Basic(t *testing.T) {
	e := Entry{
		Metadata: Metadata{
			Name:       NewName("ns1", "res1"),
			CreateTime: time.Unix(1, 1).UTC(),
			Version:    "v1",
		},
		Item: parseStruct(`{ "foo": "bar" }`),
	}

	env, err := Serialize(&e)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if env.Metadata.Name != e.Metadata.Name.String() {
		t.Fatalf("unexpected name: %v", env.Metadata.Name)
	}

	if env.Metadata.Version != string(e.Metadata.Version) {
		t.Fatalf("unexpected version: %v", env.Metadata.Version)
	}

	if env.Metadata.CreateTime == nil {
		t.Fatal("CreateTime is nil")
	}

	expected := boxAny(parseStruct(`{ "foo": "bar" }`))
	if !reflect.DeepEqual(env.Body, expected) {
		t.Fatalf("Resources are not equal %v != %v", env.Body, expected)
	}

	ext, err := Deserialize(env)
	if err != nil {
		t.Fatalf("Unexpected error when extracting: %v", err)
	}

	if !reflect.DeepEqual(ext.Metadata, e.Metadata) {
		t.Fatalf("mismatch: got:%v, wanted: %v", ext, e)
	}
}

func TestSerialize_Error(t *testing.T) {
	e := Entry{
		Metadata: Metadata{
			Name:       NewName("ns1", "res1"),
			CreateTime: time.Unix(1, 1).UTC(),
			Version:    "v1",
		},
		Item: &invalidProto{},
	}

	_, err := Serialize(&e)
	if err == nil {
		t.Fatal("expected error not found")
	}
}

func TestMustSerialize(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("Should not have panicked %v", r)
		}
	}()

	e := Entry{
		Metadata: Metadata{
			Name:       NewName("ns1", "res1"),
			CreateTime: time.Unix(1, 1).UTC(),
			Version:    "v1",
		},
		Item: &types.Empty{},
	}

	_ = MustSerialize(&e)
}

func TestMustSerialize_Panic(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatalf("Should have panicked %v", r)
		}
	}()

	e := Entry{
		Metadata: Metadata{
			Name:       NewName("ns1", "res1"),
			CreateTime: time.Unix(1, 1).UTC(),
			Version:    "v1",
		},
		Item: &invalidProto{},
	}

	_ = MustSerialize(&e)
}

func TestSerialize_InvalidTimestamp_Error(t *testing.T) {
	e := Entry{
		Metadata: Metadata{
			Name:       NewName("ns1", "res1"),
			CreateTime: time.Unix(math.MinInt64, math.MinInt64).UTC(),
			Version:    "v1",
		},
		Item: &types.Empty{},
	}
	_, err := Serialize(&e)
	if err == nil {
		t.Fatal("expected error not found")
	}
}

func TestDeserialize_Error(t *testing.T) {
	e := Entry{
		Metadata: Metadata{
			Name:       NewName("ns1", "res1"),
			CreateTime: time.Unix(1, 1).UTC(),
			Version:    "v1",
		},
		Item: &types.Empty{},
	}

	env, err := Serialize(&e)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	env.Body.TypeUrl += ".foo"

	if _, err = Deserialize(env); err == nil {
		t.Fatalf("expected error not found")
	}
}

func TestDeserialize_InvalidTimestamp_Error(t *testing.T) {
	e := Entry{
		Metadata: Metadata{
			Name:       NewName("ns1", "res1"),
			CreateTime: time.Unix(1, 1).UTC(),
			Version:    "v1",
		},
		Item: &types.Empty{},
	}

	env, err := Serialize(&e)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	env.Metadata.CreateTime.Seconds = 253402300800 + 1

	if _, err = Deserialize(env); err == nil {
		t.Fatalf("expected error not found")
	}
}

func TestDeserialize_Any_Error(t *testing.T) {
	e := Entry{
		Metadata: Metadata{
			Name:       NewName("ns1", "res1"),
			CreateTime: time.Unix(1, 1).UTC(),
			Version:    "v1",
		},
		Item: &types.Empty{},
	}

	env, err := Serialize(&e)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	b := make([]byte, len(env.Body.Value)+1)
	b[0] = 0xFA
	copy(b[1:], env.Body.Value)
	env.Body.Value = b

	if _, err = Deserialize(env); err == nil {
		t.Fatalf("expected error not found")
	}
}

func TestMustDeserialize(t *testing.T) {
	e := Entry{
		Metadata: Metadata{
			Name:       NewName("ns1", "res1"),
			CreateTime: time.Unix(1, 1).UTC(),
			Version:    "v1",
		},
		Item: &types.Empty{},
	}

	s := MustSerialize(&e)

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("Should not have panicked %v", r)
		}
	}()

	_ = MustDeserialize(s)
}

func TestMustDeserialize_Panic(t *testing.T) {
	e := Entry{
		Metadata: Metadata{
			Name:       NewName("ns1", "res1"),
			CreateTime: time.Unix(1, 1).UTC(),
			Version:    "v1",
		},
		Item: &types.Empty{},
	}

	s := MustSerialize(&e)

	defer func() {
		if r := recover(); r == nil {
			t.Fatalf("Should have panicked %v", r)
		}
	}()

	s.Metadata.CreateTime.Seconds = 253402300800 + 1

	_ = MustDeserialize(s)
}

func TestDeserializeAll(t *testing.T) {
	entries := []*Entry{
		{
			Metadata: Metadata{
				Name:       NewName("ns1", "res1"),
				CreateTime: time.Unix(1, 1).UTC(),
				Version:    "v1",
			},
			Item: parseStruct(`{"foo": "bar"}`),
		},
		{
			Metadata: Metadata{
				Name:       NewName("ns2", "res2"),
				CreateTime: time.Unix(1, 1).UTC(),
				Version:    "v2",
			},
			Item: parseStruct(`{"bar": "foo"}`),
		},
	}

	envs, err := SerializeAll(entries)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	actual, err := DeserializeAll(envs)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !reflect.DeepEqual(entries, actual) {
		t.Fatalf("mismatch: got:%+v, wanted:%+v", actual, entries)
	}
}

func TestSerializeAll_Error(t *testing.T) {
	entries := []*Entry{
		{
			Metadata: Metadata{
				Name:       NewName("ns1", "res1"),
				CreateTime: time.Unix(1, 1).UTC(),
				Version:    "v1",
			},
			Item: &invalidProto{},
		},
		{
			Metadata: Metadata{
				Name:       NewName("ns2", "res2"),
				CreateTime: time.Unix(1, 1).UTC(),
				Version:    "v2",
			},
			Item: &types.Empty{},
		},
	}

	if _, err := SerializeAll(entries); err == nil {
		t.Fatal("expected error not found")
	}
}

func TestDeserializeAll_Error(t *testing.T) {
	entries := []*Entry{
		{
			Metadata: Metadata{
				Name:       NewName("ns1", "res1"),
				CreateTime: time.Unix(1, 1).UTC(),
				Version:    "v1",
			},
			Item: &types.Empty{},
		},
		{
			Metadata: Metadata{
				Name:       NewName("ns2", "res2"),
				CreateTime: time.Unix(2, 2).UTC(),
				Version:    "v2",
			},
			Item: &types.Empty{},
		},
	}

	env, err := SerializeAll(entries)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	env[1].Metadata.CreateTime.Seconds = 253402300800 + 1

	if _, err = DeserializeAll(env); err == nil {
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

func parseStruct(s string) *types.Struct {
	p := &types.Struct{}

	b := bytes.NewReader([]byte(s))
	if err := jsonpb.Unmarshal(b, p); err != nil {
		panic(fmt.Errorf("invalid struct JSON: %v", err))
	}

	return p
}

func boxAny(p *types.Struct) *types.Any { // nolint:interfacer
	a, err := types.MarshalAny(p)
	if err != nil {
		panic(fmt.Errorf("unable to marshal to any: %v", err))
	}
	return a
}
