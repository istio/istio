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

package util

import (
	apps_v1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"

	"istio.io/api/annotation"
	"istio.io/istio/galley/pkg/config/analysis"
	"istio.io/istio/pkg/config/resource"
	"istio.io/istio/pkg/config/schema/collections"
)

// DeploymentinMesh returns true if deployment is in the service mesh (has sidecar)
func DeploymentInMesh(r *resource.Instance, c analysis.Context) bool {
	d := r.Message.(*apps_v1.Deployment)
	return inMesh(d.Spec.Template.Annotations, resource.Namespace(d.Namespace), d.Spec.Template.Spec.Containers, c)
}

// PodInMesh returns true if a Pod is in the service mesh (has sidecar)
func PodInMesh(r *resource.Instance, c analysis.Context) bool {
	p := r.Message.(*v1.Pod)
	return inMesh(p.Annotations, resource.Namespace(p.Namespace), p.Spec.Containers, c)

}

func inMesh(annos map[string]string, namespace resource.Namespace, containers []v1.Container, c analysis.Context) bool {
	// If pod has the sidecar container set, then, the pod is in the mesh
	if hasIstioProxy(containers) {
		return true
	}

	// If Pod has annotation, return the injection annotation value
	if piv, pivok := getPodSidecarInjectionStatus(annos); pivok {
		return piv
	}

	// In case the annotation is not present but there is a auto-injection label on the namespace,
	// return the auto-injection label status
	if niv, nivok := getNamesSidecarInjectionStatus(namespace, c); nivok {
		return niv
	}

	return false
}

// getPodSidecarInjectionStatus returns two booleans: enabled and ok.
// enabled is true when deployment d PodSpec has either the annotation 'sidecar.istio.io/inject: "true"'
// ok is true when the PodSpec doesn't have the 'sidecar.istio.io/inject' annotation present.
func getPodSidecarInjectionStatus(annos map[string]string) (enabled bool, ok bool) {
	v, ok := annos[annotation.SidecarInject.Name]
	return v == "true", ok
}

// autoInjectionEnabled returns two booleans: enabled and ok.
// enabled is true when namespace ns has 'istio-injection' label set to 'enabled'
// ok is true when the namespace doesn't have the label 'istio-injection'
func getNamesSidecarInjectionStatus(ns resource.Namespace, c analysis.Context) (enabled bool, ok bool) {
	enabled, ok = false, false

	namespace := c.Find(collections.K8SCoreV1Namespaces.Name(), resource.NewFullName("", resource.LocalName(ns)))
	if namespace != nil {
		enabled, ok = namespace.Metadata.Labels[InjectionLabelName] == InjectionLabelEnableValue, true
	}

	return enabled, ok
}

func hasIstioProxy(containers []v1.Container) bool {
	proxyImage := ""
	for _, container := range containers {
		if container.Name == IstioProxyName {
			proxyImage = container.Image
			break
		}
	}

	return proxyImage != ""
}
