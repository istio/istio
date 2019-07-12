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
This file contains functions for setting values in a map[string]interface{} tree, using a similar [key:value] list item
selection path format to the patch package. See patch package and unit tests here for path format examples.
*/

package translate

import (
	"fmt"

	"istio.io/operator/pkg/util"
)

// setTree sets the YAML path in the given Tree to the given value, creating any required intermediate nodes.
func setTree(root interface{}, path util.Path, value interface{}) error {
	return setTreeInternal(root, path, value, nil, "")
}

// setTreeInternal sets the YAML path in the given Tree to the given value, creating any required intermediate nodes.
// Slices are passed along with the parent map and the key to the slice so the map can be added to for new slice
// entries.
// TODO: there's some duplication between here and patch package. The latter is more general but does not support
// creating missing intermediate nodes. Investigate if these can be merged.
func setTreeInternal(root interface{}, path util.Path, value interface{}, parentMap *map[string]interface{}, keyToChild string) error {
	if len(path) == 0 {
		return fmt.Errorf("path cannot be empty")
	}

	dbgPrint("setTree %s:%v", path, value)
	pe := path[0]
	switch {
	case util.IsSlice(root):
		k, v, err := util.PathKV(pe)
		if err != nil {
			return err
		}
		sli := root.([]interface{})
		for _, svi := range sli {
			sv, ok := svi.(map[string]interface{})
			if !ok {
				return fmt.Errorf("setTree path %s: got type %T, expect map[string]interface{}", path, svi)
			}
			dbgPrint("check kv %s:%s against %s?", k, v, sv)
			if sv[k] == v {
				dbgPrint("Found matching kv %s:%s", k, v)
				if len(path) == 1 {
					valK, valV, err := getTreeRootKV(value)
					if err != nil {
						return err
					}
					sv[valK] = valV
					return nil
				}
				return setTreeInternal(sv, path[1:], value, nil, "")
			}
		}
		// KV not found, create a new slice entry with the given KV and proceed into it.
		dbgPrint("no matching kv for %s:%s, create node", k, v)
		nn := map[string]interface{}{k: v}
		(*parentMap)[keyToChild] = append(sli, nn)
		if len(path) == 1 {
			return nil
		}
		return setTreeInternal(nn, path[1:], value, nil, "")
	case util.IsMap(root):
		r := root.(map[string]interface{})
		if len(path) == 1 {
			dbgPrint("set value %v -> %v", r[pe], value)
			r[pe] = value
			return nil
		}
		if r[pe] == nil {
			r[pe] = make(map[string]interface{})
		}
		if util.IsSlice(r[pe]) {
			return setTreeInternal(r[pe], path[1:], value, &r, pe)
		}
		return setTreeInternal(r[pe], path[1:], value, &r, pe)
	default:
		if len(path) != 1 {
			// Scalar nodes must be leaves.
			return fmt.Errorf("bad node type at path %s:%T", path, root)
		}
		(*parentMap)[keyToChild] = value
	}
	return nil
}

// getTreeRootKV returns the string key and interface value for the root of the tree, which must have a single root
// and be type map[string]interface{}
func getTreeRootKV(value interface{}) (string, interface{}, error) {
	m, ok := value.(map[string]interface{})
	if !ok {
		return "", nil, fmt.Errorf("bad type in getTreeRootKV: got %T, expect map[string]interface{}", value)
	}
	// We are guaranteed trees with single baseYAML.
	for k, v := range m {
		return k, v, nil
	}
	return "", nil, nil
}
