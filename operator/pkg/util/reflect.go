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

package util

import (
	"fmt"
	"reflect"
)

// kindOf returns the reflection Kind that represents the dynamic type of value.
// If value is a nil interface value, kindOf returns reflect.Invalid.
func kindOf(value any) reflect.Kind {
	if value == nil {
		return reflect.Invalid
	}
	return reflect.TypeOf(value).Kind()
}

// IsString reports whether value is a string type.
func IsString(value any) bool {
	return kindOf(value) == reflect.String
}

// IsMap reports whether value is a map type.
func IsMap(value any) bool {
	return kindOf(value) == reflect.Map
}

// IsSlice reports whether value is a slice type.
func IsSlice(value any) bool {
	return kindOf(value) == reflect.Slice
}

// IsSliceInterfacePtr reports whether v is a slice ptr type.
func IsSliceInterfacePtr(v any) bool {
	// Must use ValueOf because Elem().Elem() type resolves dynamically.
	vv := reflect.ValueOf(v)
	return vv.Kind() == reflect.Ptr && vv.Elem().Kind() == reflect.Interface && vv.Elem().Elem().Kind() == reflect.Slice
}

// IsValueNil returns true if either value is nil, or has dynamic type {ptr,
// map, slice} with value nil.
func IsValueNil(value any) bool {
	if value == nil {
		return true
	}
	switch kindOf(value) {
	case reflect.Slice, reflect.Ptr, reflect.Map:
		return reflect.ValueOf(value).IsNil()
	}
	return false
}

// DeleteFromSlicePtr deletes an entry at index from the parent, which must be a slice ptr.
func DeleteFromSlicePtr(parentSlice any, index int) error {
	scope.Debugf("DeleteFromSlicePtr index=%d, slice=\n%v", index, parentSlice)
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

// InsertIntoMap inserts value with key into parent which must be a map, map ptr, or interface to map.
func InsertIntoMap(parentMap any, key any, value any) error {
	scope.Debugf("InsertIntoMap key=%v, value=%v, map=\n%v", key, value, parentMap)
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
		scope.Debugf("error %v", v.Type().Kind())
		return fmt.Errorf("insertIntoMap parent type is %T, must be map", parentMap)
	}

	v.SetMapIndex(kv, vv)

	return nil
}

// DeleteFromMap deletes an entry with the given key parent, which must be a map.
func DeleteFromMap(parentMap any, key any) error {
	scope.Debugf("DeleteFromMap key=%s, parent:\n%v\n", key, parentMap)
	pv := reflect.ValueOf(parentMap)

	if !IsMap(parentMap) {
		return fmt.Errorf("deleteFromMap parent type is %T, must be map", parentMap)
	}
	pv.SetMapIndex(reflect.ValueOf(key), reflect.Value{})

	return nil
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
