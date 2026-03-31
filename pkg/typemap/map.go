// Copyright Istio Authors
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

package typemap

import (
	"reflect"

	"istio.io/istio/pkg/ptr"
)

// TypeMap provides a map that holds a map of Type -> Value. There can be only a single value per type.
// The value stored for a type must be of the same type as the key.
type TypeMap struct {
	inner map[reflect.Type]any
}

func NewTypeMap() TypeMap {
	return TypeMap{make(map[reflect.Type]any)}
}

func Set[T any](t TypeMap, v T) {
	t.inner[reflect.TypeFor[T]()] = v
}

func Get[T any](t TypeMap) *T {
	v, f := t.inner[reflect.TypeFor[T]()]
	if f {
		return ptr.Of(v.(T))
	}
	return nil
}
