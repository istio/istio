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

package annotations

import (
	"strings"

	"istio.io/api/annotation"

	"istio.io/istio/galley/pkg/config/analysis"
	"istio.io/istio/galley/pkg/config/analysis/msg"
	"istio.io/istio/galley/pkg/config/meta/metadata"
	"istio.io/istio/galley/pkg/config/meta/schema/collection"
	"istio.io/istio/galley/pkg/config/resource"
)

// K8sAnalyzer checks for misplayed and invalid Istio annotations in K8s resources
type K8sAnalyzer struct{}

var (
	istioAnnotations = annotation.AllResourceAnnotations()
)

// Metadata implements analyzer.Analyzer
func (*K8sAnalyzer) Metadata() analysis.Metadata {
	return analysis.Metadata{
		Name: "annotations.K8sAnalyzer",
		Inputs: collection.Names{
			metadata.K8SCoreV1Endpoints,
			metadata.K8SCoreV1Namespaces,
			metadata.K8SCoreV1Services,
			metadata.K8SCoreV1Pods,
			metadata.K8SExtensionsV1Beta1Ingresses,
			metadata.K8SCoreV1Nodes,
			metadata.K8SAppsV1Deployments,
		},
	}
}

// Analyze implements analysis.Analyzer
func (fa *K8sAnalyzer) Analyze(ctx analysis.Context) {
	ctx.ForEach(metadata.K8SCoreV1Endpoints, func(r *resource.Entry) bool {
		fa.allowAnnotations(r, ctx, "Endpoints", metadata.K8SCoreV1Endpoints)
		return true
	})
	ctx.ForEach(metadata.K8SCoreV1Namespaces, func(r *resource.Entry) bool {
		fa.allowAnnotations(r, ctx, "Namespace", metadata.K8SCoreV1Namespaces)
		return true
	})
	ctx.ForEach(metadata.K8SCoreV1Nodes, func(r *resource.Entry) bool {
		fa.allowAnnotations(r, ctx, "Node", metadata.K8SCoreV1Nodes)
		return true
	})
	ctx.ForEach(metadata.K8SCoreV1Pods, func(r *resource.Entry) bool {
		fa.allowAnnotations(r, ctx, "Pod", metadata.K8SCoreV1Pods)
		return true
	})
	ctx.ForEach(metadata.K8SCoreV1Services, func(r *resource.Entry) bool {
		fa.allowAnnotations(r, ctx, "Service", metadata.K8SCoreV1Services)
		return true
	})
	ctx.ForEach(metadata.K8SExtensionsV1Beta1Ingresses, func(r *resource.Entry) bool {
		fa.allowAnnotations(r, ctx, "Ingress", metadata.K8SExtensionsV1Beta1Ingresses)
		return true
	})
	ctx.ForEach(metadata.K8SAppsV1Deployments, func(r *resource.Entry) bool {
		fa.allowAnnotations(r, ctx, "Deployment", metadata.K8SAppsV1Deployments)
		return true
	})
}

func (*K8sAnalyzer) allowAnnotations(r *resource.Entry, ctx analysis.Context, kind string, collectionType collection.Name) {
	if len(r.Metadata.Annotations) == 0 {
		return
	}

	// It is fine if the annotation is kubectl.kubernetes.io/last-applied-configuration.
outer:
	for ann := range r.Metadata.Annotations {
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

		// Note: currently we don't validate the value of the annotation,
		// just key and placement.
		// There is annotation value validation (for pod annotations) in the web hook at
		// istio.io/istio/pkg/kube/inject/inject.go.  Rather than refactoring
		// that code I intend to bring all of the web hook checks into this framework
		// and checking here will be redundant.
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
