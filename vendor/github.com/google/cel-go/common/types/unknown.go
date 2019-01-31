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

	"github.com/google/cel-go/common/types/ref"
)

// Unknown type implementation which collects expression ids which caused the
// current value to become unknown.
type Unknown []int64

var (
	// UnknownType singleton.
	UnknownType = NewTypeValue("unknown")
)

// ConvertToNative implements ref.Value.ConvertToNative.
func (u Unknown) ConvertToNative(typeDesc reflect.Type) (interface{}, error) {
	return u.Value(), nil
}

// ConvertToType implements ref.Value.ConvertToType.
func (u Unknown) ConvertToType(typeVal ref.Type) ref.Value {
	return u
}

// Equal implements ref.Value.Equal.
func (u Unknown) Equal(other ref.Value) ref.Value {
	return u
}

// Type implements ref.Value.Type.
func (u Unknown) Type() ref.Type {
	return UnknownType
}

// Value implements ref.Value.Value.
func (u Unknown) Value() interface{} {
	return []int64(u)
}

// IsUnknown returns whether the element ref.Type or ref.Value is equal to the
// UnknownType singleton.
func IsUnknown(val ref.Value) bool {
	return val.Type() == UnknownType
}
