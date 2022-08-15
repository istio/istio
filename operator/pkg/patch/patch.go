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
Package patch implements a simple patching mechanism for k8s resources.
Paths are specified in the form a.b.c.[key:value].d.[list_entry_value], where:
  - [key:value] selects a list entry in list c which contains an entry with key:value
  - [list_entry_value] selects a list entry in list d which is a regex match of list_entry_value.

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

# MODIFY

1. set v1 to v1new

	path: a.b.[name:n1].value
	value: v1new

2. set vv1 to vv3

	// Note the lack of quotes around vv1 (see NOTES below).
	path: a.b.[name:n2].list.[vv1]
	value: vv3

3. set vv2=foo to vv2=bar (using regex match)

	path: a.b.[name:n2].list.[vv2]
	value: vv2=bar

4. replace a port whose port was 15010

  - path: spec.ports.[port:15010]
    value:
    port: 15020
    name: grpc-xds
    protocol: TCP

# DELETE

1. Delete container with name: n1

	path: a.b.[name:n1]

2. Delete list value vv1

	path: a.b.[name:n2].list.[vv1]

# ADD

1. Add vv3 to list

	path: a.b.[name:n2].list.[1000]
	value: vv3

Note: the value 1000 is an example. That value used in the patch should
be a value greater than number of the items in the list. Choose 1000 is
just an example which normally is greater than the most of the lists used.

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
	"strings"

	yaml2 "gopkg.in/yaml.v2"

	"istio.io/api/operator/v1alpha1"
	"istio.io/istio/operator/pkg/helm"
	"istio.io/istio/operator/pkg/metrics"
	"istio.io/istio/operator/pkg/object"
	"istio.io/istio/operator/pkg/tpath"
	"istio.io/istio/operator/pkg/util"
	"istio.io/pkg/log"
)

var scope = log.RegisterScope("patch", "patch", 0)

// overlayMatches reports whether obj matches the overlay for either the default namespace or no namespace (cluster scope).
func overlayMatches(overlay *v1alpha1.K8SObjectOverlay, obj *object.K8sObject, defaultNamespace string) bool {
	oh := obj.Hash()
	if oh == object.Hash(overlay.Kind, defaultNamespace, overlay.Name) ||
		oh == object.Hash(overlay.Kind, "", overlay.Name) {
		return true
	}
	return false
}

// YAMLManifestPatch patches a base YAML in the given namespace with a list of overlays.
// Each overlay has the format described in the K8SObjectOverlay definition.
// It returns the patched manifest YAML.
func YAMLManifestPatch(baseYAML string, defaultNamespace string, overlays []*v1alpha1.K8SObjectOverlay) (string, error) {
	var ret strings.Builder
	var errs util.Errors
	objs, err := object.ParseK8sObjectsFromYAMLManifest(baseYAML)
	if err != nil {
		return "", err
	}

	matches := make(map[*v1alpha1.K8SObjectOverlay]object.K8sObjects)
	// Try to apply the defined overlays.
	for _, obj := range objs {
		oy, err := obj.YAML()
		if err != nil {
			errs = util.AppendErr(errs, fmt.Errorf("object to YAML error (%s) for base object: \n%s", err, obj.YAMLDebugString()))
			continue
		}
		oys := string(oy)
		for _, overlay := range overlays {
			if overlayMatches(overlay, obj, defaultNamespace) {
				matches[overlay] = append(matches[overlay], obj)
				var errs2 util.Errors
				oys, errs2 = applyPatches(obj, overlay.Patches)
				errs = util.AppendErrs(errs, errs2)
			}
		}
		if _, err := ret.WriteString(oys + helm.YAMLSeparator); err != nil {
			errs = util.AppendErr(errs, fmt.Errorf("writeString: %s", err))
		}
	}

	for _, overlay := range overlays {
		// Each overlay should have exactly one match in the output manifest.
		switch {
		case len(matches[overlay]) == 0:
			errs = util.AppendErr(errs, fmt.Errorf("overlay for %s:%s does not match any object in output manifest. Available objects are:\n%s",
				overlay.Kind, overlay.Name, strings.Join(objs.Keys(), "\n")))
		case len(matches[overlay]) > 1:
			errs = util.AppendErr(errs, fmt.Errorf("overlay for %s:%s matches multiple objects in output manifest:\n%s",
				overlay.Kind, overlay.Name, strings.Join(objs.Keys(), "\n")))
		}
	}

	return ret.String(), errs.ToError()
}

// applyPatches applies the given patches against the given object. It returns the resulting patched YAML if successful,
// or a list of errors otherwise.
func applyPatches(base *object.K8sObject, patches []*v1alpha1.K8SObjectOverlay_PathValue) (outYAML string, errs util.Errors) {
	bo := make(map[any]any)
	by, err := base.YAML()
	if err != nil {
		return "", util.NewErrs(err)
	}
	// Use yaml2 specifically to allow interface{} as key which WritePathContext treats specially
	err = yaml2.Unmarshal(by, &bo)
	if err != nil {
		return "", util.NewErrs(err)
	}
	for _, p := range patches {
		v := p.Value.AsInterface()
		if strings.TrimSpace(p.Path) == "" {
			scope.Warnf("value=%s has empty path, skip\n", v)
			continue
		}
		scope.Debugf("applying path=%s, value=%s\n", p.Path, v)
		inc, _, err := tpath.GetPathContext(bo, util.PathFromString(p.Path), true)
		if err != nil {
			errs = util.AppendErr(errs, err)
			metrics.ManifestPatchErrorTotal.Increment()
			continue
		}

		err = tpath.WritePathContext(inc, v, false)
		if err != nil {
			errs = util.AppendErr(errs, err)
			metrics.ManifestPatchErrorTotal.Increment()
		}
	}
	oy, err := yaml2.Marshal(bo)
	if err != nil {
		return "", util.AppendErr(errs, err)
	}
	return string(oy), errs
}
