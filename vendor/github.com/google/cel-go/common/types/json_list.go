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
	jsonListValueType = reflect.TypeOf(&structpb.ListValue{})
)

type jsonListValue struct {
	*structpb.ListValue
}

// NewJSONList creates a traits.Lister implementation backed by a JSON list
// that has been encoded in protocol buffer form.
func NewJSONList(l *structpb.ListValue) traits.Lister {
	return &jsonListValue{l}
}

func (l *jsonListValue) Add(other ref.Val) ref.Val {
	if other.Type() != ListType {
		return ValOrErr(other, "no such overload")
	}
	switch other.(type) {
	case *jsonListValue:
		otherList := other.(*jsonListValue)
		concatElems := append(l.GetValues(), otherList.GetValues()...)
		return NewJSONList(&structpb.ListValue{Values: concatElems})
	}
	return &concatList{
		prevList: l,
		nextList: other.(traits.Lister)}
}

func (l *jsonListValue) Contains(elem ref.Val) ref.Val {
	for i := Int(0); i < l.Size().(Int); i++ {
		if l.Get(i).Equal(elem) == True {
			return True
		}
	}
	return False
}

func (l *jsonListValue) ConvertToNative(typeDesc reflect.Type) (interface{}, error) {
	switch typeDesc.Kind() {
	case reflect.Array, reflect.Slice:
		elemCount := int(l.Size().(Int))
		nativeList := reflect.MakeSlice(typeDesc, elemCount, elemCount)
		for i := 0; i < elemCount; i++ {
			elem := l.Get(Int(i))
			nativeElemVal, err := elem.ConvertToNative(typeDesc.Elem())
			if err != nil {
				return nil, err
			}
			nativeList.Index(i).Set(reflect.ValueOf(nativeElemVal))
		}
		return nativeList.Interface(), nil

	case reflect.Ptr:
		switch typeDesc {
		case jsonValueType:
			return &structpb.Value{
				Kind: &structpb.Value_ListValue{
					ListValue: l.ListValue}}, nil
		case jsonListValueType:
			return l.ListValue, nil
		case anyValueType:
			return ptypes.MarshalAny(l.Value().(proto.Message))
		}

	case reflect.Interface:
		// If the list is already assignable to the desired type return it.
		if reflect.TypeOf(l).Implements(typeDesc) {
			return l, nil
		}
	}
	return nil, fmt.Errorf("no conversion found from list type to native type."+
		" list elem: google.protobuf.Value, native type: %v", typeDesc)
}

func (l *jsonListValue) ConvertToType(typeVal ref.Type) ref.Val {
	switch typeVal {
	case ListType:
		return l
	case TypeType:
		return ListType
	}
	return NewErr("type conversion error from '%s' to '%s'", ListType, typeVal)
}

func (l *jsonListValue) Equal(other ref.Val) ref.Val {
	if ListType != other.Type() {
		return ValOrErr(other, "no such overload")
	}
	otherList := other.(traits.Lister)
	if l.Size() != otherList.Size() {
		return False
	}
	for i := IntZero; i < l.Size().(Int); i++ {
		thisElem := l.Get(i)
		otherElem := otherList.Get(i)
		elemEq := thisElem.Equal(otherElem)
		if elemEq == False || IsUnknownOrError(elemEq) {
			return elemEq
		}
	}
	return True
}

func (l *jsonListValue) Get(index ref.Val) ref.Val {
	if IntType != index.Type() {
		return ValOrErr(index, "unsupported index type: '%v", index.Type())
	}
	i := index.(Int)
	if i < 0 || i >= l.Size().(Int) {
		return NewErr("index '%d' out of range in list size '%d'", i, l.Size())
	}
	elem := l.GetValues()[i]
	return NativeToValue(elem)
}

func (l *jsonListValue) Iterator() traits.Iterator {
	return &jsonValueListIterator{
		baseIterator: &baseIterator{},
		elems:        l.GetValues(),
		len:          len(l.GetValues())}
}

func (l *jsonListValue) Size() ref.Val {
	return Int(len(l.GetValues()))
}

func (l *jsonListValue) Type() ref.Type {
	return ListType
}

func (l *jsonListValue) Value() interface{} {
	return l.ListValue
}

type jsonValueListIterator struct {
	*baseIterator
	cursor int
	elems  []*structpb.Value
	len    int
}

func (it *jsonValueListIterator) HasNext() ref.Val {
	return Bool(it.cursor < it.len)
}

func (it *jsonValueListIterator) Next() ref.Val {
	if it.HasNext() == True {
		index := it.cursor
		it.cursor++
		return NativeToValue(it.elems[index])
	}
	return nil
}
