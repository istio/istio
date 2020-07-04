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

package injection

import (
	"strings"

	v1 "k8s.io/api/core/v1"

	"istio.io/api/label"

	"istio.io/api/annotation"

	"istio.io/istio/galley/pkg/config/analysis"
	"istio.io/istio/galley/pkg/config/analysis/analyzers/util"
	"istio.io/istio/galley/pkg/config/analysis/msg"
	"istio.io/istio/pkg/config/resource"
	"istio.io/istio/pkg/config/schema/collection"
	"istio.io/istio/pkg/config/schema/collections"
)

// Analyzer checks conditions related to Istio sidecar injection.
type Analyzer struct{}

var _ analysis.Analyzer = &Analyzer{}

// We assume that enablement is via an istio-injection=enabled or istio.io/rev namespace label
// In theory, there can be alternatives using Mutatingwebhookconfiguration, but they're very uncommon
// See https://istio.io/docs/ops/troubleshooting/injection/ for more info.
const (
	RevisionInjectionLabelName = label.IstioRev
)

// Metadata implements Analyzer
func (a *Analyzer) Metadata() analysis.Metadata {
	return analysis.Metadata{
		Name:        "injection.Analyzer",
		Description: "Checks conditions related to Istio sidecar injection",
		Inputs: collection.Names{
			collections.K8SCoreV1Namespaces.Name(),
			collections.K8SCoreV1Pods.Name(),
		},
	}
}

// Analyze implements Analyzer
func (a *Analyzer) Analyze(c analysis.Context) {
	injectedNamespaces := make(map[string]bool)

	c.ForEach(collections.K8SCoreV1Namespaces.Name(), func(r *resource.Instance) bool {

		ns := r.Metadata.FullName.String()
		if util.IsSystemNamespace(resource.Namespace(ns)) {
			return true
		}

		injectionLabel := r.Metadata.Labels[util.InjectionLabelName]
		_, okNewInjectionLabel := r.Metadata.Labels[RevisionInjectionLabelName]

		if injectionLabel == "" && !okNewInjectionLabel {
			// TODO: if Istio is installed with sidecarInjectorWebhook.enableNamespacesByDefault=true
			// (in the istio-sidecar-injector configmap), we need to reverse this logic and treat this as an injected namespace

			c.Report(collections.K8SCoreV1Namespaces.Name(), msg.NewNamespaceNotInjected(r, r.Metadata.FullName.String(), r.Metadata.FullName.String()))
			return true
		}

		if okNewInjectionLabel {
			if injectionLabel != "" {
				c.Report(collections.K8SCoreV1Namespaces.Name(),
					msg.NewNamespaceMultipleInjectionLabels(r,
						r.Metadata.FullName.String(),
						r.Metadata.FullName.String()))
				return true
			}
		} else if injectionLabel != util.InjectionLabelEnableValue {
			// If legacy label has any value other than the enablement value, they are deliberately not injecting it, so ignore
			return true
		}

		injectedNamespaces[r.Metadata.FullName.String()] = true

		return true
	})

	c.ForEach(collections.K8SCoreV1Pods.Name(), func(r *resource.Instance) bool {
		pod := r.Message.(*v1.Pod)

		if !injectedNamespaces[pod.GetNamespace()] {
			return true
		}

		// If a pod has injection explicitly disabled, no need to check further
		if val := pod.GetAnnotations()[annotation.SidecarInject.Name]; strings.EqualFold(val, "false") {
			return true
		}

		proxyImage := ""
		for _, container := range pod.Spec.Containers {
			if container.Name == util.IstioProxyName {
				proxyImage = container.Image
				break
			}
		}

		if proxyImage == "" {
			c.Report(collections.K8SCoreV1Pods.Name(), msg.NewPodMissingProxy(r))
		}

		return true
	})
}
