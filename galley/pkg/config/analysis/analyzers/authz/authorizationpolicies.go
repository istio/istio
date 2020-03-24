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

package authz

import (
	apps_v1 "k8s.io/api/apps/v1"
	k8s_labels "k8s.io/apimachinery/pkg/labels"

	"istio.io/istio/galley/pkg/config/analysis/msg"

	"istio.io/api/security/v1beta1"

	"istio.io/istio/galley/pkg/config/analysis"
	"istio.io/istio/pkg/config/resource"
	"istio.io/istio/pkg/config/schema/collection"
	"istio.io/istio/pkg/config/schema/collections"
)

// AuthorizationPoliciesAnalyzer checks the validity of authorization policies
type AuthorizationPoliciesAnalyzer struct{}

var _ analysis.Analyzer = &AuthorizationPoliciesAnalyzer{}

func (a *AuthorizationPoliciesAnalyzer) Metadata() analysis.Metadata {
	return analysis.Metadata{
		Name:        "auth.AuthorizationPoliciesAnalyzer",
		Description: "Checks the validity of authorization policies",
		Inputs: collection.Names{
			collections.IstioSecurityV1Beta1Authorizationpolicies.Name(),
			collections.K8SAppsV1Deployments.Name(),
		},
	}
}

func (a *AuthorizationPoliciesAnalyzer) Analyze(c analysis.Context) {
	c.ForEach(collections.IstioSecurityV1Beta1Authorizationpolicies.Name(), func(r *resource.Instance) bool {
		a.analyzeAuthPolicy(r, c)
		return true
	})
}

func (a *AuthorizationPoliciesAnalyzer) analyzeAuthPolicy(r *resource.Instance, c analysis.Context) {
	ap := r.Message.(*v1beta1.AuthorizationPolicy)
	ns := r.Metadata.FullName.Namespace
	apSelector := k8s_labels.SelectorFromSet(ap.Selector.MatchLabels)

	hasMatchingPods := false

	c.ForEach(collections.K8SAppsV1Deployments.Name(), func(s *resource.Instance) bool {
		d := s.Message.(*apps_v1.Deployment)
		pLabels := k8s_labels.Set(d.Spec.Template.Labels)

		if d.Namespace == ns.String() && apSelector.Matches(pLabels) {
			hasMatchingPods = true
			return false
		}

		return true
	})

	if !hasMatchingPods {
		c.Report(collections.K8SAppsV1Deployments.Name(), msg.NewNoMatchingWorkloadsFound(r, apSelector.String()))
	}

}
