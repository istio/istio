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

	"k8s.io/apimachinery/pkg/types"

	"istio.io/istio/pkg/test/util/assert"
)

func TestConsolidatedDestRuleEquals(t *testing.T) {
	testcases := []struct {
		name     string
		l        *consolidatedDestRule
		r        *consolidatedDestRule
		expected bool
	}{
		{
			name:     "two nil",
			expected: true,
		},
		{
			name: "l is nil",
			l:    nil,
			r: &consolidatedDestRule{
				from: []types.NamespacedName{
					{
						Namespace: "default",
						Name:      "dr1",
					},
				},
			},
			expected: false,
		},
		{
			name: "r is nil",
			l: &consolidatedDestRule{
				from: []types.NamespacedName{
					{
						Namespace: "default",
						Name:      "dr1",
					},
				},
			},
			r:        nil,
			expected: false,
		},
		{
			name: "from length not equal",
			l: &consolidatedDestRule{
				from: []types.NamespacedName{
					{
						Namespace: "default",
						Name:      "dr1",
					},
				},
			},
			r: &consolidatedDestRule{
				from: []types.NamespacedName{
					{
						Namespace: "default",
						Name:      "dr1",
					},
					{
						Namespace: "default",
						Name:      "dr2",
					},
				},
			},
			expected: false,
		},
		{
			name: "from length equals but element is different",
			l: &consolidatedDestRule{
				from: []types.NamespacedName{
					{
						Namespace: "default",
						Name:      "dr1",
					},
					{
						Namespace: "default",
						Name:      "dr2",
					},
				},
			},
			r: &consolidatedDestRule{
				from: []types.NamespacedName{
					{
						Namespace: "default",
						Name:      "dr1",
					},
					{
						Namespace: "default",
						Name:      "dr3",
					},
				},
			},
			expected: false,
		},
		{
			name: "all from elements equal",
			l: &consolidatedDestRule{
				from: []types.NamespacedName{
					{
						Namespace: "default",
						Name:      "dr1",
					},
					{
						Namespace: "default",
						Name:      "dr2",
					},
				},
			},
			r: &consolidatedDestRule{
				from: []types.NamespacedName{
					{
						Namespace: "default",
						Name:      "dr1",
					},
					{
						Namespace: "default",
						Name:      "dr2",
					},
				},
			},
			expected: true,
		},
	}

	for _, tc := range testcases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.l.Equals(tc.r), tc.expected)
		})
	}
}
