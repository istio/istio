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
	"fmt"
	"reflect"

	"github.com/kr/pretty"
)

// IsString reports whether value is a string type.
func IsString(value interface{}) bool {
	return reflect.TypeOf(value).Kind() == reflect.String
}

// IsPtr reports whether value is a ptr type.
func IsPtr(value interface{}) bool {
	return reflect.TypeOf(value).Kind() == reflect.Ptr
}

// IsMap reports whether value is a map type.
func IsMap(value interface{}) bool {
	return reflect.TypeOf(value).Kind() == reflect.Map
}

// IsMapPtr reports whether v is a map ptr type.
func IsMapPtr(v interface{}) bool {
	t := reflect.TypeOf(v)
	return t.Kind() == reflect.Ptr && t.Elem().Kind() == reflect.Map
}

// IsSlice reports whether value is a slice type.
func IsSlice(value interface{}) bool {
	return reflect.TypeOf(value).Kind() == reflect.Slice
}

// IsSlicePtr reports whether v is a slice ptr type.
func IsSlicePtr(v interface{}) bool {
	t := reflect.TypeOf(v)
	return t.Kind() == reflect.Ptr && t.Elem().Kind() == reflect.Slice
}

// IsSliceInterfacePtr reports whether v is a slice ptr type.
func IsSliceInterfacePtr(v interface{}) bool {
	// Must use ValueOf because Elem().Elem() type resolves dynamically.
	vv := reflect.ValueOf(v)
	return vv.Kind() == reflect.Ptr && vv.Elem().Kind() == reflect.Interface && vv.Elem().Elem().Kind() == reflect.Slice
}

// IsInterfacePtr reports whether v is a slice ptr type.
func IsInterfacePtr(v interface{}) bool {
	t := reflect.TypeOf(v)
	return t.Kind() == reflect.Ptr && t.Elem().Kind() == reflect.Interface
}

// IsTypeStruct reports whether t is a struct type.
func IsTypeStruct(t reflect.Type) bool {
	return t.Kind() == reflect.Struct
}

// IsTypeStructPtr reports whether v is a struct ptr type.
func IsTypeStructPtr(t reflect.Type) bool {
	if t == reflect.TypeOf(nil) {
		return false
	}
	return t.Kind() == reflect.Ptr && t.Elem().Kind() == reflect.Struct
}

// IsTypeSlice reports whether v is a slice type.
func IsTypeSlice(t reflect.Type) bool {
	return t.Kind() == reflect.Slice
}

// IsTypeSlicePtr reports whether v is a slice ptr type.
func IsTypeSlicePtr(t reflect.Type) bool {
	if t == reflect.TypeOf(nil) {
		return false
	}
	return t.Kind() == reflect.Ptr && t.Elem().Kind() == reflect.Slice
}

// IsTypeMap reports whether v is a map type.
func IsTypeMap(t reflect.Type) bool {
	if t == reflect.TypeOf(nil) {
		return false
	}
	return t.Kind() == reflect.Map
}

// IsTypeInterface reports whether v is an interface.
func IsTypeInterface(t reflect.Type) bool {
	if t == reflect.TypeOf(nil) {
		return false
	}
	return t.Kind() == reflect.Interface
}

// IsTypeSliceOfInterface reports whether v is a slice of interface.
func IsTypeSliceOfInterface(t reflect.Type) bool {
	if t == reflect.TypeOf(nil) {
		return false
	}
	return t.Kind() == reflect.Slice && t.Elem().Kind() == reflect.Interface
}

// IsNilOrInvalidValue reports whether v is nil or reflect.Zero.
func IsNilOrInvalidValue(v reflect.Value) bool {
	return !v.IsValid() || (v.Kind() == reflect.Ptr && v.IsNil()) || IsValueNil(v.Interface())
}

// IsValueNil returns true if either value is nil, or has dynamic type {ptr,
// map, slice} with value nil.
func IsValueNil(value interface{}) bool {
	if value == nil {
		return true
	}
	switch reflect.TypeOf(value).Kind() {
	case reflect.Slice, reflect.Ptr, reflect.Map:
		return reflect.ValueOf(value).IsNil()
	}
	return false
}

// IsValueNilOrDefault returns true if either IsValueNil(value) or the default
// value for the type.
func IsValueNilOrDefault(value interface{}) bool {
	if IsValueNil(value) {
		return true
	}
	if !IsValueScalar(reflect.ValueOf(value)) {
		// Default value is nil for non-scalar types.
		return false
	}
	return value == reflect.New(reflect.TypeOf(value)).Elem().Interface()
}

// IsValuePtr reports whether v is a ptr type.
func IsValuePtr(v reflect.Value) bool {
	return v.Kind() == reflect.Ptr
}

// IsValueInterface reports whether v is an interface type.
func IsValueInterface(v reflect.Value) bool {
	return v.Kind() == reflect.Interface
}

