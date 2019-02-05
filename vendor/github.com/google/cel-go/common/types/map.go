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

	structpb "github.com/golang/protobuf/ptypes/struct"
	"github.com/google/cel-go/common/types/ref"
	"github.com/google/cel-go/common/types/traits"
)

// baseMap is a reflection based map implementation designed to handle a variety of map-like types.
type baseMap struct {
	value    interface{}
	refValue reflect.Value
}

// stringMap is a specialization to improve the performance of simple key, value pair lookups by
// string as this is the most common usage of maps.
type stringMap struct {
	*baseMap
	mapStrStr map[string]string
}

// NewDynamicMap returns a traits.Mapper value with dynamic key, value pairs.
func NewDynamicMap(value interface{}) traits.Mapper {
	return &baseMap{value, reflect.ValueOf(value)}
}

// NewStringStringMap returns a specialized traits.Mapper with string keys and values.
func NewStringStringMap(value map[string]string) traits.Mapper {
	return &stringMap{
		baseMap:   &baseMap{value: value},
		mapStrStr: value,
	}
}

var (
	// MapType singleton.
	MapType = NewTypeValue("map",
		traits.ContainerType,
		traits.IndexerType,
		traits.IterableType,
		traits.SizerType)
)

func (m *baseMap) Contains(index ref.Val) ref.Val {
	return Bool(m.Get(index).Type() != ErrType)
}

func (m *baseMap) ConvertToNative(refType reflect.Type) (interface{}, error) {
	// JSON conversion.
	if refType == jsonValueType || refType == jsonStructType {
		jsonEntries, err :=
			m.ConvertToNative(reflect.TypeOf(map[string]*structpb.Value{}))
		if err != nil {
			return nil, err
		}
		jsonMap := &structpb.Struct{
			Fields: jsonEntries.(map[string]*structpb.Value)}
		if refType == jsonStructType {
			return jsonMap, nil
		}
		return &structpb.Value{
			Kind: &structpb.Value_StructValue{
				StructValue: jsonMap}}, nil
	}

	// Non-map conversion.
	if refType.Kind() != reflect.Map {
		return nil, fmt.Errorf("type conversion error from map to '%v'", refType)
	}

	// Map conversion.
	thisType := m.refValue.Type()
	thisKey := thisType.Key()
	thisKeyKind := thisKey.Kind()
	thisElem := thisType.Elem()
	thisElemKind := thisElem.Kind()

	otherKey := refType.Key()
	otherKeyKind := otherKey.Kind()
	otherElem := refType.Elem()
	otherElemKind := otherElem.Kind()

	if otherKeyKind == thisKeyKind && otherElemKind == thisElemKind {
		return m.value, nil
	}
	elemCount := m.Size().(Int)
	nativeMap := reflect.MakeMapWithSize(refType, int(elemCount))
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

func (m *stringMap) ConvertToNative(refType reflect.Type) (interface{}, error) {
	if !m.baseMap.refValue.IsValid() {
		m.baseMap.refValue = reflect.ValueOf(m.value)
	}
	return m.baseMap.ConvertToNative(refType)
}

func (m *baseMap) ConvertToType(typeVal ref.Type) ref.Val {
	switch typeVal {
	case MapType:
		return m
	case TypeType:
		return MapType
	}
	return NewErr("type conversion error from '%s' to '%s'", MapType, typeVal)
}

func (m *baseMap) Equal(other ref.Val) ref.Val {
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

func (m *stringMap) Equal(other ref.Val) ref.Val {
	if !m.baseMap.refValue.IsValid() {
		m.baseMap.refValue = reflect.ValueOf(m.value)
	}
	return m.baseMap.Equal(other)
}

func (m *baseMap) Get(key ref.Val) ref.Val {
	// TODO: There are multiple reasons why a Get could fail. Typically, this is because the key
	// does not exist in the map; however, it's possible that the value cannot be converted to
	// the desired type. Refine this strategy to disambiguate these cases.
	thisKeyType := m.refValue.Type().Key()
	nativeKey, err := key.ConvertToNative(thisKeyType)
	if err != nil {
		return &Err{err}
	}
	nativeKeyVal := reflect.ValueOf(nativeKey)
	if !nativeKeyVal.Type().AssignableTo(thisKeyType) {
		return NewErr("no such key: '%v'", nativeKey)
	}
	value := m.refValue.MapIndex(nativeKeyVal)
	if !value.IsValid() {
		return NewErr("no such key: '%v'", nativeKey)
	}
	return NativeToValue(value.Interface())
}

func (m *stringMap) Get(key ref.Val) ref.Val {
	strKey, ok := key.(String)
	if !ok {
		return ValOrErr(key, "no such key: %v", key)
	}
	val, found := m.mapStrStr[string(strKey)]
	if !found {
		return NewErr("no such key: %s", key)
	}
	return String(val)
}

func (m *baseMap) Iterator() traits.Iterator {
	mapKeys := m.refValue.MapKeys()
	return &mapIterator{
		baseIterator: &baseIterator{},
		mapValue:     m,
		mapKeys:      mapKeys,
		cursor:       0,
		len:          int(m.Size().(Int))}
}

func (m *stringMap) Iterator() traits.Iterator {
	if !m.baseMap.refValue.IsValid() {
		m.baseMap.refValue = reflect.ValueOf(m.value)
	}
	return m.baseMap.Iterator()
}

func (m *baseMap) Size() ref.Val {
	return Int(m.refValue.Len())
}

func (m *stringMap) Size() ref.Val {
	return Int(len(m.mapStrStr))
}

func (m *baseMap) Type() ref.Type {
	return MapType
}

func (m *baseMap) Value() interface{} {
	return m.value
}

type mapIterator struct {
	*baseIterator
	mapValue traits.Mapper
	mapKeys  []reflect.Value
	cursor   int
	len      int
}

func (it *mapIterator) HasNext() ref.Val {
	return Bool(it.cursor < it.len)
}

func (it *mapIterator) Next() ref.Val {
	if it.HasNext() == True {
		index := it.cursor
		it.cursor++
		refKey := it.mapKeys[index]
		return NativeToValue(refKey.Interface())
	}
	return nil
}
