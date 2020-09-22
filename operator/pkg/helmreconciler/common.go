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

package helmreconciler

import (
	"io"
	"strings"

	jsonpatch "github.com/evanphx/json-patch"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"

	"istio.io/istio/operator/pkg/name"
	"istio.io/istio/operator/pkg/util"
	"istio.io/pkg/log"
)

const (
	// MetadataNamespace is the namespace for mesh metadata (labels, annotations)
	MetadataNamespace = "install.operator.istio.io"
	// OwningResourceName represents the name of the owner to which the resource relates
	OwningResourceName = MetadataNamespace + "/owning-resource"
	// OwningResourceNamespace represents the namespace of the owner to which the resource relates
	OwningResourceNamespace = MetadataNamespace + "/owning-resource-namespace"
	// operatorLabelStr indicates Istio operator is managing this resource.
	operatorLabelStr = name.OperatorAPINamespace + "/managed"
	// operatorReconcileStr indicates that the operator will reconcile the resource.
	operatorReconcileStr = "Reconcile"
	// IstioComponentLabelStr indicates which Istio component a resource belongs to.
	IstioComponentLabelStr = name.OperatorAPINamespace + "/component"
	// istioVersionLabelStr indicates the Istio version of the installation.
	istioVersionLabelStr = name.OperatorAPINamespace + "/version"
)

var (
	// TestMode sets the controller into test mode. Used for unit tests to bypass things like waiting on resources.
	TestMode = false

	scope = log.RegisterScope("installer", "installer", 0)
)

func init() {
	// Tree representation and wait channels are an inversion of ComponentDependencies and are constructed from it.
	buildInstallTree()
}

// ComponentTree represents a tree of component dependencies.
type ComponentTree map[name.ComponentName]interface{}
type componentNameToListMap map[name.ComponentName][]name.ComponentName

var (
	// ComponentDependencies is a tree of component dependencies. The semantics are ComponentDependencies[cname] gives
	// the subtree of components that must wait for cname to be installed before starting installation themselves.
	ComponentDependencies = componentNameToListMap{
		name.PilotComponentName: {
			name.PolicyComponentName,
			name.TelemetryComponentName,
			name.CNIComponentName,
			name.IngressComponentName,
			name.EgressComponentName,
			name.AddonComponentName,
		},
		name.IstioBaseComponentName: {
			name.PilotComponentName,
		},
	}

	// InstallTree is a top down hierarchy tree of dependencies where children must wait for the parent to complete
	// before starting installation.
	InstallTree = make(ComponentTree)
)

// buildInstallTree builds a tree from buildInstallTree where parents are the root of each subtree.
func buildInstallTree() {
	// Starting with root, recursively insert each first level child into each node.
	insertChildrenRecursive(name.IstioBaseComponentName, InstallTree, ComponentDependencies)
}

func insertChildrenRecursive(componentName name.ComponentName, tree ComponentTree, children componentNameToListMap) {
	tree[componentName] = make(ComponentTree)
	for _, child := range children[componentName] {
		insertChildrenRecursive(child, tree[componentName].(ComponentTree), children)
	}
}

// InstallTreeString returns a string representation of the dependency tree.
func InstallTreeString() string {
	var sb strings.Builder
	buildInstallTreeString(name.IstioBaseComponentName, "", &sb)
	return sb.String()
}

func buildInstallTreeString(componentName name.ComponentName, prefix string, sb io.StringWriter) {
	_, _ = sb.WriteString(prefix + string(componentName) + "\n")
	if _, ok := InstallTree[componentName].(ComponentTree); !ok {
		return
	}
	for k := range InstallTree[componentName].(ComponentTree) {
		buildInstallTreeString(k, prefix+"  ", sb)
	}
}

// applyOverlay applies an overlay using JSON patch strategy over the current Object in place.
func applyOverlay(current, overlay *unstructured.Unstructured) error {
	cj, err := runtime.Encode(unstructured.UnstructuredJSONScheme, current)
	if err != nil {
		return err
	}
	uj, err := runtime.Encode(unstructured.UnstructuredJSONScheme, overlay)
	if err != nil {
		return err
	}
	merged, err := jsonpatch.MergePatch(cj, uj)
	if err != nil {
		return err
	}
	// save any immutable values
	clusterIP := getPath(current, name.ServiceStr, util.PathFromString("spec.clusterIP"))

	if err = runtime.DecodeInto(unstructured.UnstructuredJSONScheme, merged, current); err != nil {
		return err
	}
	uc := current.UnstructuredContent()
	uo := overlay.UnstructuredContent()
	writeMap(uc, uo, util.PathFromString("spec"))
	writeMap(uc, uo, util.PathFromString("metadata.labels"))

	// restore any immutable values
	writePath(current, name.ServiceStr, util.PathFromString("spec.clusterIP"), clusterIP)

	return nil

}

// writeMap replaces base with overlay at the given path. If the paths hit any nil non-leaf node, nothing is modified.
func writeMap(base, overlay map[string]interface{}, path util.Path) {
	for ; len(path) > 1; path = path[1:] {
		pe := path[0]
		if base[pe] == nil || overlay[pe] == nil {
			return
		}
		bn, ok := base[pe].(map[string]interface{})
		if !ok {
			scope.Error("Unexpected type %T at path %s", base[pe], path)
		}
		on, ok := overlay[pe].(map[string]interface{})
		if !ok {
			scope.Error("Unexpected type %T at path %s", overlay[pe], path)
		}
		base, overlay = bn, on
	}
	pe := path[0]
	base[pe] = overlay[pe]
}

// getPath returns the value at path, if it exists and the objects is of the given kind, otherwise returns nil.
func getPath(obj *unstructured.Unstructured, kind string, path util.Path) interface{} {
	if len(path) < 1 {
		return nil
	}
	if obj.GroupVersionKind().Kind != kind {
		return nil
	}
	uo := obj.UnstructuredContent()
	for ; len(path) > 1; path = path[1:] {
		pe := path[0]
		if uo[pe] == nil {
			return nil
		}
		uo = uo[pe].(map[string]interface{})
	}
	return uo[path[0]]
}

// writePath writes the value at path, if the path exists and the objects is of the given kind, otherwise does nothing.
func writePath(obj *unstructured.Unstructured, kind string, path util.Path, value interface{}) {
	if len(path) < 1 {
		return
	}
	if obj.GroupVersionKind().Kind != kind {
		return
	}
	uo := obj.UnstructuredContent()
	for ; len(path) > 1; path = path[1:] {
		pe := path[0]
		if uo[pe] == nil {
			return
		}
		uo = uo[pe].(map[string]interface{})
	}
	uo[path[0]] = value
}
