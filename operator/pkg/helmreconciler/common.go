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
	"fmt"
	"io"
	"strconv"
	"strings"

	jsonpatch "github.com/evanphx/json-patch/v5"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/kubectl/pkg/scheme"

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
	// istioOperatorInstallPrefix indicates the name of resource istiooperators.install.istio.io for Istio operator installation.
	istioOperatorsInstallPrefix = "installed-state"
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
type (
	ComponentTree          map[name.ComponentName]any
	componentNameToListMap map[name.ComponentName][]name.ComponentName
)

var (
	// ComponentDependencies is a tree of component dependencies. The semantics are ComponentDependencies[cname] gives
	// the subtree of components that must wait for cname to be installed before starting installation themselves.
	ComponentDependencies = componentNameToListMap{
		name.PilotComponentName: {
			name.CNIComponentName,
			name.IngressComponentName,
			name.EgressComponentName,
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

	overlayUpdated := overlay.DeepCopy()
	if strings.EqualFold(current.GetKind(), "service") {
		if err := saveClusterIP(current, overlayUpdated); err != nil {
			return err
		}

		saveNodePorts(current, overlayUpdated)
	}

	uj, err := runtime.Encode(unstructured.UnstructuredJSONScheme, overlayUpdated)
	if err != nil {
		return err
	}
	merged, err := jsonpatch.MergePatch(cj, uj)
	if err != nil {
		return err
	}
	return runtime.DecodeInto(unstructured.UnstructuredJSONScheme, merged, current)
}

// createPortMap returns a map, mapping the value of the port and value of the nodePort
func createPortMap(current *unstructured.Unstructured) map[string]uint32 {
	portMap := make(map[string]uint32)
	svc := &v1.Service{}
	if err := scheme.Scheme.Convert(current, svc, nil); err != nil {
		log.Error(err.Error())
		return portMap
	}
	for _, p := range svc.Spec.Ports {
		portMap[strconv.Itoa(int(p.Port))] = uint32(p.NodePort)
	}
	return portMap
}

// saveNodePorts transfers the port values from the current cluster into the overlay
func saveNodePorts(current, overlay *unstructured.Unstructured) {
	portMap := createPortMap(current)
	ports, _, _ := unstructured.NestedFieldNoCopy(overlay.Object, "spec", "ports")
	portList, ok := ports.([]any)
	if !ok {
		return
	}
	for _, port := range portList {
		m, ok := port.(map[string]any)
		if !ok {
			continue
		}
		if nodePortNum, ok := m["nodePort"]; ok && fmt.Sprintf("%v", nodePortNum) == "0" {
			if portNum, ok := m["port"]; ok {
				if v, ok := portMap[fmt.Sprintf("%v", portNum)]; ok {
					m["nodePort"] = v
				}
			}
		}
	}
}

// saveClusterIP copies the cluster IP from the current cluster into the overlay
func saveClusterIP(current, overlay *unstructured.Unstructured) error {
	// Save the value of spec.clusterIP set by the cluster
	if clusterIP, found, err := unstructured.NestedString(current.Object, "spec",
		"clusterIP"); err != nil {
		return err
	} else if found {
		if err := unstructured.SetNestedField(overlay.Object, clusterIP, "spec",
			"clusterIP"); err != nil {
			return err
		}
	}
	return nil
}

// getIstioOperatorCRName get the Istio operator crd name based on specified revision
func getIstioOperatorCRName(revision string) string {
	name := istioOperatorsInstallPrefix
	if revision == "" || revision == "default" {
		return name
	}
	return name + "-" + revision
}
