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

package annotations

import (
	"strings"

	"istio.io/api/annotation"
	"istio.io/api/label"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/analysis"
	"istio.io/istio/pkg/config/analysis/analyzers/maturity"
	"istio.io/istio/pkg/config/analysis/analyzers/util"
	"istio.io/istio/pkg/config/analysis/msg"
	"istio.io/istio/pkg/config/resource"
	"istio.io/istio/pkg/config/schema/gvk"
	"istio.io/istio/pkg/kube/inject"
	"istio.io/istio/pkg/slices"
)

// K8sAnalyzer checks for misplaced and invalid Istio annotations in K8s resources
type K8sAnalyzer struct{}

var istioAnnotations = annotation.AllResourceAnnotations()

// Metadata implements analyzer.Analyzer
func (*K8sAnalyzer) Metadata() analysis.Metadata {
	return analysis.Metadata{
		Name:        "annotations.K8sAnalyzer",
		Description: "Checks for misplaced and invalid Istio annotations in Kubernetes resources",
		Inputs: []config.GroupVersionKind{
			gvk.Namespace,
			gvk.Service,
			gvk.Pod,
			gvk.Deployment,
		},
	}
}

// Analyze implements analysis.Analyzer
func (fa *K8sAnalyzer) Analyze(ctx analysis.Context) {
	ctx.ForEach(gvk.Namespace, func(r *resource.Instance) bool {
		fa.allowAnnotations(r, ctx, "Namespace", gvk.Namespace)
		return true
	})
	ctx.ForEach(gvk.Service, func(r *resource.Instance) bool {
		fa.allowAnnotations(r, ctx, "Service", gvk.Service)
		return true
	})
	ctx.ForEach(gvk.Pod, func(r *resource.Instance) bool {
		fa.allowAnnotations(r, ctx, "Pod", gvk.Pod)
		return true
	})
	ctx.ForEach(gvk.Deployment, func(r *resource.Instance) bool {
		fa.allowAnnotations(r, ctx, "Deployment", gvk.Deployment)
		return true
	})
}

var deprecationExtraMessages = map[string]string{
	annotation.SidecarInject.Name: ` in favor of the "sidecar.istio.io/inject" label`,
}

func (*K8sAnalyzer) allowAnnotations(r *resource.Instance, ctx analysis.Context, kind string, collectionType config.GroupVersionKind) {
	if len(r.Metadata.Annotations) == 0 {
		return
	}

	// It is fine if the annotation is kubectl.kubernetes.io/last-applied-configuration.
outer:
	for ann, value := range r.Metadata.Annotations {
		if !istioAnnotation(ann) {
			continue
		}

		if maturity.AlwaysIgnoredAnnotations[ann] {
			continue
		}

		annotationDef := lookupAnnotation(ann)
		if annotationDef == nil {
			m := msg.NewUnknownAnnotation(r, ann)
			util.AddLineNumber(r, ann, m)

			ctx.Report(collectionType, m)
			continue
		}

		if annotationDef.Deprecated {
			if _, f := r.Metadata.Labels[label.SidecarInject.Name]; f && ann == annotation.SidecarInject.Name {
				// Skip to avoid noise; the user has the deprecated annotation but they also have the replacement
				// This means they are likely aware its deprecated, but are keeping both variants around for maximum
				// compatibility
			} else {
				m := msg.NewDeprecatedAnnotation(r, ann, deprecationExtraMessages[annotationDef.Name])
				util.AddLineNumber(r, ann, m)

				ctx.Report(collectionType, m)
			}
		}

		// If the annotation def attaches to Any, exit early
		for _, rt := range annotationDef.Resources {
			if rt == annotation.Any {
				continue outer
			}
		}

		attachesTo := resourceTypesAsStrings(annotationDef.Resources)
		if !slices.Contains(attachesTo, kind) {
			m := msg.NewMisplacedAnnotation(r, ann, strings.Join(attachesTo, ", "))
			util.AddLineNumber(r, ann, m)

			ctx.Report(collectionType, m)
			continue
		}

		validationFunction := inject.AnnotationValidation[ann]
		if validationFunction != nil {
			if err := validationFunction(value); err != nil {
				m := msg.NewInvalidAnnotation(r, ann, err.Error())
				util.AddLineNumber(r, ann, m)

				ctx.Report(collectionType, m)
				continue
			}
		}
	}
}

// istioAnnotation is true if the annotation is in Istio's namespace
func istioAnnotation(ann string) bool {
	// We document this Kubernetes annotation, we should analyze it as well
	if ann == "kubernetes.io/ingress.class" {
		return true
	}

	parts := strings.Split(ann, "/")
	if len(parts) == 0 {
		return false
	}

	if !strings.HasSuffix(parts[0], "istio.io") {
		return false
	}

	return true
}

func lookupAnnotation(ann string) *annotation.Instance {
	for _, candidate := range istioAnnotations {
		if candidate.Name == ann {
			return candidate
		}
	}

	return nil
}

func resourceTypesAsStrings(resourceTypes []annotation.ResourceTypes) []string {
	retval := []string{}
	for _, resourceType := range resourceTypes {
		if s := resourceType.String(); s != "Unknown" {
			retval = append(retval, s)
		}
	}
	return retval
}
