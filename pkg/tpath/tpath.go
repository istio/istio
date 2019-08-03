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

/*
Package tpath contains functions for traversing and updating a tree constructed from yaml or json.Unmarshal.
Nodes in such trees have the form map[interface{}]interface{} or map[interface{}][]interface{}.
For some tree updates, like delete or append, it's necessary to have access to the parent node. PathContext is a
tree constructed during tree traversal that gives access to ancestor nodes all the way up to the root, which can be
used for this purpose.
*/
package tpath

import (
	"fmt"
	"reflect"
	"regexp"

	"github.com/kylelemons/godebug/pretty"

	"istio.io/operator/pkg/util"
	"istio.io/pkg/log"
)

var (
	scope = log.RegisterScope("tpath", "tree traverser", 0)
)

// PathContext provides a means for traversing a tree towards the root.
type PathContext struct {
	// Parent in the Parent of this PathContext.
	Parent *PathContext
	// KeyToChild is the key required to reach the child.
	KeyToChild interface{}
	// Node is the actual Node in the data tree.
	Node interface{}
}

// String implements the Stringer interface.
func (nc *PathContext) String() string {
	ret := "\n--------------- NodeContext ------------------\n"
	ret += fmt.Sprintf("Parent.Node=\n%s\n", pretty.Sprint(nc.Parent.Node))
	ret += fmt.Sprintf("KeyToChild=%v\n", nc.Parent.KeyToChild)
	ret += fmt.Sprintf("Node=\n%s\n", pretty.Sprint(nc.Node))
	ret += "----------------------------------------------\n"
	return ret
}

// GetPathContext returns the PathContext for the Node which has the given path from root.
// It returns false and and no error if the given path is not found, or an error code in other error situations, like
// a malformed path.
// It also creates a tree of PathContexts during the traversal so that Parent nodes can be updated if required. This is
// required when (say) appending to a list, where the parent list itself must be updated.
func GetPathContext(root interface{}, path util.Path) (*PathContext, bool, error) {
	return getPathContext(&PathContext{Node: root}, path, path, false)
}

// getPathContext is the internal implementation of GetPathContext.
// If createMissing is true, it creates any missing map (but NOT list) path entries in root.
func getPathContext(nc *PathContext, fullPath, remainPath util.Path, createMissing bool) (*PathContext, bool, error) {
	scope.Debugf("getPathContext remainPath=%s, Node=%s", remainPath, pretty.Sprint(nc.Node))
	if len(remainPath) == 0 {
		return nc, true, nil
	}
	pe := remainPath[0]

	v := reflect.ValueOf(nc.Node)
	if v.Kind() == reflect.Ptr {
		v = v.Elem()
	}
	if v.Kind() == reflect.Interface {
		v = v.Elem()
	}
	ncNode := v.Interface()

	// For list types, we need a key to identify the selected list item. This can be either a a value key of the
	// form :matching_value in the case of a leaf list, or a matching key:value in the case of a non-leaf list.
	if lst, ok := ncNode.([]interface{}); ok {
		scope.Debug("list type")
		for idx, le := range lst {
			// non-leaf list, expect to match item by key:value.
			if lm, ok := le.(map[interface{}]interface{}); ok {
				k, v, err := util.PathKV(pe)
				if err != nil {
					return nil, false, fmt.Errorf("path %s: %s", fullPath, err)
				}
				if stringsEqual(lm[k], v) {
					scope.Debugf("found matching kv %v:%v", k, v)
					nn := &PathContext{
						Parent: nc,
						Node:   lm,
					}
					nc.KeyToChild = idx
					nn.KeyToChild = k
					if len(remainPath) == 1 {
						scope.Debug("KV terminate")
						return nn, true, nil
					}
					return getPathContext(nn, fullPath, remainPath[1:], createMissing)
				}
				continue
			}
			// leaf list, match based on value.
			v, err := util.PathV(pe)
			if err != nil {
				return nil, false, fmt.Errorf("path %s: %s", fullPath, err)
			}
			if matchesRegex(v, le) {
				scope.Debugf("found matching key %v, index %d", le, idx)
				nn := &PathContext{
					Parent: nc,
					Node:   le,
				}
				nc.KeyToChild = idx
				return getPathContext(nn, fullPath, remainPath[1:], createMissing)
			}
		}
		return nil, false, fmt.Errorf("path %s: element %s not found", fullPath, pe)
	}

	if util.IsMap(ncNode) {
		scope.Debug("map type")
		var nn interface{}
		if m, ok := ncNode.(map[interface{}]interface{}); ok {
			nn, ok = m[pe]
			if !ok {
				if createMissing {
					m[pe] = make(map[interface{}]interface{})
					nn = m[pe]
				} else {
					return nil, false, fmt.Errorf("path not found at element %s in path %s", pe, fullPath)
				}
			}
		}
		if m, ok := ncNode.(map[string]interface{}); ok {
			nn, ok = m[pe]
			if !ok {
				if createMissing {
					m[pe] = make(map[string]interface{})
					nn = m[pe]
				} else {
					return nil, false, fmt.Errorf("path not found at element %s in path %s", pe, fullPath)
				}
			}
		}

		npc := &PathContext{
			Parent: nc,
			Node:   nn,
		}
		if util.IsSlice(nn) {
			npc.Node = &nn
		}
		nc.KeyToChild = pe
		return getPathContext(npc, fullPath, remainPath[1:], createMissing)
	}

	return nil, false, fmt.Errorf("leaf type %T in non-leaf Node %s", nc.Node, remainPath)
}

