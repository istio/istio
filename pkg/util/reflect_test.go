// Copyright 2019 Istio Authors
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

package util

import (
	"reflect"
	"testing"
)

// TODO: add missing unit tests.

// errToString returns the string representation of err and the empty string if
// err is nil.
func errToString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

// to ptr conversion utility functions
func toInt8Ptr(i int8) *int8 { return &i }

func TestIsValueNil(t *testing.T) {
	if !IsValueNil(nil) {
		t.Error("got IsValueNil(nil) false, want true")
	}
	if !IsValueNil((*int)(nil)) {
		t.Error("got IsValueNil(ptr) false, want true")
	}
	if !IsValueNil(map[int]int(nil)) {
		t.Error("got IsValueNil(map) false, want true")
	}
	if !IsValueNil([]int(nil)) {
		t.Error("got IsValueNil(slice) false, want true")
	}
	if !IsValueNil(interface{}(nil)) {
		t.Error("got IsValueNil(interface) false, want true")
	}

	if IsValueNil(toInt8Ptr(42)) {
		t.Error("got IsValueNil(ptr) true, want false")
	}
	if IsValueNil(map[int]int{42: 42}) {
		t.Error("got IsValueNil(map) true, want false")
	}
	if IsValueNil([]int{1, 2, 3}) {
		t.Error("got IsValueNil(slice) true, want false")
	}
	if IsValueNil(interface{}(42)) {
		t.Error("got IsValueNil(interface) true, want false")
	}
}

func TestIsValueNilOrDefault(t *testing.T) {
	if !IsValueNilOrDefault(nil) {
		t.Error("got IsValueNilOrDefault(nil) false, want true")
	}
	if !IsValueNilOrDefault((*int)(nil)) {
		t.Error("got IsValueNilOrDefault(ptr) false, want true")
	}
	if !IsValueNilOrDefault(map[int]int(nil)) {
		t.Error("got IsValueNilOrDefault(map) false, want true")
	}
	if !IsValueNilOrDefault([]int(nil)) {
		t.Error("got IsValueNilOrDefault(slice) false, want true")
	}
	if !IsValueNilOrDefault(interface{}(nil)) {
		t.Error("got IsValueNilOrDefault(interface) false, want true")
	}
	if !IsValueNilOrDefault(int(0)) {
		t.Error("got IsValueNilOrDefault(int(0)) false, want true")
	}
	if !IsValueNilOrDefault("") {
		t.Error("got IsValueNilOrDefault(\"\") false, want true")
	}
	if !IsValueNilOrDefault(false) {
		t.Error("got IsValueNilOrDefault(false) false, want true")
	}
	i := 32
	ip := &i
	if IsValueNilOrDefault(&ip) {
		t.Error("got IsValueNilOrDefault(ptr to ptr) false, want true")
	}
}

func TestIsValueFuncs(t *testing.T) {
	testInt := int(42)
	testStruct := struct{}{}
	testSlice := []bool{}
	testMap := map[bool]bool{}
	var testNilSlice []bool
	var testNilMap map[bool]bool

	allValues := []interface{}{nil, testInt, &testInt, testStruct, &testStruct, testNilSlice, testSlice, &testSlice, testNilMap, testMap, &testMap}

	tests := []struct {
		desc     string
		function func(v reflect.Value) bool
		okValues []interface{}
	}{
		{
			desc:     "IsValuePtr",
			function: IsValuePtr,
			okValues: []interface{}{&testInt, &testStruct, &testSlice, &testMap},
		},
		{
			desc:     "IsValueStruct",
			function: IsValueStruct,
			okValues: []interface{}{testStruct},
		},
		{
			desc:     "IsValueInterface",
			function: IsValueInterface,
			okValues: []interface{}{},
		},
		{
			desc:     "IsValueStructPtr",
			function: IsValueStructPtr,
			okValues: []interface{}{&testStruct},
		},
		{
			desc:     "IsValueMap",
			function: IsValueMap,
			okValues: []interface{}{testNilMap, testMap},
		},
		{
			desc:     "IsValueSlice",
			function: IsValueSlice,
			okValues: []interface{}{testNilSlice, testSlice},
		},
		{
			desc:     "IsValueScalar",
			function: IsValueScalar,
			okValues: []interface{}{testInt, &testInt},
		},
	}

	for _, tt := range tests {
		for vidx, v := range allValues {
			if got, want := tt.function(reflect.ValueOf(v)), isInListOfInterface(tt.okValues, v); got != want {
				t.Errorf("%s with %s (#%d): got: %t, want: %t", tt.desc, reflect.TypeOf(v), vidx, got, want)
			}
		}
	}
}

