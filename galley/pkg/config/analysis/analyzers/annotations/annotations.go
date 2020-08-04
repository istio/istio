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

	"istio.io/istio/galley/pkg/config/analysis"
	"istio.io/istio/galley/pkg/config/analysis/msg"
	"istio.io/istio/pkg/config/resource"
	"istio.io/istio/pkg/config/schema/collection"
	"istio.io/istio/pkg/config/schema/collections"
	"istio.io/istio/pkg/kube/inject"
)

// K8sAnalyzer checks for misplaced and invalid Istio annotations in K8s resources
type K8sAnalyzer struct{}

var (
	istioAnnotations = annotation.AllResourceAnnotations()
)

// Metadata implements analyzer.Analyzer
func (*K8sAnalyzer) Metadata() analysis.Metadata {
	return analysis.Metadata{
		Name:        "annotations.K8sAnalyzer",
		Description: "Checks for misplaced and invalid Istio annotations in Kubernetes resources",
		Inputs: collection.Names{
			collections.K8SCoreV1Namespaces.Name(),
			collections.K8SCoreV1Services.Name(),
			collections.K8SCoreV1Pods.Name(),
			collections.K8SAppsV1Deployments.Name(),
		},
	}
}

// Analyze implements analysis.Analyzer
func (fa *K8sAnalyzer) Analyze(ctx analysis.Context) {
	ctx.ForEach(collections.K8SCoreV1Namespaces.Name(), func(r *resource.Instance) bool {
		fa.allowAnnotations(r, ctx, "Namespace", collections.K8SCoreV1Namespaces.Name())
		return true
	})
	ctx.ForEach(collections.K8SCoreV1Services.Name(), func(r *resource.Instance) bool {
		fa.allowAnnotations(r, ctx, "Service", collections.K8SCoreV1Services.Name())
		return true
	})
	ctx.ForEach(collections.K8SCoreV1Pods.Name(), func(r *resource.Instance) bool {
		fa.allowAnnotations(r, ctx, "Pod", collections.K8SCoreV1Pods.Name())
		return true
	})
	ctx.ForEach(collections.K8SAppsV1Deployments.Name(), func(r *resource.Instance) bool {
		fa.allowAnnotations(r, ctx, "Deployment", collections.K8SAppsV1Deployments.Name())
		return true
	})
}

func (*K8sAnalyzer) allowAnnotations(r *resource.Instance, ctx analysis.Context, kind string, collectionType collection.Name) {
	if len(r.Metadata.Annotations) == 0 {
		return
	}

	// It is fine if the annotation is kubectl.kubernetes.io/last-applied-configuration.
outer:
	for ann, value := range r.Metadata.Annotations {
		if !istioAnnotation(ann) {
			continue
		}

		annotationDef := lookupAnnotation(ann)
		if annotationDef == nil {
			ctx.Report(collectionType,
				msg.NewUnknownAnnotation(r, ann))
			continue
		}

		// If the annotation def attaches to Any, exit early
		for _, rt := range annotationDef.Resources {
			if rt == annotation.Any {
				continue outer
			}
		}

		attachesTo := resourceTypesAsStrings(annotationDef.Resources)
		if !contains(attachesTo, kind) {
			ctx.Report(collectionType,
				msg.NewMisplacedAnnotation(r, ann, strings.Join(attachesTo, ", ")))
			continue
		}

		// TODO: Check annotation.Deprecated.  Not implemented because no
		// deprecations in the table have yet been deprecated!
		validationFunction := inject.AnnotationValidation[ann]
		if validationFunction != nil {
			if err := validationFunction(value); err != nil {
				ctx.Report(collectionType,
					msg.NewInvalidAnnotation(r, ann, err.Error()))
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

func contains(candidates []string, s string) bool {
	for _, candidate := range candidates {
		if s == candidate {
			return true
		}
	}

	return false
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
