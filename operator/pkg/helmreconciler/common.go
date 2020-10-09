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
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/strategicpatch"
	"k8s.io/client-go/kubernetes/scheme"

	"istio.io/istio/operator/pkg/name"
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
	var result []byte
	currentByte, err := current.MarshalJSON()
	if err != nil {
		return err
	}
	overlayByte, err := overlay.MarshalJSON()
	if err != nil {
		return err
	}

	// All the fields set by the cluster automatically is saved in the service resources.
	// For all service resources, we leverage 2-way merging to keep the fields set by the cluster.
	if strings.EqualFold(current.GetKind(), "service") {
		var schema strategicpatch.LookupPatchMeta
		originalByte := getLastAppliedConfig(current)
		patchByte, schema, err := createPatchSchema(originalByte, overlayByte, overlay)
		if err != nil {
			return err
		}
		result, err = strategicpatch.StrategicMergePatchUsingLookupPatchMeta(currentByte, patchByte, schema)
		if err != nil {
			return err
		}

	} else {
		result, err = jsonpatch.MergePatch(currentByte, overlayByte)
		if err != nil {
			return err
		}
	}
	return current.UnmarshalJSON(result)
}

// createPatchSchema creates the patch based on the current & overlay bytes and the schema for the target
// unstructured.Unstructured
func createPatchSchema(originalByte, overlayByte []byte, mod *unstructured.Unstructured) (patchByte []byte,
	schema strategicpatch.LookupPatchMeta, err error) {
	var obj runtime.Object
	if obj, err = scheme.Scheme.New(mod.GroupVersionKind()); err != nil {
		return
	}
	if schema, err = strategicpatch.NewPatchMetaFromStruct(obj); err != nil {
		return
	}
	patchByte, err = strategicpatch.CreateTwoWayMergePatch(originalByte, overlayByte, obj)
	return
}

// getLastAppliedConfig returns the byte array of the content in `kubectl.kubernetes.io/last-applied-1·configuration`
func getLastAppliedConfig(obj *unstructured.Unstructured) []byte {
	annotations := obj.GetAnnotations()
	if annotations == nil {
		return nil
	}
	return []byte(annotations[v1.LastAppliedConfigAnnotation])
}
