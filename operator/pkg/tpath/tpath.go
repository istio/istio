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
	"strconv"
	"strings"

	yaml2 "github.com/ghodss/yaml"
	"github.com/kylelemons/godebug/pretty"
	"gopkg.in/yaml.v2"

	"istio.io/istio/operator/pkg/util"
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
	if nc.Parent != nil {
		ret += fmt.Sprintf("Parent.Node=\n%s\n", pretty.Sprint(nc.Parent.Node))
		ret += fmt.Sprintf("KeyToChild=%v\n", nc.Parent.KeyToChild)
	}

	ret += fmt.Sprintf("Node=\n%s\n", pretty.Sprint(nc.Node))
	ret += "----------------------------------------------\n"

	return ret
}

// GetPathContext returns the PathContext for the Node which has the given path from root.
// It returns false and and no error if the given path is not found, or an error code in other error situations, like
// a malformed path.
// It also creates a tree of PathContexts during the traversal so that Parent nodes can be updated if required. This is
// required when (say) appending to a list, where the parent list itself must be updated.
func GetPathContext(root interface{}, path util.Path, createMissing bool) (*PathContext, bool, error) {
	return getPathContext(&PathContext{Node: root}, path, path, createMissing)
}

// getPathContext is the internal implementation of GetPathContext.
// If createMissing is true, it creates any missing map (but NOT list) path entries in root.
func getPathContext(nc *PathContext, fullPath, remainPath util.Path, createMissing bool) (*PathContext, bool, error) {
	scope.Debugf("getPathContext remainPath=%s, Node=%s", remainPath, pretty.Sprint(nc.Node))
	if len(remainPath) == 0 {
		return nc, true, nil
	}
	pe := remainPath[0]

	if nc.Node == nil {
		// Otherwise we panic on bad input
		return nil, false, fmt.Errorf("node %s is zero", pe)
	}

	v := reflect.ValueOf(nc.Node)
	if v.Kind() == reflect.Ptr || v.Kind() == reflect.Interface {
		v = v.Elem()
	}
	ncNode := v.Interface()

	// For list types, we need a key to identify the selected list item. This can be either a a value key of the
	// form :matching_value in the case of a leaf list, or a matching key:value in the case of a non-leaf list.
	if lst, ok := ncNode.([]interface{}); ok {
		scope.Debug("list type")
		// If the path element has the form [N], a list element is being selected by index. Return the element at index
		// N if it exists.
		if util.IsNPathElement(pe) {
			idx, err := util.PathN(pe)
			if err != nil {
				return nil, false, fmt.Errorf("path %s, index %s: %s", fullPath, pe, err)
			}
			var foundNode interface{}
			if idx >= len(lst) {
				if !createMissing {
					return nil, false, fmt.Errorf("index %d exceeds list length %d at path %s", idx, len(lst), remainPath)
				}
				foundNode = make(map[string]interface{})
			} else {
				foundNode = lst[idx]
			}
			nn := &PathContext{
				Parent: nc,
				Node:   foundNode,
			}
			nc.KeyToChild = idx
			return getPathContext(nn, fullPath, remainPath[1:], createMissing)
		}

		// Otherwise the path element must have form [key:value]. In this case, go through all list elements, which
		// must have map type, and try to find one which has a matching key:value.
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
			// repeat of the block above for the case where tree unmarshals to map[string]interface{}. There doesn't
			// seem to be a way to merge this case into the above block.
			if lm, ok := le.(map[string]interface{}); ok {
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
			// leaf list, expect path element [V], match based on value V.
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
				// remainPath == 1 means the patch is creation of a new leaf.
				if createMissing || len(remainPath) == 1 {
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
				// remainPath == 1 means the patch is creation of a new leaf.
				if createMissing || len(remainPath) == 1 {
					nextElementNPath := len(remainPath) > 1 && util.IsNPathElement(remainPath[1])
					if nextElementNPath {
						scope.Debug("map type, slice child")
						m[pe] = make([]interface{}, 0)
					} else {
						scope.Debug("map type, map child")
						m[pe] = make(map[string]interface{})
					}
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
		// for slices, use the address so that the slice can be mutated.
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
	return WritePathContext(pc, value, false)
}

// MergeNode merges value to the tree in root at the given path, creating any required missing internal nodes in path.
func MergeNode(root interface{}, path util.Path, value interface{}) error {
	pc, _, err := getPathContext(&PathContext{Node: root}, path, path, true)
	if err != nil {
		return err
	}
	return WritePathContext(pc, value, true)
}

// mergeConditional returns a merge of newVal and originalVal if merge is true, otherwise it returns newVal.
func mergeConditional(newVal, originalVal interface{}, merge bool) (interface{}, error) {
	if !merge || util.IsValueNilOrDefault(originalVal) {
		return newVal, nil
	}
	newS, err := yaml.Marshal(newVal)
	if err != nil {
		return nil, err
	}
	if util.IsYAMLEmpty(string(newS)) {
		return originalVal, nil
	}
	originalS, err := yaml.Marshal(originalVal)
	if err != nil {
		return nil, err
	}
	if util.IsYAMLEmpty(string(originalS)) {
		return newVal, nil
	}

	mergedS, err := util.OverlayYAML(string(originalS), string(newS))
	if err != nil {
		return nil, err
	}

	if util.IsMap(originalVal) {
		// For JSON compatibility
		out := make(map[string]interface{})
		if err := yaml.Unmarshal([]byte(mergedS), &out); err != nil {
			return nil, err
		}
		return out, nil
	}
	// For scalars and slices, copy the type
	out := originalVal
	if err := yaml.Unmarshal([]byte(mergedS), &out); err != nil {
		return nil, err
	}
	return out, nil
}

// WritePathContext writes the given value to the Node in the given PathContext.
func WritePathContext(nc *PathContext, value interface{}, merge bool) error {
	scope.Debugf("WritePathContext PathContext=%s, value=%v", nc, value)

	switch {
	case util.IsValueNil(value):
		scope.Debug("delete")
		switch {
		case nc.Parent != nil && isSliceOrPtrInterface(nc.Parent.Node):
			if err := util.DeleteFromSlicePtr(nc.Parent.Node, nc.Parent.KeyToChild.(int)); err != nil {
				return err
			}
			// FIXME: assumes parent is a map, will not work if it is a slice.
			if isMapOrInterface(nc.Parent.Parent.Node) {
				if err := util.InsertIntoMap(nc.Parent.Parent.Node, nc.Parent.Parent.KeyToChild, nc.Parent.Node); err != nil {
					return err
				}
			}
		}
	default:
		return setPathContext(nc, value, merge)
	}

	return nil
}

// setPathContext writes the given value to the Node in the given PathContext,
// enlarging all PathContext lists to ensure all indexes are valid.
func setPathContext(nc *PathContext, value interface{}, merge bool) error {
	err := setValueContext(nc, value, merge)
	if err != nil {
		return err
	}

	// If the path included insertions, process them now
	if nc.Parent.Parent == nil {
		return nil
	}
	return setPathContext(nc.Parent, nc.Parent.Node, false) // note: tail recursive
}

// setValueContext writes the given value to the Node in the given PathContext.
// If setting the value requires growing the final slice, grows it.
func setValueContext(nc *PathContext, value interface{}, merge bool) error {
	vv := value
	// If value type is a string it could either be a literal string or a map type passed as a string. Try to unmarshal
	// to discover it's the latter.
	if reflect.TypeOf(vv).Kind() == reflect.String && strings.Contains(value.(string), ":") {
		nv := make(map[string]interface{})
		if err := yaml2.Unmarshal([]byte(vv.(string)), &nv); err == nil {
			vv = nv
		}
		// got error, continue with value as a string.
	}
	switch parentNode := nc.Parent.Node.(type) {
	case *interface{}:
		switch vParentNode := (*parentNode).(type) {
		case []interface{}:
			idx := nc.Parent.KeyToChild.(int)
			if idx == -1 {
				// Treat -1 as insert-at-end of list
				idx = len(vParentNode)
			}

			if idx >= len(vParentNode) {
				newElements := make([]interface{}, idx-len(vParentNode)+1)
				vParentNode = append(vParentNode, newElements...)
				*parentNode = vParentNode
			}

			merged, err := mergeConditional(vv, nc.Node, merge)
			if err != nil {
				return err
			}

			vParentNode[idx] = merged
			nc.Node = merged
		default:
			return fmt.Errorf("don't know about vtype %T", vParentNode)
		}
	case map[string]interface{}:
		key := nc.Parent.KeyToChild.(string)

		// Update is treated differently depending on whether the value is a scalar or map type. If scalar,
		// insert a new element into the terminal node, otherwise replace the terminal node with the new subtree.
		if ncNode, ok := nc.Node.(*interface{}); ok {
			switch vNcNode := (*ncNode).(type) {
			case []interface{}:
				switch vv.(type) {
				case map[string]interface{}:
					// the vv is a map, and the node is a slice
					mergedValue := append(vNcNode, vv)
					parentNode[key] = mergedValue
				case *interface{}:
					merged, err := mergeConditional(vv, vNcNode, merge)
					if err != nil {
						return err
					}

					parentNode[key] = merged
					nc.Node = merged
				default:
					// the vv is an basic JSON type (int, float, string, bool)
					vv = append(vNcNode, vv)
					parentNode[key] = vv
					nc.Node = vv
				}
			default:
				return fmt.Errorf("don't know about vnc type %T", vNcNode)
			}
		} else {
			// the vv is an basic JSON type (int, float, string, bool); or a map[string]interface{}
			parentNode[key] = vv
			nc.Node = vv
		}
	// TODO `map[interface{}]interface{}` is used by tests in operator/cmd/mesh, we should add our own tests
	case map[interface{}]interface{}:
		key := nc.Parent.KeyToChild.(string)
		parentNode[key] = vv
		nc.Node = vv
	default:
		return fmt.Errorf("don't know about type %T", parentNode)
	}

	return nil
}

func IsLeafNode(treeNode interface{}) bool {
	switch treeNode.(type) {
	case map[string]interface{}, []interface{}:
		return false
	default:
		return true
	}
}

// GetFromTreePath returns the value at path from the given tree, or false if the path does not exist.
func GetFromTreePath(inputTree map[string]interface{}, path util.Path) (interface{}, bool, error) {
	log.Debugf("GetFromTreePath path=%s", path)
	if len(path) == 0 {
		return nil, false, fmt.Errorf("path is empty")
	}
	node, found := GetNodeByPath(inputTree, path)
	return node, found, nil
}

// GetNodeByPath returns the value at path from the given tree, or false if the path does not exist.
func GetNodeByPath(treeNode interface{}, path util.Path) (interface{}, bool) {
	if len(path) == 0 || treeNode == nil {
		return nil, false
	}
	switch nt := treeNode.(type) {
	case map[interface{}]interface{}:
		val := nt[path[0]]
		if val == nil {
			return nil, false
		}
		if len(path) == 1 {
			return val, true
		}
		return GetNodeByPath(val, path[1:])
	case map[string]interface{}:
		val := nt[path[0]]
		if val == nil {
			return nil, false
		}
		if len(path) == 1 {
			return val, true
		}
		return GetNodeByPath(val, path[1:])
	case []interface{}:
		idx, err := strconv.Atoi(path[0])
		if err != nil {
			return nil, false
		}
		if idx >= len(nt) {
			return nil, false
		}
		val := nt[idx]
		return GetNodeByPath(val, path[1:])
	default:
		return nil, false
	}
}

// TODO Merge this into existing WritePathContext method (istio/istio#15494)
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

// GetFromStructPath returns the value at path from the given node, or false if the path does not exist.
func GetFromStructPath(node interface{}, path string) (interface{}, bool, error) {
	return getFromStructPath(node, util.PathFromString(path))
}

// getFromStructPath is the internal implementation of GetFromStructPath which recurses through a tree of Go structs
// given a path. It terminates when the end of the path is reached or a path element does not exist.
func getFromStructPath(node interface{}, path util.Path) (interface{}, bool, error) {
	scope.Debugf("getFromStructPath path=%s, node(%T)", path, node)
	if len(path) == 0 {
		scope.Debugf("getFromStructPath returning node(%T)%v", node, node)
		return node, !util.IsValueNil(node), nil
	}
	val := reflect.ValueOf(node)
	kind := reflect.TypeOf(node).Kind()
	var structElems reflect.Value
	if len(path) == 0 {
		return nil, false, fmt.Errorf("getFromStructPath path %s, unsupported leaf type %T", path, node)
	}
	switch kind {
	case reflect.Map:
		if path[0] == "" {
			return nil, false, fmt.Errorf("getFromStructPath path %s, empty map key value", path)
		}
		return getFromStructPath(val.MapIndex(reflect.ValueOf(path[0])).Interface(), path[1:])
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
func SetFromPath(node interface{}, path string, out interface{}) (bool, error) {
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
func Set(val, out interface{}) error {
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

// AddSpecRoot adds a root node called "spec" to the given tree and returns the resulting tree.
func AddSpecRoot(tree string) (string, error) {
	t, nt := make(map[string]interface{}), make(map[string]interface{})
	if err := yaml.Unmarshal([]byte(tree), &t); err != nil {
		return "", err
	}
	nt["spec"] = t
	out, err := yaml.Marshal(nt)
	if err != nil {
		return "", err
	}
	return string(out), nil
}

// GetSpecSubtree returns the subtree under "spec".
func GetSpecSubtree(yml string) (string, error) {
	return GetConfigSubtree(yml, "spec")
}

// GetConfigSubtree returns the subtree at the given path.
func GetConfigSubtree(manifest, path string) (string, error) {
	root := make(map[string]interface{})
	if err := yaml2.Unmarshal([]byte(manifest), &root); err != nil {
		return "", err
	}

	nc, _, err := GetPathContext(root, util.PathFromString(path), false)
	if err != nil {
		return "", err
	}
	out, err := yaml2.Marshal(nc.Node)
	if err != nil {
		return "", err
	}
	return string(out), nil
}
