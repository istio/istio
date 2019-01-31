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
	"reflect"

	"github.com/golang/protobuf/proto"
	"github.com/golang/protobuf/ptypes"
	structpb "github.com/golang/protobuf/ptypes/struct"
	"github.com/google/cel-go/common/types/ref"
)

// Null type implementation.
type Null structpb.NullValue

var (
	// NullType singleton.
	NullType = NewTypeValue("null_type")
	// NullValue singleton.
	NullValue = Null(structpb.NullValue_NULL_VALUE)
)

// ConvertToNative implements ref.Value.ConvertToNative.
func (n Null) ConvertToNative(typeDesc reflect.Type) (interface{}, error) {
	switch typeDesc.Kind() {
	case reflect.Ptr:
		switch typeDesc {
		case jsonValueType:
			return &structpb.Value{
				Kind: &structpb.Value_NullValue{
					NullValue: structpb.NullValue_NULL_VALUE}}, nil
		case anyValueType:
			pb, err := n.ConvertToNative(jsonValueType)
			if err != nil {
				return nil, err
			}
			return ptypes.MarshalAny(pb.(proto.Message))
		}
	case reflect.Interface:
		if reflect.TypeOf(n).Implements(typeDesc) {
			return n, nil
		}
	}
	// By default return 'null'.
	// TODO: determine whether there are other valid conversions for `null`.
	return structpb.NullValue_NULL_VALUE, nil
}

// ConvertToType implements ref.Value.ConvertToType.
func (n Null) ConvertToType(typeVal ref.Type) ref.Value {
	if typeVal == StringType {
		return String("null")
	}
	if typeVal == NullType {
		return n
	}
	return NewErr("type conversion error from '%s' to '%s'", NullType, typeVal)
}

// Equal implements ref.Value.Equal.
func (n Null) Equal(other ref.Value) ref.Value {
	if NullType != other.Type() {
		return ValOrErr(other, "no such overload")
	}
	return True
}

// Type implements ref.Value.Type.
func (n Null) Type() ref.Type {
	return NullType
}

// Value implements ref.Value.Value.
func (n Null) Value() interface{} {
	return structpb.NullValue_NULL_VALUE
}