func TestValuesAreSameType(t *testing.T) {
	type EnumType int64

	tests := []struct {
		inDesc string
		inV1   interface{}
		inV2   interface{}
		want   bool
	}{
		{
			inDesc: "success both are int32 types",
			inV1:   int32(42),
			inV2:   int32(43),
			want:   true,
		},
		{
			inDesc: "fail unmatching int types",
			inV1:   int16(42),
			inV2:   int32(43),
			want:   false,
		},
		{
			inDesc: "fail unmatching int and string type",
			inV1:   int32(42),
			inV2:   "42",
			want:   false,
		},
		{
			inDesc: "fail EnumType and int64 types",
			inV1:   EnumType(42),
			inV2:   int64(43),
			want:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.inDesc, func(t *testing.T) {
			got := ValuesAreSameType(reflect.ValueOf(tt.inV1), reflect.ValueOf(tt.inV2))
			if got != tt.want {
				t.Errorf("got %v, want %v for comparing %T against %T", got, tt.want, tt.inV1, tt.inV2)
			}
		})
	}
}

func TestIsTypeFuncs(t *testing.T) {
	testInt := int(42)
	testStruct := struct{}{}
	testSlice := []bool{}
	testSliceOfInterface := []interface{}{}
	testMap := map[bool]bool{}
	var testNilSlice []bool
	var testNilMap map[bool]bool

	allTypes := []interface{}{nil, testInt, &testInt, testStruct, &testStruct, testNilSlice,
		testSlice, &testSlice, testSliceOfInterface, testNilMap, testMap, &testMap}

	tests := []struct {
		desc     string
		function func(v reflect.Type) bool
		okTypes  []interface{}
	}{
		{
			desc:     "IsTypeStructPtr",
			function: IsTypeStructPtr,
			okTypes:  []interface{}{&testStruct},
		},
		{
			desc:     "IsTypeSlicePtr",
			function: IsTypeSlicePtr,
			okTypes:  []interface{}{&testSlice},
		},
		{
			desc:     "IsTypeMap",
			function: IsTypeMap,
			okTypes:  []interface{}{testNilMap, testMap},
		},
		{
			desc:     "IsTypeInterface",
			function: IsTypeInterface,
			okTypes:  []interface{}{},
		},
		{
			desc:     "IsTypeSliceOfInterface",
			function: IsTypeSliceOfInterface,
			okTypes:  []interface{}{testSliceOfInterface},
		},
	}

	for _, tt := range tests {
		for vidx, v := range allTypes {
			if got, want := tt.function(reflect.TypeOf(v)), isInListOfInterface(tt.okTypes, v); got != want {
				t.Errorf("%s with %s (#%d): got: %t, want: %t", tt.desc, reflect.TypeOf(v), vidx, got, want)
			}
		}
	}

}

type interfaceContainer struct {
	I anInterface
}

type anInterface interface {
	IsU()
}

type implementsInterface struct {
	A string
}

func (*implementsInterface) IsU() {}

func TestIsValueInterface(t *testing.T) {
	intf := &interfaceContainer{
		I: &implementsInterface{
			A: "a",
		},
	}
	iField := reflect.ValueOf(intf).Elem().FieldByName("I")
	if !IsValueInterface(iField) {
		t.Errorf("IsValueInterface(): got false, want true")
	}
}

func TestIsTypeInterface(t *testing.T) {
	intf := &interfaceContainer{
		I: &implementsInterface{
			A: "a",
		},
	}
	testIfField := reflect.ValueOf(intf).Elem().Field(0)

	if !IsTypeInterface(testIfField.Type()) {
		t.Errorf("IsTypeInterface(): got false, want true")
	}
}

func isInListOfInterface(lv []interface{}, v interface{}) bool {
	for _, vv := range lv {
		if reflect.DeepEqual(vv, v) {
			return true
		}
	}
	return false
}

func TestInsertIntoMap(t *testing.T) {
	parentMap := map[int]string{42: "forty two", 43: "forty three"}
	key := 44
	value := "forty four"
	if err := InsertIntoMap(parentMap, key, value); err != nil {
		t.Fatalf("got error: %s, want error: nil", err)
	}
	wantMap := map[int]string{42: "forty two", 43: "forty three", 44: "forty four"}
	if got, want := parentMap, wantMap; !reflect.DeepEqual(got, want) {
		t.Errorf("got:\n%v\nwant:\n%v\n", got, want)
	}

	badParent := struct{}{}
	wantErr := `insertIntoMap parent type is *struct {}, must be map`
	if got, want := errToString(InsertIntoMap(&badParent, key, value)), wantErr; got != want {
		t.Fatalf("got error: %s, want error: %s", got, want)
	}
}
