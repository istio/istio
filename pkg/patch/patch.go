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
Package patch implements a simple patching mechanism for k8s resources.
Paths are specified in the form a.b.c.[key:value].d.[list_entry_value], where:
 -  [key:value] selects a list entry in list c which contains an entry with key:value
 -  [list_entry_value] selects a list entry in list d which is a regex match of list_entry_value.

Some examples are given below. Given a resource:

kind: Deployment
metadata:
  name: istio-citadel
  namespace: istio-system
a:
  b:
  - name: n1
    value: v1
  - name: n2
    list:
    - "vv1"
    - vv2=foo

values and list entries can be added, modifed or deleted.

MODIFY

1. set v1 to v1new

  path: a.b.[name:n1].value
  value: v1

2. set vv1 to vv3

  // Note the lack of quotes around vv1 (see NOTES below).
  path: a.b.[name:n2].list.[vv1]
  value: vv3

3. set vv2=foo to vv2=bar (using regex match)

  path: a.b.[name:n2].list.[vv2]
  value: vv2=bar

DELETE

1. Delete container with name: n1

  path: a.b.[name:n1]

2. Delete list value vv1

  path: a.b.[name:n2].list.[vv1]

ADD

1. Add vv3 to list

  path: a.b.[name:n2].list
  value: vv3

2. Add new key:value to container name: n1

  path: a.b.[name:n1]
  value:
    new_attr: v3

*NOTES*
- Due to loss of string quoting during unmarshaling, keys and values should not be string quoted, even if they appear
that way in the object being patched.
- [key:value] treats ':' as a special separator character. Any ':' in the key or value string must be escaped as \:.
*/
package patch

import (
	"fmt"
	"reflect"
	"regexp"
	"strings"

	"github.com/kr/pretty"
	"gopkg.in/yaml.v2"

	"istio.io/operator/pkg/apis/istio/v1alpha2"
	"istio.io/operator/pkg/manifest"
	"istio.io/operator/pkg/util"
	"istio.io/pkg/log"
)

var (
	// DebugPackage controls verbose debugging in this package. Used for offline debugging.
	DebugPackage = false
)

// pathContext provides a means for traversing a tree towards the root.
type pathContext struct {
	// parent in the parent of this pathContext.
	parent *pathContext
	// keyToChild is the key required to reach the child.
	keyToChild interface{}
	// node is the actual node in the data tree.
	node interface{}
}

// String implements the Stringer interface.
func (nc *pathContext) String() string {
	ret := "\n--------------- NodeContext ------------------\n"
	ret += fmt.Sprintf("parent.node=\n%s\n", pretty.Sprint(nc.parent.node))
	ret += fmt.Sprintf("keyToChild=%v\n", nc.parent.keyToChild)
	ret += fmt.Sprintf("node=\n%s\n", pretty.Sprint(nc.node))
	ret += "----------------------------------------------\n"
	return ret
}

// makeNodeContext returns a pathContext created from the given object.
func makeNodeContext(obj interface{}) *pathContext {
	return &pathContext{
		node: obj,
	}
}

// YAMLManifestPatch patches a base YAML in the given namespace with a list of overlays.
// Each overlay has the format described in the K8SObjectOverlay definition.
// It returns the patched manifest YAML.
func YAMLManifestPatch(baseYAML string, namespace string, overlays []*v1alpha2.K8SObjectOverlay) (string, error) {
	baseObjs, err := manifest.ParseObjectsFromYAMLManifest(baseYAML)
	if err != nil {
		return "", err
	}

	bom := baseObjs.ToMap()
	oom := objectOverrideMap(overlays, namespace)

	var ret strings.Builder

	// Try to apply the defined overlays.
	for k, oo := range oom {
		bo := bom[k]
		if bo == nil {
			// TODO: error log overlays with no matches in any component.
			os := ""
			for k2 := range bom {
				os += k2 + "\n"
			}
			log.Errorf("Overlay for %s does not match any object in output manifest:\n%s\n\nAvailable objects are:\n%s\n",
				k, pretty.Sprint(oo), os)
			continue
		}
		patched, err := applyPatches(bo, oo)
		if err != nil {
			log.Errorf("patch error: %s", err)
			continue
		}
		if _, err := ret.Write(patched); err != nil {
			log.Errorf("write: %s", err)
		}
		if _, err := ret.WriteString("\n---\n"); err != nil {
			log.Errorf("patch WriteString error: %s", err)
			continue
		}
	}
	// Render the remaining objects with no overlays.
	for k, oo := range bom {
		if oom[k] != nil {
			// Skip objects that have overlays, these were rendered above.
			continue
		}
		oy, err := oo.YAML()
		if err != nil {
			log.Errorf("Object to YAML error (%s) for base object: \n%v", err, oo)
			continue
		}
		if _, err := ret.Write(oy); err != nil {
			log.Errorf("write: %s", err)
		}
		if _, err := ret.WriteString("\n---\n"); err != nil {
			log.Errorf("writeString: %s", err)
		}
	}
	return ret.String(), nil
}

