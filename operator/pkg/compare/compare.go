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

package compare

import (
	"fmt"
	"io"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"

	"github.com/google/go-cmp/cmp"
	"sigs.k8s.io/yaml"

	"istio.io/istio/operator/pkg/object"
	"istio.io/istio/operator/pkg/tpath"
	"istio.io/istio/operator/pkg/util"
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
	return YAMLCmpWithIgnore(a, b, nil, "")
}

// YAMLCmpWithIgnore compares two yaml texts, and ignores paths in ignorePaths.
func YAMLCmpWithIgnore(a, b string, ignorePaths []string, ignoreYaml string) string {
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

	ignoreYamlOpt, err := genYamlIgnoreOpt(ignoreYaml)
	if err != nil {
		return err.Error()
	}

	var r YAMLCmpReporter
	cmp.Equal(ao, bo, cmp.Reporter(&r), genPathIgnoreOpt(ignorePaths), ignoreYamlOpt)
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
func genYamlIgnoreOpt(yamlStr string) (cmp.Option, error) {
	tree := make(map[string]interface{})
	if err := yaml.Unmarshal([]byte(yamlStr), &tree); err != nil {
		return nil, err
	}
	return cmp.FilterPath(func(curPath cmp.Path) bool {
		up := pathToStringList(curPath)
		treeNode, found, _ := tpath.Find(tree, up)
		return found && IsLeafNode(treeNode)
	}, cmp.Ignore()), nil
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
			// Create an element, but never an NPath
			s := t.String()
			if util.IsNPathElement(s) {
				// Convert e.g. [0] to [#0]
				s = fmt.Sprintf("%c%c%s", s[0], '#', s[1:])
			}
			up = append(up, s)
		}
	}
	return
}

func ManifestDiff(a, b string, verbose bool) (string, error) {
	ao, err := object.ParseK8sObjectsFromYAMLManifest(a)
	if err != nil {
		return "", err
	}
	bo, err := object.ParseK8sObjectsFromYAMLManifest(b)
	if err != nil {
		return "", err
	}

	aom, bom := ao.ToMap(), bo.ToMap()
	return manifestDiff(aom, bom, nil, verbose)
}

// ManifestDiffWithSelect checks the manifest differences with selected and ignored resources.
// The selected filter will apply before the ignored filter.
func ManifestDiffWithRenameSelectIgnore(a, b, renameResources, selectResources, ignoreResources string, verbose bool) (string, error) {
	rnm := getKeyValueMap(renameResources)
	sm := getObjPathMap(selectResources)
	im := getObjPathMap(ignoreResources)

	ao, err := object.ParseK8sObjectsFromYAMLManifest(a)
	if err != nil {
		return "", err
	}
	aom := ao.ToMap()

	bo, err := object.ParseK8sObjectsFromYAMLManifest(b)
	if err != nil {
		return "", err
	}
	bom := bo.ToMap()

	if len(rnm) != 0 {
		aom, err = renameResource(aom, rnm)
		if err != nil {
			return "", err
		}
	}

	aosm, err := filterResourceWithSelectAndIgnore(aom, sm, im)
	if err != nil {
		return "", err
	}
	bosm, err := filterResourceWithSelectAndIgnore(bom, sm, im)
	if err != nil {
		return "", err
	}

	return manifestDiff(aosm, bosm, im, verbose)
}

// FilterManifest selects and ignores subset from the manifest string
func FilterManifest(ms string, selectResources string, ignoreResources string) (string, error) {
	sm := getObjPathMap(selectResources)
	im := getObjPathMap(ignoreResources)
	ao, err := object.ParseK8sObjectsFromYAMLManifestFailOption(ms, false)
	if err != nil {
		return "", err
	}
	aom := ao.ToMap()
	slrs, err := filterResourceWithSelectAndIgnore(aom, sm, im)
	if err != nil {
		return "", err
	}
	var sb strings.Builder
	for _, ko := range slrs {
		yl, err := ko.YAML()
		if err != nil {
			return "", err
		}
		sb.WriteString(string(yl) + object.YAMLSeparator)
	}
	k8sObjects, err := object.ParseK8sObjectsFromYAMLManifest(sb.String())
	if err != nil {
		return "", err
	}
	k8sObjects.Sort(object.DefaultObjectOrder())
	sortdManifests, err := k8sObjects.YAMLManifest()
	if err != nil {
		return "", err
	}
	return sortdManifests, nil
}

