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
	"sort"
	"strings"

	"github.com/kr/pretty"
	"gopkg.in/yaml.v2"

	"istio.io/operator/pkg/apis/istio/v1alpha2"
	"istio.io/operator/pkg/object"
	"istio.io/operator/pkg/tpath"
	"istio.io/operator/pkg/util"
	"istio.io/pkg/log"
)

var (
	scope = log.RegisterScope("patch", "patch", 0)
)

// YAMLManifestPatch patches a base YAML in the given namespace with a list of overlays.
// Each overlay has the format described in the K8SObjectOverlay definition.
// It returns the patched manifest YAML.
func YAMLManifestPatch(baseYAML string, namespace string, overlays []*v1alpha2.K8SObjectOverlay) (string, error) {
	baseObjs, err := object.ParseK8sObjectsFromYAMLManifest(baseYAML)
	if err != nil {
		return "", err
	}

	bom := baseObjs.ToMap()
	oom := objectOverrideMap(overlays, namespace)

	var ret strings.Builder

	var errs util.Errors
	keys := make([]string, 0)
	for key := range oom {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	// Try to apply the defined overlays.
	for _, k := range keys {
		oo := oom[k]
		bo := bom[k]
		if bo == nil {
			os := ""
			for k2 := range bom {
				os += k2 + "\n"
			}
			errs = util.AppendErr(errs, fmt.Errorf("overlay for %s does not match any object in output manifest:\n%s\n\nAvailable objects are:\n%s",
				k, pretty.Sprint(oo), os))
			continue
		}
		patched, err := applyPatches(bo, oo)
		if err != nil {
			errs = util.AppendErr(errs, fmt.Errorf("patch error: %s", err))
			continue
		}
		if _, err := ret.Write(patched); err != nil {
			errs = util.AppendErr(errs, fmt.Errorf("write: %s", err))
		}
		if _, err := ret.WriteString("\n---\n"); err != nil {
			errs = util.AppendErr(errs, fmt.Errorf("patch WriteString error: %s", err))
		}
	}
	// Render the remaining objects with no overlays.
	// keep the original sorted order
	keys = make([]string, 0)
	for key := range bom {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, k := range keys {
		oo := bom[k]
		if oom[k] != nil {
			// Skip objects that have overlays, these were rendered above.
			continue
		}
		oy, err := oo.YAML()
		if err != nil {
			errs = util.AppendErr(errs, fmt.Errorf("object to YAML error (%s) for base object: \n%v", err, oo))
			continue
		}
		if _, err := ret.Write(oy); err != nil {
			errs = util.AppendErr(errs, fmt.Errorf("write: %s", err))
		}
		if _, err := ret.WriteString("\n---\n"); err != nil {
			errs = util.AppendErr(errs, fmt.Errorf("writeString: %s", err))
		}
	}
	return ret.String(), errs.ToError()
}

// applyPatches applies the given patches against the given object. It returns the resulting patched YAML if successful,
// or a list of errors otherwise.
func applyPatches(base *object.K8sObject, patches []*v1alpha2.K8SObjectOverlay_PathValue) (outYAML []byte, errs util.Errors) {
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
		scope.Debugf("applying path=%s, value=%s\n", p.Path, p.Value)
		inc, _, err := tpath.GetPathContext(bo, util.PathFromString(p.Path))
		if err != nil {
			errs = util.AppendErr(errs, err)
			continue
		}
		errs = util.AppendErr(errs, tpath.WritePathContext(inc, p.Value))
	}
	oy, err := yaml.Marshal(bo)
	if err != nil {
		return nil, util.AppendErr(errs, err)
	}
	return oy, errs
}

// objectOverrideMap converts oos, a slice of object overlays, into a map of the same overlays where the key is the
// object manifest.Hash.
func objectOverrideMap(oos []*v1alpha2.K8SObjectOverlay, namespace string) map[string][]*v1alpha2.K8SObjectOverlay_PathValue {
	ret := make(map[string][]*v1alpha2.K8SObjectOverlay_PathValue)
	for _, o := range oos {
		ret[object.Hash(o.Kind, namespace, o.Name)] = o.Patches
	}
	return ret
}