// applyPatches applies the given patches against the given object. It returns the resulting patched YAML if successful,
// or a list of errors otherwise.
func applyPatches(base *manifest.Object, patches []*v1alpha2.K8SObjectOverlay_PathValue) (outYAML []byte, errs util.Errors) {
	bo := make(map[interface{}]interface{})
	by, err := base.YAML()
	if err != nil {
		return nil, util.NewErrs(err)
	}
	err = yaml.Unmarshal(by, bo)
	if err != nil {
		return nil, util.NewErrs(err)
	}
	for _, p := range patches {
		dbgPrint("applying path=%s, value=%s\n", p.Path, p.Value)
		path := util.PathFromString(p.Path)
		inc, err := getNode(makeNodeContext(bo), path, path)
		if err != nil {
			errs = util.AppendErr(errs, err)
			continue
		}
		errs = util.AppendErr(errs, writeNode(inc, p.Value))
	}
	oy, err := yaml.Marshal(bo)
	if err != nil {
		return nil, util.AppendErr(errs, err)
	}
	return oy, errs
}

// getNode returns the node which has the given patch from the source node given by nc.
// It creates a tree of nodeContexts during the traversal so that parent structures can be updated if required.
func getNode(nc *pathContext, fullPath, remainPath util.Path) (*pathContext, error) {
	dbgPrint("getNode remainPath=%s, node=%s", remainPath, pretty.Sprint(nc.node))
	if len(remainPath) == 0 {
		dbgPrint("terminate with nc=%s", nc)
		return nc, nil
	}
	pe := remainPath[0]

	v := reflect.ValueOf(nc.node)
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
		dbgPrint("list type")
		for idx, le := range lst {
			// non-leaf list, expect to match item by key:value.
			if lm, ok := le.(map[interface{}]interface{}); ok {
				k, v, err := util.PathKV(pe)
				if err != nil {
					return nil, fmt.Errorf("path %s: %s", fullPath, err)
				}
				if stringsEqual(lm[k], v) {
					dbgPrint("found matching kv %v:%v", k, v)
					nn := &pathContext{
						parent: nc,
						node:   lm,
					}
					nc.keyToChild = idx
					nn.keyToChild = k
					if len(remainPath) == 1 {
						dbgPrint("KV terminate")
						return nn, nil
					}
					return getNode(nn, fullPath, remainPath[1:])
				}
				continue
			}
			// leaf list, match based on value.
			v, err := util.PathV(pe)
			if err != nil {
				return nil, fmt.Errorf("path %s: %s", fullPath, err)
			}
			if matchesRegex(v, le) {
				dbgPrint("found matching key %v, index %d", le, idx)
				nn := &pathContext{
					parent: nc,
					node:   le,
				}
				nc.keyToChild = idx
				return getNode(nn, fullPath, remainPath[1:])
			}
		}
		return nil, fmt.Errorf("path %s: element %s not found", fullPath, pe)
	}

	dbgPrint("interior node")
	// non-list node.
	if nnt, ok := nc.node.(map[interface{}]interface{}); ok {
		nn := nnt[pe]
		nnc := &pathContext{
			parent: nc,
			node:   nn,
		}
		if _, ok := nn.([]interface{}); ok {
			// Slices must be passed by pointer for mutations.
			nnc.node = &nn
		}
		nc.keyToChild = pe
		return getNode(nnc, fullPath, remainPath[1:])
	}

	return nil, fmt.Errorf("leaf type %T in non-leaf node %s", nc.node, remainPath)
}

// writeNode writes the given value to the node in the given pathContext.
func writeNode(nc *pathContext, value interface{}) error {
	dbgPrint("writeNode pathContext=%s, value=%v", nc, value)

	switch {
	case value == nil:
		dbgPrint("delete")
		switch {
		case nc.parent != nil && isSliceOrPtrInterface(nc.parent.node):
			if err := util.DeleteFromSlicePtr(nc.parent.node, nc.parent.keyToChild.(int)); err != nil {
				return err
			}
			// FIXME
			if isMapOrInterface(nc.parent.parent.node) {
				if err := util.InsertIntoMap(nc.parent.parent.node, nc.parent.parent.keyToChild, nc.parent.node); err != nil {
					return err
				}
			}
		}
	default:
		switch {
		case isSliceOrPtrInterface(nc.parent.node):
			idx := nc.parent.keyToChild.(int)
			if idx == -1 {
				dbgPrint("insert")

			} else {
				dbgPrint("update index %d\n", idx)
				if err := util.UpdateSlicePtr(nc.parent.node, idx, value); err != nil {
					return err
				}
			}
		default:
			dbgPrint("leaf update")
			if isMapOrInterface(nc.parent.node) {
				if err := util.InsertIntoMap(nc.parent.node, nc.parent.keyToChild, value); err != nil {
					return err
				}
			}
		}
	}

	return nil
}

// objectOverrideMap converts oos, a slice of object overlays, into a map of the same overlays where the key is the
// object manifest.Hash.
func objectOverrideMap(oos []*v1alpha2.K8SObjectOverlay, namespace string) map[string][]*v1alpha2.K8SObjectOverlay_PathValue {
	ret := make(map[string][]*v1alpha2.K8SObjectOverlay_PathValue)
	for _, o := range oos {
		ret[manifest.Hash(o.Kind, namespace, o.Name)] = o.Patches
	}
	return ret
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
	dbgPrint("%v regex %v? %v\n", pattern, str, match)
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

// dbgPrint prints v if the package global variable DebugPackage is set.
// v has the same format as Printf. A trailing newline is added to the output.
func dbgPrint(v ...interface{}) {
	if !DebugPackage {
		return
	}
	log.Infof(fmt.Sprintf(v[0].(string), v[1:]...))
}
