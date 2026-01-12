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

package typemap_test

import (
	"testing"

	"istio.io/istio/pkg/ptr"
	"istio.io/istio/pkg/test/util/assert"
	"istio.io/istio/pkg/typemap"
)

type TestStruct struct {
	Field string
}

type TestInterface[T any] interface {
	Foo() T
}

func (t TestStruct) Foo() string {
	return t.Field
}

func TestTypeMap(t *testing.T) {
	tm := typemap.NewTypeMap()
	typemap.Set(tm, 1)
	typemap.Set(tm, int32(2))
	typemap.Set(tm, "old")
	typemap.Set(tm, "string")
	typemap.Set(tm, TestStruct{Field: "inner"})
	typemap.Set(tm, &TestStruct{Field: "pointer"})
	typemap.Set[TestInterface[string]](tm, TestStruct{Field: "interface"})

	assert.Equal(t, typemap.Get[int](tm), ptr.Of(1))
	assert.Equal(t, typemap.Get[int32](tm), ptr.Of(int32(2)))
	assert.Equal(t, typemap.Get[string](tm), ptr.Of("string"))
	assert.Equal(t, typemap.Get[*TestStruct](tm), ptr.Of(&TestStruct{Field: "pointer"}))
	assert.Equal(t, typemap.Get[TestInterface[string]](tm), ptr.Of(TestInterface[string](TestStruct{Field: "interface"})))
}
