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

/*
struct.go contains functions for traversing and modifying trees of Go structs.
*/
package tpath

import (
	"fmt"
	"reflect"
	"strconv"

	"google.golang.org/protobuf/types/known/structpb"

	"istio.io/istio/operator/pkg/util"
)

// GetFromStructPath returns the value at path from the given node, or false if the path does not exist.
func GetFromStructPath(node any, path string) (any, bool, error) {
	return getFromStructPath(node, util.PathFromString(path))
}

// getFromStructPath is the internal implementation of GetFromStructPath which recurses through a tree of Go structs
// given a path. It terminates when the end of the path is reached or a path element does not exist.
func getFromStructPath(node any, path util.Path) (any, bool, error) {
	scope.Debugf("getFromStructPath path=%s, node(%T)", path, node)
	if len(path) == 0 {
		scope.Debugf("getFromStructPath returning node(%T)%v", node, node)
		return node, !util.IsValueNil(node), nil
	}
	// For protobuf types, switch them out with standard types; otherwise we will traverse protobuf internals rather
	// than the standard representation
	if v, ok := node.(*structpb.Struct); ok {
		node = v.AsMap()
	}
	if v, ok := node.(*structpb.Value); ok {
		node = v.AsInterface()
	}
	val := reflect.ValueOf(node)
	kind := reflect.TypeOf(node).Kind()
	var structElems reflect.Value

	switch kind {
	case reflect.Map:
		if path[0] == "" {
			return nil, false, fmt.Errorf("getFromStructPath path %s, empty map key value", path)
		}
		mapVal := val.MapIndex(reflect.ValueOf(path[0]))
		if !mapVal.IsValid() {
			return nil, false, fmt.Errorf("getFromStructPath path %s, path does not exist", path)
		}
		return getFromStructPath(mapVal.Interface(), path[1:])
	case reflect.Slice:
		idx, err := strconv.Atoi(path[0])
		if err != nil {
			return nil, false, fmt.Errorf("getFromStructPath path %s, expected index number, got %s", path, path[0])
		}
		return getFromStructPath(val.Index(idx).Interface(), path[1:])
	case reflect.Ptr:
		structElems = reflect.ValueOf(node).Elem()
		if !util.IsStruct(structElems) {
			return nil, false, fmt.Errorf("getFromStructPath path %s, expected struct ptr, got %T", path, node)
		}
	default:
		return nil, false, fmt.Errorf("getFromStructPath path %s, unsupported type %T", path, node)
	}

	if util.IsNilOrInvalidValue(structElems) {
		return nil, false, nil
	}

	for i := 0; i < structElems.NumField(); i++ {
		fieldName := structElems.Type().Field(i).Name

		if fieldName != path[0] {
			continue
		}

		fv := structElems.Field(i)
		return getFromStructPath(fv.Interface(), path[1:])
	}

	return nil, false, nil
}

// SetFromPath sets out with the value at path from node. out is not set if the path doesn't exist or the value is nil.
// All intermediate along path must be type struct ptr. Out must be either a struct ptr or map ptr.
// TODO: move these out to a separate package (istio/istio#15494).
func SetFromPath(node any, path string, out any) (bool, error) {
	val, found, err := GetFromStructPath(node, path)
	if err != nil {
		return false, err
	}
	if !found {
		return false, nil
	}

	return true, Set(val, out)
}

// Set sets out with the value at path from node. out is not set if the path doesn't exist or the value is nil.
func Set(val, out any) error {
	// Special case: map out type must be set through map ptr.
	if util.IsMap(val) && util.IsMapPtr(out) {
		reflect.ValueOf(out).Elem().Set(reflect.ValueOf(val))
		return nil
	}
	if util.IsSlice(val) && util.IsSlicePtr(out) {
		reflect.ValueOf(out).Elem().Set(reflect.ValueOf(val))
		return nil
	}

	if reflect.TypeOf(val) != reflect.TypeOf(out) {
		return fmt.Errorf("setFromPath from type %T != to type %T, %v", val, out, util.IsSlicePtr(out))
	}

	if !reflect.ValueOf(out).CanSet() {
		return fmt.Errorf("can't set %v(%T) to out type %T", val, val, out)
	}
	reflect.ValueOf(out).Set(reflect.ValueOf(val))
	return nil
}
