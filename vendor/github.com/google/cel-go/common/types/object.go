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
	"github.com/google/cel-go/common/types/pb"
	"github.com/google/cel-go/common/types/ref"
	"github.com/google/cel-go/common/types/traits"
)

type protoObj struct {
	ref.TypeAdapter
	value     proto.Message
	refValue  reflect.Value
	typeDesc  *pb.TypeDescription
	typeValue *TypeValue
	isAny     bool
}

// NewObject returns an object based on a proto.Message value which handles
// conversion between protobuf type values and expression type values.
// Objects support indexing and iteration.
// Note:  only uses default Db.
func NewObject(adapter ref.TypeAdapter,
	typeDesc *pb.TypeDescription,
	value proto.Message) ref.Val {
	return &protoObj{
		TypeAdapter: adapter,
		value:       value,
		refValue:    reflect.ValueOf(value),
		typeDesc:    typeDesc,
		typeValue:   NewObjectTypeValue(typeDesc.Name())}
}

func (o *protoObj) ConvertToNative(refl reflect.Type) (interface{}, error) {
	if refl.AssignableTo(o.refValue.Type()) {
		return o.value, nil
	}
	if refl == anyValueType {
		return ptypes.MarshalAny(o.Value().(proto.Message))
	}
	// If the object is already assignable to the desired type return it.
	if reflect.TypeOf(o).AssignableTo(refl) {
		return o, nil
	}
	return nil, fmt.Errorf("type conversion error from '%v' to '%v'",
		o.refValue.Type(), refl)
}

func (o *protoObj) ConvertToType(typeVal ref.Type) ref.Val {
	switch typeVal {
	default:
		if o.Type().TypeName() == typeVal.TypeName() {
			return o
		}
	case TypeType:
		return o.typeValue
	}
	return NewErr("type conversion error from '%s' to '%s'",
		o.typeDesc.Name(), typeVal)
}

func (o *protoObj) Equal(other ref.Val) ref.Val {
	if o.typeDesc.Name() != other.Type().TypeName() {
		return ValOrErr(other, "no such overload")
	}
	return Bool(proto.Equal(o.value, other.Value().(proto.Message)))
}

// IsSet tests whether a field which is defined is set to a non-default value.
func (o *protoObj) IsSet(field ref.Val) ref.Val {
	if field.Type() != StringType {
		return ValOrErr(field, "illegal object field type '%s'", field.Type())
	}
	protoFieldName := string(field.(String))
	if f, found := o.typeDesc.FieldByName(protoFieldName); found {
		if !f.IsOneof() {
			return isFieldSet(o.refValue.Elem().Field(f.Index()))
		}

		getter := o.refValue.MethodByName(f.GetterName())
		if getter.IsValid() {
			refField := getter.Call([]reflect.Value{})[0]
			if refField.IsValid() {
				return isFieldSet(refField)
			}
		}
	}
	return NewErr("no such field '%s'", field)
}

func (o *protoObj) Get(index ref.Val) ref.Val {
	if index.Type() != StringType {
		return ValOrErr(index, "illegal object field type '%s'", index.Type())
	}
	protoFieldName := string(index.(String))
	if f, found := o.typeDesc.FieldByName(protoFieldName); found {
		if !f.IsOneof() {
			return getOrDefaultInstance(o.TypeAdapter, o.refValue.Elem().Field(f.Index()))
		}

		getter := o.refValue.MethodByName(f.GetterName())
		if getter.IsValid() {
			refField := getter.Call([]reflect.Value{})[0]
			if refField.IsValid() {
				return getOrDefaultInstance(o.TypeAdapter, refField)
			}
		}
	}
	return NewErr("no such field '%s'", index)
}

func (o *protoObj) Iterator() traits.Iterator {
	return &msgIterator{
		baseIterator: &baseIterator{},
		refValue:     o.refValue,
		typeDesc:     o.typeDesc,
		cursor:       0}
}

func (o *protoObj) Type() ref.Type {
	return o.typeValue
}

func (o *protoObj) Value() interface{} {
	return o.value
}

type msgIterator struct {
	*baseIterator
	refValue reflect.Value
	typeDesc *pb.TypeDescription
	cursor   int
	len      int
}

func (it *msgIterator) HasNext() ref.Val {
	return Bool(it.cursor < it.typeDesc.FieldCount())
}

func (it *msgIterator) Next() ref.Val {
	if it.HasNext() == False {
		return nil
	}
	fieldName, _ := it.typeDesc.FieldNameAtIndex(it.cursor, it.refValue)
	it.cursor++
	return String(fieldName)
}

var (
	protoDefaultInstanceMap = make(map[reflect.Type]ref.Val)
)

func isFieldSet(refVal reflect.Value) ref.Val {
	if refVal.Kind() == reflect.Ptr && refVal.IsNil() {
		return False
	}
	return True
}

func getOrDefaultInstance(adapter ref.TypeAdapter, refVal reflect.Value) ref.Val {
	if isFieldSet(refVal) == True {
		value := refVal.Interface()
		return adapter.NativeToValue(value)
	}
	return getDefaultInstance(adapter, refVal.Type())
}

func getDefaultInstance(adapter ref.TypeAdapter, refType reflect.Type) ref.Val {
	if refType.Kind() == reflect.Ptr {
		refType = refType.Elem()
	}
	if defaultValue, found := protoDefaultInstanceMap[refType]; found {
		return defaultValue
	}
	defaultValue := adapter.NativeToValue(reflect.New(refType).Interface())
	protoDefaultInstanceMap[refType] = defaultValue
	return defaultValue
}
