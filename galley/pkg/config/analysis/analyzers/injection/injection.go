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

package injection

import (
	"strings"

	"istio.io/istio/galley/pkg/config/analysis"
	"istio.io/istio/galley/pkg/config/analysis/msg"
	"istio.io/istio/galley/pkg/config/processor/metadata"
	"istio.io/istio/galley/pkg/config/resource"
	v1 "k8s.io/api/core/v1"
)

// Analyzer checks conditions related to Istio injection
type Analyzer struct{}

var _ analysis.Analyzer = &Analyzer{}

// Name implements Analyzer
func (s *Analyzer) Name() string {
	return "injection.Analyzer"
}

// We assume that enablement is via an istio-injection=enabled namespace label
// In theory, there can be alternatives using Mutatingwebhookconfiguration, but they're very uncommon
// See https://istio.io/docs/ops/troubleshooting/injection/ for more info.
const injectionLabelName = "istio-injection"
const injectionLabelEnableValue = "enabled"

// Analyze implements Analyzer
func (s *Analyzer) Analyze(c analysis.Context) {
	injectedNamespaces := make(map[string]bool)

	c.ForEach(metadata.K8SCoreV1Namespaces, func(r *resource.Entry) bool {

		// Ignore system namespaces
		if strings.HasPrefix(r.Metadata.Name.String(), "kube-") || strings.HasPrefix(r.Metadata.Name.String(), "istio-") {
			return true
		}

		injectionLabel := r.Metadata.Labels[injectionLabelName]

		if injectionLabel == "" {
			c.Report(metadata.K8SCoreV1Namespaces, msg.NewNamespaceNotInjected(r, r.Metadata.Name.String(), r.Metadata.Name.String()))
			return true
		}

		// If it has any value other than the enablement value, they are deliberately not injecting it, so ignore
		if r.Metadata.Labels[injectionLabelName] != injectionLabelEnableValue {
			return true
		}

		injectedNamespaces[r.Metadata.Name.String()] = true

		return true
	})

	c.ForEach(metadata.K8SCoreV1Pods, func(r *resource.Entry) bool {
		pod := r.Item.(*v1.Pod)

		if !injectedNamespaces[pod.GetNamespace()] {
			return true
		}

		proxyImage := ""
		for _, container := range pod.Spec.Containers {
			if container.Name == "istio-proxy" {
				proxyImage = container.Image
			}
		}

		if proxyImage == "" {
			c.Report(metadata.K8SCoreV1Pods, msg.NewPodMissingProxy(r, pod.Name, pod.GetNamespace()))
		}

		// TODO: if the pod is injected, check that it's using the right image. This would
		// cover scenarios where Istio is upgraded but pods are not restarted.
		// This is challenging because getting the expected image for the current version
		// of Istio is non-trivial and non-standard across versions

		return true
	})
}
