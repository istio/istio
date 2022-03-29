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

package webhook

import (
	"fmt"
	"strings"

	v1 "k8s.io/api/admissionregistration/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	klabels "k8s.io/apimachinery/pkg/labels"

	"istio.io/api/label"
	"istio.io/istio/pkg/config/analysis"
	"istio.io/istio/pkg/config/analysis/msg"
	"istio.io/istio/pkg/config/resource"
	"istio.io/istio/pkg/config/schema/collection"
	"istio.io/istio/pkg/config/schema/collections"
	"istio.io/istio/pkg/util/sets"
)

var (
	webhookCol = collections.K8SAdmissionregistrationK8SIoV1Mutatingwebhookconfigurations.Name()
	serviceCol = collections.K8SCoreV1Services.Name()
)

type Analyzer struct {
	SkipServiceCheck bool
}

var _ analysis.Analyzer = &Analyzer{}

func (a *Analyzer) Metadata() analysis.Metadata {
	return analysis.Metadata{
		Name:        "webhook.Analyzer",
		Description: "Checks the validity of Istio webhooks",
		Inputs: collection.Names{
			webhookCol,
			serviceCol,
		},
	}
}

func getNamespaceLabels() []klabels.Set {
	return []klabels.Set{
		{},
		{"istio-injection": "enabled"},
		{"istio-injection": "disabled"},
	}
}

func getObjectLabels() []klabels.Set {
	return []klabels.Set{
		{},
		{"sidecar.istio.io/inject": "true"},
		{"sidecar.istio.io/inject": "false"},
	}
}

func (a *Analyzer) Analyze(context analysis.Context) {
	// First, extract and index all webhooks we found
	webhooks := map[string][]v1.MutatingWebhook{}
	resources := map[string]*resource.Instance{}
	revisions := sets.New()
	context.ForEach(webhookCol, func(resource *resource.Instance) bool {
		wh := resource.Message.(*v1.MutatingWebhookConfiguration)
		revs := extractRevisions(wh)
		if len(revs) == 0 && !isIstioWebhook(wh) {
			return true
		}
		webhooks[resource.Metadata.FullName.String()] = wh.Webhooks
		for _, h := range wh.Webhooks {
			resources[fmt.Sprintf("%v/%v", resource.Metadata.FullName.String(), h.Name)] = resource
		}
		revisions.InsertAll(revs...)
		return true
	})

	// Set up all relevant namespace and object selector permutations
	namespaceLabels := getNamespaceLabels()
	for rev := range revisions {
		for _, base := range getNamespaceLabels() {
			base[label.IoIstioRev.Name] = rev
			namespaceLabels = append(namespaceLabels, base)
		}
	}
	objectLabels := getObjectLabels()
	for rev := range revisions {
		for _, base := range getObjectLabels() {
			base[label.IoIstioRev.Name] = rev
			objectLabels = append(objectLabels, base)
		}
	}

	// For each permutation, we check which webhooks it matches. It must match exactly 0 or 1!
	for _, nl := range namespaceLabels {
		for _, ol := range objectLabels {
			matches := sets.New()
			for name, whs := range webhooks {
				for _, wh := range whs {
					if selectorMatches(wh.NamespaceSelector, nl) && selectorMatches(wh.ObjectSelector, ol) {
						matches.Insert(fmt.Sprintf("%v/%v", name, wh.Name))
					}
				}
			}
			if len(matches) > 1 {
				for match := range matches {
					others := matches.Difference(sets.NewWith(match))
					context.Report(webhookCol, msg.NewInvalidWebhook(resources[match],
						fmt.Sprintf("Webhook overlaps with others: %v. This may cause injection to occur twice.", others.UnsortedList())))
				}
			}
		}
	}

	// Next, check service references
	if a.SkipServiceCheck {
		return
	}
	for name, whs := range webhooks {
		for _, wh := range whs {
			if wh.ClientConfig.Service == nil {
				// it is an url, skip it
				continue
			}
			fname := resource.NewFullName(
				resource.Namespace(wh.ClientConfig.Service.Namespace),
				resource.LocalName(wh.ClientConfig.Service.Name))
			if !context.Exists(serviceCol, fname) {
				context.Report(webhookCol, msg.NewInvalidWebhook(resources[fmt.Sprintf("%v/%v", name, wh.Name)],
					fmt.Sprintf("Injector refers to a control plane service that does not exist: %v.", fname)))
			}
		}
	}
}

func isIstioWebhook(wh *v1.MutatingWebhookConfiguration) bool {
	for _, w := range wh.Webhooks {
		if strings.HasSuffix(w.Name, "istio.io") {
			return true
		}
	}
	return false
}

func extractRevisions(wh *v1.MutatingWebhookConfiguration) []string {
	revs := sets.New()
	if r, f := wh.Labels[label.IoIstioRev.Name]; f {
		revs.Insert(r)
	}
	for _, webhook := range wh.Webhooks {
		if webhook.NamespaceSelector != nil {
			if r, f := webhook.NamespaceSelector.MatchLabels[label.IoIstioRev.Name]; f {
				revs.Insert(r)
			}

			for _, ls := range webhook.NamespaceSelector.MatchExpressions {
				if ls.Key == label.IoIstioRev.Name {
					revs.InsertAll(ls.Values...)
				}
			}
		}
		if webhook.ObjectSelector != nil {
			if r, f := webhook.ObjectSelector.MatchLabels[label.IoIstioRev.Name]; f {
				revs.Insert(r)
			}

			for _, ls := range webhook.ObjectSelector.MatchExpressions {
				if ls.Key == label.IoIstioRev.Name {
					revs.InsertAll(ls.Values...)
				}
			}
		}
	}
	return revs.UnsortedList()
}

func selectorMatches(selector *metav1.LabelSelector, labels klabels.Set) bool {
	// From webhook spec: "Default to the empty LabelSelector, which matches everything."
	if selector == nil {
		return true
	}
	s, err := metav1.LabelSelectorAsSelector(selector)
	if err != nil {
		return false
	}
	return s.Matches(labels)
}
