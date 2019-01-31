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
	"bytes"
	"fmt"
	"reflect"

	"github.com/google/cel-go/common/types/ref"
	"github.com/google/cel-go/common/types/traits"
)

// Bytes type that implements ref.Value and supports add, compare, and size
// operations.
type Bytes []byte

var (
	// BytesType singleton.
	BytesType = NewTypeValue("bytes",
		traits.AdderType,
		traits.ComparerType,
		traits.SizerType)
)

// Add implements traits.Adder.Add by concatenating byte sequences.
func (b Bytes) Add(other ref.Value) ref.Value {
	if BytesType != other.Type() {
		return ValOrErr(other, "no such overload")
	}
	return append(b, other.(Bytes)...)
}

// Compare implments traits.Comparer.Compare by lexicographic ordering.
func (b Bytes) Compare(other ref.Value) ref.Value {
	if BytesType != other.Type() {
		return ValOrErr(other, "no such overload")
	}
	return Int(bytes.Compare(b, other.(Bytes)))
}

// ConvertToNative implements ref.Value.ConvertToNative.
func (b Bytes) ConvertToNative(typeDesc reflect.Type) (interface{}, error) {
	switch typeDesc.Kind() {
	case reflect.Array, reflect.Slice:
		if typeDesc.Elem().Kind() == reflect.Uint8 {
			return b.Value(), nil
		}
	case reflect.Interface:
		if reflect.TypeOf(b).Implements(typeDesc) {
			return b, nil
		}
	}
	return nil, fmt.Errorf("type conversion error from Bytes to '%v'", typeDesc)
}

// ConvertToType implements ref.Value.ConvertToType.
func (b Bytes) ConvertToType(typeVal ref.Type) ref.Value {
	switch typeVal {
	case StringType:
		return String(b)
	case BytesType:
		return b
	case TypeType:
		return BytesType
	}
	return NewErr("type conversion error from '%s' to '%s'", BytesType, typeVal)
}

// Equal implements ref.Value.Equal.
func (b Bytes) Equal(other ref.Value) ref.Value {
	if BytesType != other.Type() {
		return ValOrErr(other, "no such overload")
	}
	return Bool(bytes.Equal(b, other.(Bytes)))
}

// Size implements traits.Sizer.Size.
func (b Bytes) Size() ref.Value {
	return Int(len(b))
}

// Type implements ref.Value.Type.
func (b Bytes) Type() ref.Type {
	return BytesType
}

// Value implements ref.Value.Value.
func (b Bytes) Value() interface{} {
	return []byte(b)
}