// renameResource filter the input resources with selected and ignored filter.
func renameResource(iom map[string]*object.K8sObject, rnm map[string]string) (map[string]*object.K8sObject, error) {
	oom := make(map[string]*object.K8sObject)
	for name, obj := range iom {
		isRenamed := false
		for fromPat, toPat := range rnm {
			fromRe, err := buildResourceRegexp(strings.TrimSpace(fromPat))
			if err != nil {
				return nil, fmt.Errorf("error building the regexp from "+
					"rename-from string: %v, error: %v", fromPat, err)
			}
			if fromRe.MatchString(name) {
				fromList := strings.Split(name, ":")
				if len(fromList) != 3 {
					return nil, fmt.Errorf("failed to split the old name,"+
						" length != 3: %v", name)
				}
				toList := strings.Split(toPat, ":")
				if len(toList) != 3 {
					return nil, fmt.Errorf("failed to split the rename-to string,"+
						" length != 3: %v", toPat)
				}

				// Use the old name if toList has "*" or ""
				// Otherwise, use the name in toList
				newList := make([]string, 3)
				for i := range toList {
					if toList[i] == "" || toList[i] == "*" {
						newList[i] = fromList[i]
					} else {
						newList[i] = toList[i]
					}
				}
				tk := strings.Join(newList, ":")
				oom[tk] = obj
				isRenamed = true
			}
		}
		if !isRenamed {
			oom[name] = obj
		}
	}
	return oom, nil
}

// filterResourceWithSelectAndIgnore filter the input resources with selected and ignored filter.
func filterResourceWithSelectAndIgnore(aom map[string]*object.K8sObject, sm, im map[string]string) (map[string]*object.K8sObject, error) {
	aosm := make(map[string]*object.K8sObject)
	for ak, av := range aom {
		for selected := range sm {
			re, err := buildResourceRegexp(strings.TrimSpace(selected))
			if err != nil {
				return nil, fmt.Errorf("error building the resource regexp: %v", err)
			}
			if re.MatchString(ak) {
				aosm[ak] = av
			}
		}
		for ignored := range im {
			re, err := buildResourceRegexp(strings.TrimSpace(ignored))
			if err != nil {
				return nil, fmt.Errorf("error building the resource regexp: %v", err)
			}
			if re.MatchString(ak) {
				delete(aosm, ak)
			}
		}
	}
	return aosm, nil
}

// buildResourceRegexp translates the resource indicator to regexp.
func buildResourceRegexp(s string) (*regexp.Regexp, error) {
	hash := strings.Split(s, ":")
	for i, v := range hash {
		if v == "" || v == "*" {
			hash[i] = ".*"
		}
	}
	return regexp.Compile(strings.Join(hash, ":"))
}

// manifestDiff an internal function to compare the manifests difference specified in the input.
func manifestDiff(aom, bom map[string]*object.K8sObject, im map[string]string, verbose bool) (string, error) {
	var sb strings.Builder
	out := make(map[string]string)
	for ak, av := range aom {
		ay, err := av.YAML()
		if err != nil {
			return "", err
		}
		bo := bom[ak]
		if bo == nil {
			out[ak] = fmt.Sprintf("\n\nObject %s is missing in B:\n\n", ak)
			continue
		}
		by, err := bo.YAML()
		if err != nil {
			return "", err
		}

		var diff string
		if verbose {
			diff = util.YAMLDiff(string(ay), string(by))
		} else {
			ignorePaths := objectIgnorePaths(ak, im)
			diff = YAMLCmpWithIgnore(string(ay), string(by), ignorePaths, "")
		}

		if diff != "" {
			out[ak] = fmt.Sprintf("\n\nObject %s has diffs:\n\n%s", ak, diff)
		}
	}
	for bk := range bom {
		ao := aom[bk]
		if ao == nil {
			out[bk] = fmt.Sprintf("\n\nObject %s is missing in A:\n\n", bk)
			continue
		}
	}

	keys := make([]string, 0, len(out))
	for k := range out {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for i := range keys {
		writeStringSafe(&sb, out[keys[i]])
	}

	return sb.String(), nil
}

func getObjPathMap(rs string) map[string]string {
	rm := make(map[string]string)
	if len(rs) == 0 {
		return rm
	}
	for _, r := range strings.Split(rs, ",") {
		split := strings.Split(r, ":")
		if len(split) < 4 {
			rm[r] = ""
			continue
		}
		kind, namespace, name, path := split[0], split[1], split[2], split[3]
		obj := fmt.Sprintf("%v:%v:%v", kind, namespace, name)
		rm[obj] = path
	}
	return rm
}

func getKeyValueMap(rs string) map[string]string {
	rm := make(map[string]string)
	if len(rs) == 0 {
		return rm
	}
	for _, r := range strings.Split(rs, ",") {
		split := strings.Split(r, "->")
		if len(split) != 2 {
			continue
		}
		rm[split[0]] = split[1]
	}
	return rm
}

func objectIgnorePaths(objectName string, im map[string]string) (ignorePaths []string) {
	if im == nil {
		im = make(map[string]string)
	}
	for obj, path := range im {
		if path == "" {
			continue
		}
		re, err := buildResourceRegexp(strings.TrimSpace(obj))
		if err != nil {
			continue
		}
		if re.MatchString(objectName) {
			ignorePaths = append(ignorePaths, path)
		}
	}
	return ignorePaths
}

func writeStringSafe(sb io.StringWriter, s string) {
	_, err := sb.WriteString(s)
	if err != nil {
		log.Error(err.Error())
	}
}

// IsLeafNode reports whether the given node is a leaf, assuming internal nodes can only be maps or slices.
func IsLeafNode(node interface{}) bool {
	return !util.IsMap(node) && !util.IsSlice(node)
}
