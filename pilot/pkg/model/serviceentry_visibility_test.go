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
	"testing"

	meshapi "istio.io/api/mesh/v1alpha1"
	"istio.io/istio/pkg/test/util/assert"
)

func nsSelectorRule(sel *meshapi.LabelSelector) *meshapi.ServiceEntryVisibility_MatchRule {
	return &meshapi.ServiceEntryVisibility_MatchRule{
		Matcher: &meshapi.ServiceEntryVisibility_MatchRule_NamespaceSelector{NamespaceSelector: sel},
	}
}

func TestCompileServiceEntryVisibility(t *testing.T) {
	t.Run("nil config is Public and ok", func(t *testing.T) {
		m, ok := CompileServiceEntryVisibility(nil)
		assert.Equal(t, ok, true)
		assert.Equal(t, m.VisibilityFor(nil), ServiceVisibilityPublic)
	})

	t.Run("valid policy compiles and matches", func(t *testing.T) {
		m, ok := CompileServiceEntryVisibility(&meshapi.ServiceEntryVisibility{
			DefaultVisibility: meshapi.ServiceEntryVisibility_NAMESPACE,
			Policies: []*meshapi.ServiceEntryVisibility_Policy{{
				Visibility: meshapi.ServiceEntryVisibility_PUBLIC,
				MatchingRules: []*meshapi.ServiceEntryVisibility_MatchRule{
					nsSelectorRule(&meshapi.LabelSelector{MatchLabels: map[string]string{"se-visibility": "public"}}),
				},
			}},
		})
		assert.Equal(t, ok, true)
		assert.Equal(t, m.VisibilityFor(map[string]string{"se-visibility": "public"}), ServiceVisibilityPublic)
		// No policy matches -> default.
		assert.Equal(t, m.VisibilityFor(map[string]string{"other": "x"}), ServiceVisibilityNamespace)
	})

	t.Run("invalid selector: ok is false, best-effort matcher drops the bad rule", func(t *testing.T) {
		m, ok := CompileServiceEntryVisibility(&meshapi.ServiceEntryVisibility{
			DefaultVisibility: meshapi.ServiceEntryVisibility_NAMESPACE,
			Policies: []*meshapi.ServiceEntryVisibility_Policy{{
				Visibility: meshapi.ServiceEntryVisibility_PUBLIC,
				MatchingRules: []*meshapi.ServiceEntryVisibility_MatchRule{
					// An invalid operator makes the selector fail to compile.
					nsSelectorRule(&meshapi.LabelSelector{MatchExpressions: []*meshapi.LabelSelectorRequirement{{
						Key:      "k",
						Operator: "BadOperator",
					}}}),
				},
			}},
		})
		assert.Equal(t, ok, false)
		// The matcher is still usable; the dropped rule never matches, so everything falls to default.
		assert.Equal(t, m.VisibilityFor(map[string]string{"se-visibility": "public"}), ServiceVisibilityNamespace)
	})
}
