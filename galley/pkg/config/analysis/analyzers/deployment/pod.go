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

package deployment

import (
	v1 "k8s.io/api/core/v1"

	"istio.io/istio/galley/pkg/config/analysis"
	"istio.io/istio/galley/pkg/config/analysis/analyzers/util"
	"istio.io/istio/galley/pkg/config/analysis/msg"
	"istio.io/istio/pkg/config/resource"
	"istio.io/istio/pkg/config/schema/collection"
	"istio.io/istio/pkg/config/schema/collections"
)

type ApplicationUIDAnalyzer struct{}

var _ analysis.Analyzer = &ApplicationUIDAnalyzer{}

func (appUID *ApplicationUIDAnalyzer) Metadata() analysis.Metadata {
	return analysis.Metadata{
		Name:        "applicationUID.Analyzer",
		Description: "Checks invalid application UID",
		Inputs: collection.Names{
			collections.K8SCoreV1Pods.Name(),
		},
	}
}

func (appUID *ApplicationUIDAnalyzer) Analyze(context analysis.Context) {
	context.ForEach(collections.K8SCoreV1Pods.Name(), func(resource *resource.Instance) bool {
		appUID.analyzeApplicationUID(resource, context)
		return true
	})
}

func (appUID *ApplicationUIDAnalyzer) analyzeApplicationUID(resource *resource.Instance, context analysis.Context) {
	p := resource.Message.(*v1.Pod)
	UID := int64(1337)
	message := msg.NewInvalidApplicationUID(resource)

	// Ref: https://istio.io/latest/docs/ops/deployment/requirements/#pod-requirements
	if p.Spec.SecurityContext != nil && p.Spec.SecurityContext.RunAsUser != nil {
		if *p.Spec.SecurityContext.RunAsUser == UID {
			context.Report(collections.K8SCoreV1Pods.Name(), message)
		}
	}
	for _, container := range p.Spec.Containers {
		if container.Name != util.IstioProxyName {
			if container.SecurityContext != nil && container.SecurityContext.RunAsUser != nil {
				if *container.SecurityContext.RunAsUser == UID {
					context.Report(collections.K8SCoreV1Pods.Name(), message)
				}
			}
		}
	}
}
