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

package model

import (
	"fmt"

	"google.golang.org/protobuf/proto"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"

	meshapi "istio.io/api/mesh/v1alpha1"
)

// ServiceVisibility is the visibility resolved for a ServiceEntry from
// MeshConfig.serviceEntryVisibility. It is the shared, dataplane-neutral result: the ambient path
// maps it onto the WDS Service.Visibility field, and the sidecar path maps it onto exportTo scoping.
// The zero value is Public so that services without a resolved visibility behave as legacy (public).
type ServiceVisibility int

const (
	// ServiceVisibilityPublic is the legacy default: the service is visible per its exportTo /
	// control-plane connectivity, unrestricted by this feature.
	ServiceVisibilityPublic ServiceVisibility = iota
	// ServiceVisibilityNamespace restricts the service to its own namespace.
	ServiceVisibilityNamespace
	// ServiceVisibilityNone hides the service entirely (published to no one).
	ServiceVisibilityNone
)

func (v ServiceVisibility) String() string {
	switch v {
	case ServiceVisibilityNamespace:
		return "Namespace"
	case ServiceVisibilityNone:
		return "None"
	default:
		return "Public"
	}
}

// ServiceEntryVisibilityMatcher is the precompiled, dependency-narrowed projection of
// MeshConfig.serviceEntryVisibility. Compile it once per serviceEntryVisibility change (label
// selectors are converted only once) and reuse it to resolve visibility per ServiceEntry. It is
// shared by the ambient and sidecar (serviceentry) registries so the policy is evaluated
// identically for both dataplanes.
type ServiceEntryVisibilityMatcher struct {
	// defaultVisibility is applied when no policy matches.
	defaultVisibility ServiceVisibility
	policies          []visibilityPolicy
	// source is retained only for equality, so a recompiled-but-identical value is not treated as a
	// change by krt.
	source *meshapi.ServiceEntryVisibility
}

type visibilityPolicy struct {
	visibility ServiceVisibility
	rules      []visibilityRule
}

type visibilityRule struct {
	// namespaceSelector is set for a namespace_selector matcher. A nil selector never matches
	// (unknown/unset matcher), so a malformed rule fails closed.
	namespaceSelector labels.Selector
}

// ResourceName lets the matcher be used as a krt singleton value.
func (m ServiceEntryVisibilityMatcher) ResourceName() string { return "ServiceEntryVisibility" }

// Equals dedupes on the underlying config so an unrelated MeshConfig change that recompiles an
// identical matcher is not seen as a change.
func (m ServiceEntryVisibilityMatcher) Equals(other ServiceEntryVisibilityMatcher) bool {
	return proto.Equal(m.source, other.source)
}

// VisibilityFor resolves the visibility for a ServiceEntry given its namespace labels: the first
// policy whose rules all match wins, otherwise the default. A nil receiver (visibility not
// configured/computed) is Public (legacy behavior).
func (m *ServiceEntryVisibilityMatcher) VisibilityFor(nsLabels map[string]string) ServiceVisibility {
	if m == nil {
		return ServiceVisibilityPublic
	}
	set := labels.Set(nsLabels)
	for _, p := range m.policies {
		if p.matches(set) {
			return p.visibility
		}
	}
	return m.defaultVisibility
}

// matches reports whether all of a policy's rules match (AND semantics). An empty rule list matches
// everything, making the policy a catch-all.
func (p visibilityPolicy) matches(nsLabels labels.Set) bool {
	for _, r := range p.rules {
		if r.namespaceSelector == nil || !r.namespaceSelector.Matches(nsLabels) {
			return false
		}
	}
	return true
}

