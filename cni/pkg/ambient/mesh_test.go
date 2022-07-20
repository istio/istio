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

package ambient

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var defaultSelectors = []*metav1.LabelSelector{
	{
		MatchLabels: map[string]string{
			"foo": "bar",
		},
	},
	{
		MatchLabels: map[string]string{
			"bar": "baz",
		},
	},
	{
		MatchExpressions: []metav1.LabelSelectorRequirement{
			{
				Key:      "foo2",
				Operator: metav1.LabelSelectorOpIn,
				Values:   []string{"bar2", "baz2"},
			},
		},
	},
}

func TestNamespaceMatchesDisabledSelector(t *testing.T) {
	cases := []struct {
		name      string
		namespace *corev1.Namespace
		expected  bool
	}{
		{
			name: "no selector",
			namespace: &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: "default",
				},
			},
			expected: false,
		},
		{
			name: "match label selector",
			namespace: &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: "default",
					Labels: map[string]string{
						"foo": "bar",
					},
				},
			},
			expected: true,
		},
		{
			name: "match label selector with other label",
			namespace: &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: "default",
					Labels: map[string]string{
						"bar": "baz",
					},
				},
			},
			expected: true,
		},
		{
			name: "match label with expression",
			namespace: &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: "default",
					Labels: map[string]string{
						"foo2": "bar2",
					},
				},
			},
			expected: true,
		},
		{
			name: "fail match with other labels",
			namespace: &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: "default",
					Labels: map[string]string{
						"hello": "world",
					},
				},
			},
			expected: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := NamespaceMatchesDisabledSelectors(tc.namespace, defaultSelectors)
			if err != nil {
				t.Errorf("unexpected error: %v", err)
			}
			if got != tc.expected {
				t.Errorf("test %s: expected %v, got %v", tc.name, tc.expected, got)
			}
		})
	}
}
