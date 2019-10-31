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

package compare

import (
	"fmt"
	"path/filepath"
	"reflect"
	"strings"

	"github.com/google/go-cmp/cmp"
	"sigs.k8s.io/yaml"

	"istio.io/operator/pkg/tpath"
	"istio.io/pkg/log"
)

// YAMLCmpReporter is a custom reporter to generate tree based diff for YAMLs, used by cmp.Equal().
type YAMLCmpReporter struct {
	path     cmp.Path
	diffTree map[string]interface{}
}

// PushStep implements interface to keep track of current path by pushing.
// a step into YAMLCmpReporter.path
func (r *YAMLCmpReporter) PushStep(ps cmp.PathStep) {
	r.path = append(r.path, ps)
}

// PopStep implements interface to keep track of current path by popping a step out.
// of YAMLCmpReporter.path
func (r *YAMLCmpReporter) PopStep() {
	r.path = r.path[:len(r.path)-1]
}

// Report implements interface to add diff path into YAMLCmpReporter.diffTree.
func (r *YAMLCmpReporter) Report(rs cmp.Result) {
	if !rs.Equal() {
		vx, vy := r.path.Last().Values()
		var dm string
		isNonEmptyX := isValidAndNonEmpty(vx)
		isNonEmptyY := isValidAndNonEmpty(vy)
		if isNonEmptyX && !isNonEmptyY {
			dm = fmt.Sprintf("%v ->", vx)
		} else if !isNonEmptyX && isNonEmptyY {
			dm = fmt.Sprintf("-> %v", vy)
		} else if isNonEmptyX && isNonEmptyY {
			dm = fmt.Sprintf("%v -> %v", vx, vy)
		} else {
			// ignore the case that both x and y are invalid or empty
			return
		}
		if r.diffTree == nil {
			r.diffTree = make(map[string]interface{})
		}
		if err := tpath.WriteNode(r.diffTree, pathToStringList(r.path), dm); err != nil {
			panic(err)
		}
	}
}

func isValidAndNonEmpty(v reflect.Value) bool {
	if !v.IsValid() {
		return false
	}
	k := v.Kind()
	switch k {
	case reflect.Interface:
		return isValidAndNonEmpty(v.Elem())
	case reflect.Array, reflect.Chan, reflect.Map, reflect.Slice, reflect.String:
		return v.Len() > 0
	}
	return true
}

// String returns a text representation of diff tree.
func (r *YAMLCmpReporter) String() string {
	if len(r.diffTree) == 0 {
		return ""
	}
	y, err := yaml.Marshal(r.diffTree)
	if err != nil {
		return err.Error()
	}
	return string(y)
}

// YAMLCmp compares two yaml texts, return a tree based diff text.
func YAMLCmp(a, b string) string {
	return YAMLCmpWithIgnore(a, b, nil)
}

// YAMLCmpWithIgnore compares two yaml texts, and ignores paths in ignorePaths.
func YAMLCmpWithIgnore(a, b string, ignorePaths []string) string {
	ao, bo := make(map[string]interface{}), make(map[string]interface{})
	if err := yaml.Unmarshal([]byte(a), &ao); err != nil {
		return err.Error()
	}
	if err := yaml.Unmarshal([]byte(b), &bo); err != nil {
		return err.Error()
	}

	if kind := ao["kind"]; kind == "ConfigMap" {
		if err := UnmarshalInlineYaml(ao, "data"); err != nil {
			log.Warnf("Unable to unmarshal ConfigMap Data, error: %v", err)
		}
	}
	if kind := bo["kind"]; kind == "ConfigMap" {
		if err := UnmarshalInlineYaml(bo, "data"); err != nil {
			log.Warnf("Unable to unmarshal ConfigMap Data, error: %v", err)
		}
	}

	var r YAMLCmpReporter
	cmp.Equal(ao, bo, cmp.Reporter(&r), genPathIgnoreOpt(ignorePaths))
	return r.String()
}

// UnmarshalInlineYaml tries to unmarshal string values in obj into YAML objects
// at a given targetPath. Side effect: this will mutate obj in place.
func UnmarshalInlineYaml(obj map[string]interface{}, targetPath string) (err error) {
	nodeList := strings.Split(targetPath, ".")
	if len(nodeList) == 0 {
		return fmt.Errorf("targetPath '%v' length is zero after split", targetPath)
	}

	cur := obj
	for _, nname := range nodeList {
		ndata, ok := cur[nname]
		if !ok || ndata == nil { // target path does not exist
			return fmt.Errorf("targetPath '%v' doest not exist in obj: '%v' is missing",
				targetPath, nname)
		}
		switch nnode := ndata.(type) {
		case map[string]interface{}:
			cur = nnode
		default: // target path type does not match
			return fmt.Errorf("targetPath '%v' doest not exist in obj: "+
				"'%v' type is not map[string]interface{}", targetPath, nname)
		}
	}

	for dk, dv := range cur {
		switch vnode := dv.(type) {
		case string:
			vo := make(map[string]interface{})
			if err := yaml.Unmarshal([]byte(vnode), &vo); err != nil {
				continue
			}
			// Replace the original text yaml tree node with yaml objects
			cur[dk] = vo
		}
	}
	return
}

// genPathIgnoreOpt returns a cmp.Option to ignore paths specified in parameter ignorePaths.
func genPathIgnoreOpt(ignorePaths []string) cmp.Option {
	return cmp.FilterPath(func(curPath cmp.Path) bool {
		cp := strings.Join(pathToStringList(curPath), ".")
		for _, ip := range ignorePaths {
			if res, err := filepath.Match(ip, cp); err == nil && res {
				return true
			}
		}
		return false
	}, cmp.Ignore())
}

func pathToStringList(path cmp.Path) (up []string) {
	for _, step := range path {
		switch t := step.(type) {
		case cmp.MapIndex:
			up = append(up, fmt.Sprintf("%v", t.Key()))
		case cmp.SliceIndex:
			up = append(up, fmt.Sprintf("%v", t.String()))
		}
	}
	return
}