// CompileServiceEntryVisibility precompiles a serviceEntryVisibility config. A nil config is legacy
// behavior: everything is Public.
func CompileServiceEntryVisibility(sev *meshapi.ServiceEntryVisibility) *ServiceEntryVisibilityMatcher {
	if sev == nil {
		return &ServiceEntryVisibilityMatcher{defaultVisibility: ServiceVisibilityPublic}
	}
	out := &ServiceEntryVisibilityMatcher{
		defaultVisibility: toServiceVisibility(sev.GetDefaultVisibility()),
		source:            sev,
	}
	for _, policy := range sev.GetPolicies() {
		vp := visibilityPolicy{visibility: toServiceVisibility(policy.GetVisibility())}
		for _, rule := range policy.GetMatchingRules() {
			vp.rules = append(vp.rules, compileVisibilityRule(rule))
		}
		out.policies = append(out.policies, vp)
	}
	return out
}

// compileVisibilityRule converts a single matcher into its precompiled form. Only namespaceSelector
// is implemented today; future matchers (hostnameSuffix, addressInCidr) become new cases here.
func compileVisibilityRule(rule *meshapi.ServiceEntryVisibility_MatchRule) visibilityRule {
	switch rule.GetMatcher().(type) {
	case *meshapi.ServiceEntryVisibility_MatchRule_NamespaceSelector:
		sel, err := labelSelectorAsSelector(rule.GetNamespaceSelector())
		if err != nil {
			log.Warnf("failed to convert serviceEntryVisibility namespace selector: %v", err)
			return visibilityRule{}
		}
		return visibilityRule{namespaceSelector: sel}
	default:
		// Unknown or unset matcher never matches.
		return visibilityRule{}
	}
}

// toServiceVisibility maps a MeshConfig visibility to the neutral ServiceVisibility. UNSPECIFIED is
// Public (legacy default).
func toServiceVisibility(v meshapi.ServiceEntryVisibility_Visibility) ServiceVisibility {
	switch v {
	case meshapi.ServiceEntryVisibility_NAMESPACE:
		return ServiceVisibilityNamespace
	case meshapi.ServiceEntryVisibility_NONE:
		return ServiceVisibilityNone
	default:
		return ServiceVisibilityPublic
	}
}

// labelSelectorAsSelector converts a mesh API LabelSelector to a labels.Selector. A nil selector
// matches nothing; an empty selector matches everything.
//
// NOTE: this is a copy of the identical LabelSelectorAsSelector in pkg/kube/namespace (and
// pilot/pkg/serviceregistry/ambient). It is copied, not imported, because pkg/kube/namespace pulls
// in heavy kube-client machinery (kube/kclient/controllers) that the central model package must not
// depend on. Importing ambient would be a cycle.
// TODO: targeted DRY — lift this pure converter into a lean leaf package (e.g.
// pkg/config/mesh, already imported by pkg/kube/namespace and cycle-free for model/ambient) so the
// three call sites share one copy.
func labelSelectorAsSelector(ps *meshapi.LabelSelector) (labels.Selector, error) {
	if ps == nil {
		return labels.Nothing(), nil
	}
	if len(ps.MatchLabels)+len(ps.MatchExpressions) == 0 {
		return labels.Everything(), nil
	}
	requirements := make([]labels.Requirement, 0, len(ps.MatchLabels)+len(ps.MatchExpressions))
	for k, v := range ps.MatchLabels {
		r, err := labels.NewRequirement(k, selection.Equals, []string{v})
		if err != nil {
			return nil, err
		}
		requirements = append(requirements, *r)
	}
	for _, expr := range ps.MatchExpressions {
		var op selection.Operator
		switch metav1.LabelSelectorOperator(expr.Operator) {
		case metav1.LabelSelectorOpIn:
			op = selection.In
		case metav1.LabelSelectorOpNotIn:
			op = selection.NotIn
		case metav1.LabelSelectorOpExists:
			op = selection.Exists
		case metav1.LabelSelectorOpDoesNotExist:
			op = selection.DoesNotExist
		default:
			return nil, fmt.Errorf("%q is not a valid label selector operator", expr.Operator)
		}
		r, err := labels.NewRequirement(expr.Key, op, append([]string(nil), expr.Values...))
		if err != nil {
			return nil, err
		}
		requirements = append(requirements, *r)
	}
	return labels.NewSelector().Add(requirements...), nil
}
