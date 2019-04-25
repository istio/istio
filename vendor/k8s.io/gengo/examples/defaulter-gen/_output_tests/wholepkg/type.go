/*
Copyright 2018 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package wholepkg

import (
	"k8s.io/gengo/examples/defaulter-gen/_output_tests/empty"
)

// Only primitives
type Struct_Primitives struct {
	empty.TypeMeta
	BoolField   *bool
	IntField    *int
	StringField *string
	FloatField  *float64
}
type Struct_Primitives_Alias Struct_Primitives

type Struct_Struct_Primitives struct {
	empty.TypeMeta
	StructField Struct_Primitives
}

//Pointer
type Struct_Pointer struct {
	empty.TypeMeta
	PointerStructPrimitivesField             Struct_Primitives
	PointerPointerStructPrimitivesField      *Struct_Primitives
	PointerStructPrimitivesAliasField        Struct_Primitives_Alias
	PointerPointerStructPrimitivesAliasField Struct_Primitives_Alias
	PointerStructStructPrimitives            Struct_Struct_Primitives
	PointerPointerStructStructPrimitives     *Struct_Struct_Primitives
}

// Slices
type Struct_Slices struct {
	empty.TypeMeta
	SliceStructPrimitivesField             []Struct_Primitives
	SlicePointerStructPrimitivesField      []*Struct_Primitives
	SliceStructPrimitivesAliasField        []Struct_Primitives_Alias
	SlicePointerStructPrimitivesAliasField []*Struct_Primitives_Alias
	SliceStructStructPrimitives            []Struct_Struct_Primitives
	SlicePointerStructStructPrimitives     []*Struct_Struct_Primitives
}

// Everything
type Struct_Everything struct {
	empty.TypeMeta
	BoolPtrField       *bool
	IntPtrField        *int
	StringPtrField     *string
	FloatPtrField      *float64
	PointerStructField Struct_Pointer
	SliceBoolField     []bool
	SliceByteField     []byte
	SliceIntField      []int
	SliceStringField   []string
	SliceFloatField    []float64
	SlicesStructField  Struct_Slices
}
