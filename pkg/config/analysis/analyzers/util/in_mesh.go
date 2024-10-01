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
	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"

	"istio.io/api/annotation"
	"istio.io/api/label"
	"istio.io/istio/pkg/config/analysis"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/config/resource"
	"istio.io/istio/pkg/config/schema/gvk"
	"istio.io/istio/pkg/slices"
)

// DeploymentInMesh returns true if deployment is in the service mesh (has sidecar)
func DeploymentInMesh(r *resource.Instance, c analysis.Context) bool {
	d := r.Message.(*appsv1.DeploymentSpec)
	return inMesh(d.Template.Annotations, d.Template.Labels,
		resource.Namespace(r.Metadata.FullName.Namespace.String()), d.Template.Spec.Containers, c)
}

// PodInMesh returns true if a Pod is in the service mesh (has sidecar)
func PodInMesh(r *resource.Instance, c analysis.Context) bool {
	p := r.Message.(*v1.PodSpec)
	return inMesh(r.Metadata.Annotations, r.Metadata.Labels,
		r.Metadata.FullName.Namespace, append(slices.Clone(p.Containers), p.InitContainers...), c)
}

// PodInAmbientMode returns true if a Pod is in the service mesh with the ambient mode
func PodInAmbientMode(r *resource.Instance) bool {
	if r == nil {
		return false
	}

	return r.Metadata.Annotations[annotation.AmbientRedirection.Name] == constants.AmbientRedirectionEnabled
}

// NamespaceInAmbientMode returns true if a Namespace is configured as a ambient namespace.
func NamespaceInAmbientMode(r *resource.Instance) bool {
	if r == nil {
		return false
	}
	// If there is a sidecar injection label, then we assume the namespace is not in ambient mode
	if r.Metadata.Labels[InjectionLabelName] == InjectionLabelEnableValue {
		return false
	}
	if v, ok := r.Metadata.Labels[label.IoIstioRev.Name]; ok && v != "" {
		return false
	}
	return r.Metadata.Labels[label.IoIstioDataplaneMode.Name] == constants.DataplaneModeAmbient
}

func inMesh(annos, labels map[string]string, namespace resource.Namespace, containers []v1.Container, c analysis.Context) bool {
	// If pod has the sidecar container set, then, the pod is in the mesh
	if hasIstioProxy(containers) {
		return true
	}

	// If Pod has labels, return the injection label value
	if piv, ok := getPodSidecarInjectionStatus(labels); ok {
		return piv
	}

	// If Pod has annotation, return the injection annotation value
	if piv, ok := getPodSidecarInjectionStatus(annos); ok {
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
// enabled is true when deployment d PodSpec has either the label/annotation 'sidecar.istio.io/inject: "true"'
// ok is true when the PodSpec doesn't have the 'sidecar.istio.io/inject' label/annotation present.
func getPodSidecarInjectionStatus(metadata map[string]string) (enabled bool, ok bool) {
	v, ok := metadata[label.SidecarInject.Name]
	return v == "true", ok
}

// autoInjectionEnabled returns two booleans: enabled and ok.
// enabled is true when namespace ns has 'istio-injection' label set to 'enabled'
// ok is true when the namespace doesn't have the label 'istio-injection'
func getNamesSidecarInjectionStatus(ns resource.Namespace, c analysis.Context) (enabled bool, ok bool) {
	enabled, ok = false, false

	namespace := c.Find(gvk.Namespace, resource.NewFullName("", resource.LocalName(ns)))
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
