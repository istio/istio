// Copyright 2018 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package types

import (
	"fmt"
	"reflect"

	"github.com/golang/protobuf/proto"
	"github.com/golang/protobuf/ptypes"
	structpb "github.com/golang/protobuf/ptypes/struct"
	"github.com/google/cel-go/common/types/ref"
	"github.com/google/cel-go/common/types/traits"
)

var (
	jsonStructType = reflect.TypeOf(&structpb.Struct{})
)

type jsonStruct struct {
	ref.TypeAdapter
	*structpb.Struct
}

// NewJSONStruct creates a traits.Mapper implementation backed by a JSON struct that has been
// encoded in protocol buffer form.
//
// The `adapter` argument provides type adaptation capabilities from proto to CEL.
func NewJSONStruct(adapter ref.TypeAdapter, st *structpb.Struct) traits.Mapper {
	return &jsonStruct{TypeAdapter: adapter, Struct: st}
}

func (m *jsonStruct) Contains(index ref.Val) ref.Val {
	return Bool(!IsError(m.Get(index)))
}

func (m *jsonStruct) ConvertToNative(typeDesc reflect.Type) (interface{}, error) {
	switch typeDesc.Kind() {
	case reflect.Map:
		otherKey := typeDesc.Key()
		otherElem := typeDesc.Elem()
		if typeDesc.Key().Kind() == reflect.String {
			nativeMap := reflect.MakeMapWithSize(typeDesc, int(m.Size().(Int)))
			it := m.Iterator()
			for it.HasNext() == True {
				key := it.Next()
				refKeyValue, err := key.ConvertToNative(otherKey)
				if err != nil {
					return nil, err
				}
				refElemValue, err := m.Get(key).ConvertToNative(otherElem)
				if err != nil {
					return nil, err
				}
				nativeMap.SetMapIndex(
					reflect.ValueOf(refKeyValue),
					reflect.ValueOf(refElemValue))
			}
			return nativeMap.Interface(), nil
		}

	case reflect.Ptr:
		switch typeDesc {
		case jsonValueType:
			return &structpb.Value{
				Kind: &structpb.Value_StructValue{
					StructValue: m.Struct}}, nil
		case jsonStructType:
			return m.Struct, nil
		case anyValueType:
			return ptypes.MarshalAny(m.Value().(proto.Message))
		}

	case reflect.Interface:
		// If the struct is already assignable to the desired type return it.
		if reflect.TypeOf(m).Implements(typeDesc) {
			return m, nil
		}
	}
	return nil, fmt.Errorf(
		"no conversion found from map type to native type."+
			" map type: google.protobuf.Struct, native type: %v", typeDesc)
}

func (m *jsonStruct) ConvertToType(typeVal ref.Type) ref.Val {
	switch typeVal {
	case MapType:
		return m
	case TypeType:
		return MapType
	}
	return NewErr("type conversion error from '%s' to '%s'", MapType, typeVal)
}

func (m *jsonStruct) Equal(other ref.Val) ref.Val {
	if MapType != other.Type() {
		return ValOrErr(other, "no such overload")
	}
	otherMap := other.(traits.Mapper)
	if m.Size() != otherMap.Size() {
		return False
	}
	it := m.Iterator()
	for it.HasNext() == True {
		key := it.Next()
		if otherVal := otherMap.Get(key); IsError(otherVal) {
			return False
		} else if thisVal := m.Get(key); IsError(thisVal) {
			return False
		} else {
			valEq := thisVal.Equal(otherVal)
			if valEq == False || IsUnknownOrError(valEq) {
				return valEq
			}
		}
	}
	return True
}

func (m *jsonStruct) Get(key ref.Val) ref.Val {
	if StringType != key.Type() {
		return ValOrErr(key, "unsupported key type: '%v", key.Type())
	}
	fields := m.Struct.GetFields()
	value, found := fields[string(key.(String))]
	if !found {
		return NewErr("no such key: '%v'", key)
	}
	return m.NativeToValue(value)
}

func (m *jsonStruct) Iterator() traits.Iterator {
	f := m.GetFields()
	keys := make([]string, len(m.GetFields()))
	i := 0
	for k := range f {
		keys[i] = k
		i++
	}
	return &jsonValueMapIterator{
		baseIterator: &baseIterator{},
		len:          len(keys),
		mapKeys:      keys}
}

func (m *jsonStruct) Size() ref.Val {
	return Int(len(m.GetFields()))
}

func (m *jsonStruct) Type() ref.Type {
	return MapType
}

func (m *jsonStruct) Value() interface{} {
	return m.Struct
}

type jsonValueMapIterator struct {
	*baseIterator
	cursor  int
	len     int
	mapKeys []string
}

func (it *jsonValueMapIterator) HasNext() ref.Val {
	return Bool(it.cursor < it.len)
}

func (it *jsonValueMapIterator) Next() ref.Val {
	if it.HasNext() == True {
		index := it.cursor
		it.cursor++
		return String(it.mapKeys[index])
	}
	return nil
}