// WriteNode writes value to the tree in root at the given path, creating any required missing internal nodes in path.
func WriteNode(root interface{}, path util.Path, value interface{}) error {
	pc, _, err := getPathContext(&PathContext{Node: root}, path, path, true)
	if err != nil {
		return err
	}
	return WritePathContext(pc, value)
}

// WritePathContext writes the given value to the Node in the given PathContext.
func WritePathContext(nc *PathContext, value interface{}) error {
	scope.Debugf("WritePathContext PathContext=%s, value=%v", nc, value)

	switch {
	case value == nil:
		scope.Debug("delete")
		switch {
		case nc.Parent != nil && isSliceOrPtrInterface(nc.Parent.Node):
			if err := util.DeleteFromSlicePtr(nc.Parent.Node, nc.Parent.KeyToChild.(int)); err != nil {
				return err
			}
			// FIXME
			if isMapOrInterface(nc.Parent.Parent.Node) {
				if err := util.InsertIntoMap(nc.Parent.Parent.Node, nc.Parent.Parent.KeyToChild, nc.Parent.Node); err != nil {
					return err
				}
			}
		}
	default:
		switch {
		case isSliceOrPtrInterface(nc.Parent.Node):
			idx := nc.Parent.KeyToChild.(int)
			if idx == -1 {
				scope.Debug("insert")

			} else {
				scope.Debugf("update index %d\n", idx)
				if err := util.UpdateSlicePtr(nc.Parent.Node, idx, value); err != nil {
					return err
				}
			}
		default:
			scope.Debug("leaf update")
			if isMapOrInterface(nc.Parent.Node) {
				if err := util.InsertIntoMap(nc.Parent.Node, nc.Parent.KeyToChild, value); err != nil {
					return err
				}
			}
		}
	}

	return nil
}

// TODO Merge this into existing WritePathContext method
// DeleteFromTree sets value at path of input untyped tree to nil
func DeleteFromTree(valueTree map[string]interface{}, path util.Path, remainPath util.Path) (bool, error) {
	if len(remainPath) == 0 {
		return false, nil
	}
	for key, val := range valueTree {
		if key == remainPath[0] {
			// found the path to delete value
			if len(remainPath) == 1 {
				valueTree[key] = nil
				return true, nil
			}
			remainPath = remainPath[1:]
			switch node := val.(type) {
			case map[string]interface{}:
				return DeleteFromTree(node, path, remainPath)
			case []interface{}:
				for _, newNode := range node {
					newMap, ok := newNode.(map[string]interface{})
					if !ok {
						return false, fmt.Errorf("fail to convert []interface{} to map[string]interface{}")
					}
					found, err := DeleteFromTree(newMap, path, remainPath)
					if found && err == nil {
						return found, nil
					}
				}
			// leaf
			default:
				return false, nil
			}
		}
	}
	return false, nil
}

func stringsEqual(a, b interface{}) bool {
	return fmt.Sprint(a) == fmt.Sprint(b)
}

// matchesRegex reports whether str regex matches pattern.
func matchesRegex(pattern, str interface{}) bool {
	match, err := regexp.MatchString(fmt.Sprint(pattern), fmt.Sprint(str))
	if err != nil {
		log.Errorf("bad regex expression %s", fmt.Sprint(pattern))
		return false
	}
	scope.Debugf("%v regex %v? %v\n", pattern, str, match)
	return match
}

// isSliceOrPtrInterface reports whether v is a slice, a ptr to slice or interface to slice.
func isSliceOrPtrInterface(v interface{}) bool {
	vv := reflect.ValueOf(v)
	if vv.Kind() == reflect.Ptr {
		vv = vv.Elem()
	}
	if vv.Kind() == reflect.Interface {
		vv = vv.Elem()
	}
	return vv.Kind() == reflect.Slice
}

// isMapOrInterface reports whether v is a map, or interface to a map.
func isMapOrInterface(v interface{}) bool {
	vv := reflect.ValueOf(v)
	if vv.Kind() == reflect.Interface {
		vv = vv.Elem()
	}
	return vv.Kind() == reflect.Map
}
