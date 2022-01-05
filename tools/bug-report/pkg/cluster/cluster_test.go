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

package cluster

import (
	"testing"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	config2 "istio.io/istio/tools/bug-report/pkg/config"
)

func TestShouldSkip(t *testing.T) {
	cases := []struct {
		name       string
		pod        *v1.Pod
		config     *config2.BugReportConfig
		deployment string
		expected   bool
	}{
		{
			"tested namespace not skip",
			&v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "in-namespace1",
				},
			},
			&config2.BugReportConfig{
				Include: []*config2.SelectionSpec{
					{
						Namespaces: []string{"in-"},
					},
				},
				Exclude: []*config2.SelectionSpec{
					{
						Namespaces: []string{"ex-"},
					},
				},
			},
			"*",
			false,
		},
		{
			"tested namespace not skip 2",
			&v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "in-namespace1",
				},
			},
			&config2.BugReportConfig{
				Include: []*config2.SelectionSpec{
					{
						Namespaces: []string{"in*"},
					},
				},
				Exclude: []*config2.SelectionSpec{
					{
						Namespaces: []string{"ex*"},
					},
				},
			},
			"*",
			false,
		},
		{
			"tested namespace not skip 3",
			&v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "in-namespace1",
				},
			},
			&config2.BugReportConfig{
				Include: []*config2.SelectionSpec{
					{
						Namespaces: []string{"*name*"},
					},
				},
				Exclude: []*config2.SelectionSpec{
					{
						Namespaces: []string{"ex*"},
					},
				},
			},
			"*",
			false,
		},
		{
			"tested namespace not skip 4",
			&v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "in-namespace1",
				},
			},
			&config2.BugReportConfig{
				Include: []*config2.SelectionSpec{
					{
						Namespaces: []string{"*space1"},
					},
				},
				Exclude: []*config2.SelectionSpec{
					{
						Namespaces: []string{"ex*"},
					},
				},
			},
			"*",
			false,
		},
		{
			"tested namespace skip",
			&v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "ex-namespace1",
				},
			},
			&config2.BugReportConfig{
				Include: []*config2.SelectionSpec{
					{
						Namespaces: []string{"in-"},
					},
				},
				Exclude: []*config2.SelectionSpec{
					{
						Namespaces: []string{"ex-"},
					},
				},
			},
			"*",
			true,
		},
		{
			"tested pod name not skip",
			&v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name: "in-pod1",
				},
			},
			&config2.BugReportConfig{
				Include: []*config2.SelectionSpec{
					{
						Pods: []string{"in-"},
					},
				},
				Exclude: []*config2.SelectionSpec{
					{
						Pods: []string{"ex-"},
					},
				},
			},
			"*",
			false,
		},
		{
			"tested pod name skip",
			&v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name: "ex-pod1",
				},
			},
			&config2.BugReportConfig{
				Include: []*config2.SelectionSpec{
					{
						Pods: []string{"in-"},
					},
				},
				Exclude: []*config2.SelectionSpec{
					{
						Pods: []string{"ex-"},
					},
				},
			},
			"*",
			true,
		},
		{
			"tested deployment not skip",
			&v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name: "in-test1",
				},
			},
			&config2.BugReportConfig{
				Include: []*config2.SelectionSpec{
					{
						Deployments: []string{"in-"},
					},
				},
				Exclude: []*config2.SelectionSpec{
					{
						Deployments: []string{"ex-"},
					},
				},
			},
			"in-dep1",
			false,
		},
		{
			"tested deployment skip",
			&v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name: "in-test1",
				},
			},
			&config2.BugReportConfig{
				Include: []*config2.SelectionSpec{
					{
						Pods:        []string{"in-"},
						Deployments: []string{"in-"},
					},
				},
				Exclude: []*config2.SelectionSpec{
					{
						Pods:        []string{"ex-"},
						Deployments: []string{"ex-"},
					},
				},
			},
			"ex-dep1",
			true,
		},
		{
			"tested container not skip",
			&v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name: "in-test1",
				},
				Spec: v1.PodSpec{
					Containers: []v1.Container{
						{
							Name: "in-con1",
						},
					},
				},
			},
			&config2.BugReportConfig{
				Include: []*config2.SelectionSpec{
					{
						Pods:       []string{"in-"},
						Containers: []string{"in-"},
					},
				},
				Exclude: []*config2.SelectionSpec{
					{
						Pods:       []string{"ex-"},
						Containers: []string{"ex-"},
					},
				},
			},
			"*",
			false,
		},
		{
			"tested container skip",
			&v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name: "in-test1",
				},
				Spec: v1.PodSpec{
					Containers: []v1.Container{
						{
							Name: "ex-con1",
						},
					},
				},
			},
			&config2.BugReportConfig{
				Include: []*config2.SelectionSpec{
					{
						Pods:       []string{"in-"},
						Containers: []string{"in-"},
					},
				},
				Exclude: []*config2.SelectionSpec{
					{
						Pods:       []string{"ex-"},
						Containers: []string{"ex-"},
					},
				},
			},
			"*",
			true,
		},
		{
			"tested label not skip",
			&v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name: "in-test1",
					Labels: map[string]string{
						"l1": "lv1",
					},
					Annotations: map[string]string{
						"a1": "av1",
						"a2": "av1",
					},
				},
			},
			&config2.BugReportConfig{
				Include: []*config2.SelectionSpec{
					{
						Pods: []string{"in-"},
						Labels: map[string]string{
							"l1": "lv1",
							"l2": "lv2",
						},
					},
				},
			},
			"*",
			false,
		},
		{
			"tested label skip",
			&v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name: "in-test1",
					Labels: map[string]string{
						"l1": "lv1",
					},
					Annotations: map[string]string{
						"a1": "av1",
						"a2": "av1",
					},
				},
			},
			&config2.BugReportConfig{
				Include: []*config2.SelectionSpec{
					{
						Pods: []string{"in-"},
						Labels: map[string]string{
							"l2": "lv2",
						},
					},
				},
			},
			"*",
			true,
		},
		{
			"tested annotation not skip",
			&v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name: "in-test1",
					Labels: map[string]string{
						"l3": "lv3",
						"l4": "lv4",
					},
					Annotations: map[string]string{
						"a3": "av3",
						"a4": "av4",
					},
				},
			},
			&config2.BugReportConfig{
				Include: []*config2.SelectionSpec{
					{
						Pods: []string{"in-"},
						Annotations: map[string]string{
							"a3": "av3",
							"a4": "av4",
						},
					},
				},
			},
			"*",
			false,
		},
		{
			"tested annotation skip",
			&v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name: "in-test1",
					Labels: map[string]string{
						"l3": "lv3",
						"l4": "lv4",
					},
					Annotations: map[string]string{
						"a3": "av3",
					},
				},
			},
			&config2.BugReportConfig{
				Include: []*config2.SelectionSpec{
					{
						Pods: []string{"in-"},
						Annotations: map[string]string{
							"a4": "av4",
						},
					},
				},
			},
			"*",
			true,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			skip := shouldSkip(c.deployment, c.config, c.pod)
			if skip != c.expected {
				t.Errorf("shouldSkip() for test case name [%s] return= %v, want %v", c.name, skip, c.expected)
			}
		})
	}
}
