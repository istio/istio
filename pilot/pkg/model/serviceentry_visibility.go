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
	"google.golang.org/protobuf/proto"
	"k8s.io/apimachinery/pkg/labels"

	meshapi "istio.io/api/mesh/v1alpha1"
	"istio.io/istio/pkg/config/mesh"
	"istio.io/istio/pkg/kube/krt"
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

// ServiceEntryVisibilityCollection derives the precompiled serviceEntryVisibility matcher as a krt
// singleton from the mesh config, so both dataplanes (ambient and classic serviceentry) compile it
// once and evaluate visibility identically. It recomputes only when the matcher changes (Equals).
func ServiceEntryVisibilityCollection(
	meshConfig krt.Collection[MeshConfig],
	opts krt.OptionsBuilder,
) krt.Singleton[ServiceEntryVisibilityMatcher] {
	return krt.NewSingleton[ServiceEntryVisibilityMatcher](func(ctx krt.HandlerContext) *ServiceEntryVisibilityMatcher {
		mc := krt.FetchOne(ctx, meshConfig)
		if mc == nil {
			return CompileServiceEntryVisibility(nil)
		}
		return CompileServiceEntryVisibility(mc.GetServiceEntryVisibility())
	}, opts.WithName("ServiceEntryVisibility")...)
}

// compileVisibilityRule converts a single matcher into its precompiled form. Only namespaceSelector
// is implemented today; future matchers (hostnameSuffix, addressInCidr) become new cases here.
func compileVisibilityRule(rule *meshapi.ServiceEntryVisibility_MatchRule) visibilityRule {
	switch rule.GetMatcher().(type) {
	case *meshapi.ServiceEntryVisibility_MatchRule_NamespaceSelector:
		sel, err := mesh.LabelSelectorAsSelector(rule.GetNamespaceSelector())
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
