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

	admit_v1 "k8s.io/api/admissionregistration/v1"
	apps_v1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	klabels "k8s.io/apimachinery/pkg/labels"

	"istio.io/istio/pkg/config/analysis"
	"istio.io/istio/pkg/config/analysis/msg"
	"istio.io/istio/pkg/config/resource"
	"istio.io/istio/pkg/config/schema/collection"
	"istio.io/istio/pkg/config/schema/collections"
)

// ImageAutoAnalyzer reports an error if Pods and Deployments with `image: auto` are not going to be injected.
type ImageAutoAnalyzer struct{}

var _ analysis.Analyzer = &ImageAutoAnalyzer{}

const (
	istioProxyContainerName = "istio-proxy"
	manualInjectionImage    = "auto"
)

// Metadata implements Analyzer.
func (a *ImageAutoAnalyzer) Metadata() analysis.Metadata {
	return analysis.Metadata{
		Name:        "injection.ImageAutoAnalyzer",
		Description: "Makes sure that Pods and Deployments with `image: auto` are going to be injected",
		Inputs: collection.Names{
			collections.K8SCoreV1Namespaces.Name(),
			collections.K8SCoreV1Pods.Name(),
			collections.K8SAppsV1Deployments.Name(),
			collections.K8SAdmissionregistrationK8SIoV1Mutatingwebhookconfigurations.Name(),
		},
	}
}

// Analyze implements Analyzer.
func (a *ImageAutoAnalyzer) Analyze(c analysis.Context) {
	var istioWebhooks []admit_v1.MutatingWebhook
	c.ForEach(collections.K8SAdmissionregistrationK8SIoV1Mutatingwebhookconfigurations.Name(), func(resource *resource.Instance) bool {
		mwhc := resource.Message.(*admit_v1.MutatingWebhookConfiguration)
		for _, wh := range mwhc.Webhooks {
			if strings.HasSuffix(wh.Name, "istio.io") {
				istioWebhooks = append(istioWebhooks, wh)
			}
		}
		return true
	})
	c.ForEach(collections.K8SCoreV1Pods.Name(), func(resource *resource.Instance) bool {
		p := resource.Message.(*v1.PodSpec)
		// If a pod has `image: auto` it is broken whether the webhooks match or not
		if !hasAutoImage(p) {
			return true
		}
		m := msg.NewImageAutoWithoutInjectionError(resource, "Pod", resource.Metadata.FullName.Name.String())
		c.Report(collections.K8SCoreV1Pods.Name(), m)
		return true
	})
	c.ForEach(collections.K8SAppsV1Deployments.Name(), func(resource *resource.Instance) bool {
		d := resource.Message.(*apps_v1.DeploymentSpec)
		if !hasAutoImage(&d.Template.Spec) {
			return true
		}
		nsLabels := getNamespaceLabels(c, resource.Metadata.FullName.Namespace.String())
		if !matchesWebhooks(nsLabels, d.Template.Labels, istioWebhooks) {
			m := msg.NewImageAutoWithoutInjectionWarning(resource, "Deployment", resource.Metadata.FullName.Name.String())
			c.Report(collections.K8SAppsV1Deployments.Name(), m)
		}
		return true
	})
}

func hasAutoImage(spec *v1.PodSpec) bool {
	for _, c := range spec.Containers {
		if c.Name == istioProxyContainerName && c.Image == manualInjectionImage {
			return true
		}
	}
	return false
}

func getNamespaceLabels(c analysis.Context, nsName string) map[string]string {
	if nsName == "" {
		nsName = "default"
	}
	ns := c.Find(collections.K8SCoreV1Namespaces.Name(), resource.NewFullName("", resource.LocalName(nsName)))
	if ns == nil {
		return nil
	}
	return ns.Metadata.Labels
}

func matchesWebhooks(nsLabels, podLabels map[string]string, istioWebhooks []admit_v1.MutatingWebhook) bool {
	for _, w := range istioWebhooks {
		if selectorMatches(w.NamespaceSelector, nsLabels) && selectorMatches(w.ObjectSelector, podLabels) {
			return true
		}
	}
	return false
}

func selectorMatches(selector *metav1.LabelSelector, labels klabels.Set) bool {
	// From webhook spec: "Default to the empty LabelSelector, which matchesWebhooks everything."
	if selector == nil {
		return true
	}
	s, err := metav1.LabelSelectorAsSelector(selector)
	if err != nil {
		return false
	}
	return s.Matches(labels)
}