// IsValueStruct reports whether v is a struct type.
func IsValueStruct(v reflect.Value) bool {
	return v.Kind() == reflect.Struct
}

// IsValueStructPtr reports whether v is a struct ptr type.
func IsValueStructPtr(v reflect.Value) bool {
	return v.Kind() == reflect.Ptr && IsValueStruct(v.Elem())
}

// IsValueMap reports whether v is a map type.
func IsValueMap(v reflect.Value) bool {
	return v.Kind() == reflect.Map
}

// IsValueSlice reports whether v is a slice type.
func IsValueSlice(v reflect.Value) bool {
	return v.Kind() == reflect.Slice
}

// IsValueScalar reports whether v is a scalar type.
func IsValueScalar(v reflect.Value) bool {
	if IsNilOrInvalidValue(v) {
		return false
	}
	if IsValuePtr(v) {
		if v.IsNil() {
			return false
		}
		v = v.Elem()
	}
	return !IsValueStruct(v) && !IsValueMap(v) && !IsValueSlice(v)
}

// ValuesAreSameType returns true if v1 and v2 has the same reflect.Type,
// otherwise it returns false.
func ValuesAreSameType(v1 reflect.Value, v2 reflect.Value) bool {
	return v1.Type() == v2.Type()
}

// IsEmptyString returns true if value is an empty string.
func IsEmptyString(value interface{}) bool {
	if value == nil {
		return true
	}
	switch reflect.TypeOf(value).Kind() {
	case reflect.String:
		return value.(string) == ""
	}
	return false
}

// DeleteFromSlicePtr deletes an entry at index from the parent, which must be a slice ptr.
func DeleteFromSlicePtr(parentSlice interface{}, index int) error {
	dbgPrint("DeleteFromSlicePtr index=%d, slice=\n%s", index, pretty.Sprint(parentSlice))
	pv := reflect.ValueOf(parentSlice)

	if !IsSliceInterfacePtr(parentSlice) {
		return fmt.Errorf("deleteFromSlicePtr parent type is %T, must be *[]interface{}", parentSlice)
	}

	pvv := pv.Elem()
	if pvv.Kind() == reflect.Interface {
		pvv = pvv.Elem()
	}

	pv.Elem().Set(reflect.AppendSlice(pvv.Slice(0, index), pvv.Slice(index+1, pvv.Len())))

	return nil
}

// UpdateSlicePtr updates an entry at index in the parent, which must be a slice ptr, with the given value.
func UpdateSlicePtr(parentSlice interface{}, index int, value interface{}) error {
	dbgPrint("UpdateSlicePtr parent=\n%s\n, index=%d, value=\n%v", pretty.Sprint(parentSlice), index, value)
	pv := reflect.ValueOf(parentSlice)
	v := reflect.ValueOf(value)

	if !IsSliceInterfacePtr(parentSlice) {
		return fmt.Errorf("updateSlicePtr parent type is %T, must be *[]interface{}", parentSlice)
	}

	pvv := pv.Elem()
	if pvv.Kind() == reflect.Interface {
		pv.Elem().Elem().Index(index).Set(v)
		return nil
	}
	pv.Elem().Index(index).Set(v)

	return nil
}

// InsertIntoMap inserts value with key into parent which must be a map, map ptr, or interface to map.
func InsertIntoMap(parentMap interface{}, key interface{}, value interface{}) error {
	dbgPrint("InsertIntoMap key=%v, value=%s, map=\n%s", key, pretty.Sprint(value), pretty.Sprint(parentMap))
	v := reflect.ValueOf(parentMap)
	kv := reflect.ValueOf(key)
	vv := reflect.ValueOf(value)

	if v.Type().Kind() == reflect.Ptr {
		v = v.Elem()
	}
	if v.Type().Kind() == reflect.Interface {
		v = v.Elem()
	}

	if v.Type().Kind() != reflect.Map {
		dbgPrint("error %v", v.Type().Kind())
		return fmt.Errorf("insertIntoMap parent type is %T, must be map", parentMap)
	}

	v.SetMapIndex(kv, vv)

	return nil
}

// ToIntValue returns 0, false if val is not a number type, otherwise it returns the int value of val.
func ToIntValue(val interface{}) (int, bool) {
	if IsValueNil(val) {
		return 0, false
	}
	v := reflect.ValueOf(val)
	switch {
	case IsIntKind(v.Kind()):
		return int(v.Int()), true
	case IsUintKind(v.Kind()):
		return int(v.Uint()), true
	}
	return 0, false
}

// IsIntKind reports whether k is an integer kind of any size.
func IsIntKind(k reflect.Kind) bool {
	switch k {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return true
	}
	return false
}

// IsUintKind reports whether k is an unsigned integer kind of any size.
func IsUintKind(k reflect.Kind) bool {
	switch k {
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return true
	}
	return false
}
