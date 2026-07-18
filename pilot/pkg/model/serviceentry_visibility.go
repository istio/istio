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
	"istio.io/istio/pkg/config/mesh/labelselector"
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

// Configured reports whether MeshConfig.serviceEntryVisibility was set at all.
func (m *ServiceEntryVisibilityMatcher) Configured() bool {
	return m != nil && m.source != nil
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
// behavior: everything is Public. It always returns a usable best-effort matcher; ok is false if any
// part of the config failed to compile, so callers that own the config can keep the last known good
// matcher instead of applying a partially-compiled one.
func CompileServiceEntryVisibility(sev *meshapi.ServiceEntryVisibility) (*ServiceEntryVisibilityMatcher, bool) {
	if sev == nil {
		return &ServiceEntryVisibilityMatcher{defaultVisibility: ServiceVisibilityPublic}, true
	}
	out := &ServiceEntryVisibilityMatcher{
		defaultVisibility: toServiceVisibility(sev.GetDefaultVisibility()),
		source:            sev,
	}
	ok := true
	for _, policy := range sev.GetPolicies() {
		vp := visibilityPolicy{visibility: toServiceVisibility(policy.GetVisibility())}
		for _, rule := range policy.GetMatchingRules() {
			r, ruleOK := compileVisibilityRule(rule)
			ok = ok && ruleOK
			vp.rules = append(vp.rules, r)
		}
		out.policies = append(out.policies, vp)
	}
	return out, ok
}

// ServiceEntryVisibilityCollection derives the precompiled serviceEntryVisibility matcher as a krt
// singleton from the mesh config, so both dataplanes (ambient and classic serviceentry) compile it
// once and evaluate visibility identically. It recomputes only when the matcher changes (Equals).
func ServiceEntryVisibilityCollection(
	meshConfig krt.Collection[MeshConfig],
	opts krt.OptionsBuilder,
) krt.Singleton[ServiceEntryVisibilityMatcher] {
	return krt.NewSingleton[ServiceEntryVisibilityMatcher](func(ctx krt.HandlerContext) *ServiceEntryVisibilityMatcher {
		var sev *meshapi.ServiceEntryVisibility
		if mc := krt.FetchOne(ctx, meshConfig); mc != nil {
			sev = mc.GetServiceEntryVisibility()
		}
		m, ok := CompileServiceEntryVisibility(sev)
		if !ok {
			// The config failed to compile (logged above). Keep the last known good matcher rather
			// than applying a partially-compiled one; a fresh instance falls back to best-effort.
			ctx.DiscardResult()
		}
		return m
	}, opts.WithName("ServiceEntryVisibility")...)
}

// compileVisibilityRule converts a single matcher into its precompiled form. Only namespaceSelector
// is implemented today; future matchers (hostnameSuffix, addressInCidr) become new cases here. ok is
// false if the matcher failed to compile; an unknown/unset matcher is not a failure (forward compat).
func compileVisibilityRule(rule *meshapi.ServiceEntryVisibility_MatchRule) (visibilityRule, bool) {
	switch rule.GetMatcher().(type) {
	case *meshapi.ServiceEntryVisibility_MatchRule_NamespaceSelector:
		sel, err := labelselector.LabelSelectorAsSelector(rule.GetNamespaceSelector())
		if err != nil {
			log.Errorf("failed to convert serviceEntryVisibility namespace selector: %v", err)
			return visibilityRule{}, false
		}
		return visibilityRule{namespaceSelector: sel}, true
	default:
		// Unknown or unset matcher never matches.
		return visibilityRule{}, true
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
