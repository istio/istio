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

package cmd

import (
	"reflect"
	"testing"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func Test_podCountByRevision(t *testing.T) {
	cases := []struct {
		name            string
		pods            []v1.Pod
		rev             string
		injectEnabled   bool
		expectedCounter map[string]revisionCount
	}{
		{
			name: "policy disabled, disabled pod count",
			pods: []v1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test",
						Labels: map[string]string{
							"istio.io/rev": "test",
						},
						Annotations: map[string]string{
							"sidecar.istio.io/inject": "false",
						},
					},
				},
			},
			rev:           "test",
			injectEnabled: false,
			expectedCounter: map[string]revisionCount{
				"test": {
					pods:     1,
					disabled: 1,
				},
			},
		},
		{
			name: "policy disabled, pod count",
			pods: []v1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test",
						Labels: map[string]string{
							"istio.io/rev": "test",
						},
					},
				},
			},
			rev:           "test",
			injectEnabled: false,
			expectedCounter: map[string]revisionCount{
				"test": {
					pods:     1,
					disabled: 1,
				},
			},
		},
		{
			name: "policy disabled, enable pod, disabled count",
			pods: []v1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test",
						Labels: map[string]string{
							"istio.io/rev": "test",
						},
						Annotations: map[string]string{
							"sidecar.istio.io/inject": "true",
						},
					},
				},
			},
			rev:           "test",
			injectEnabled: false,
			expectedCounter: map[string]revisionCount{
				"test": {
					pods: 1,
				},
			},
		},
		{
			name: "policy enabled, disabled pod count",
			pods: []v1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test",
						Labels: map[string]string{
							"istio.io/rev": "test",
						},
						Annotations: map[string]string{
							"sidecar.istio.io/inject": "false",
						},
					},
				},
			},
			rev:           "test",
			injectEnabled: true,
			expectedCounter: map[string]revisionCount{
				"test": {
					pods:     1,
					disabled: 1,
				},
			},
		},
		{
			name: "policy enabled, pod count",
			pods: []v1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test",
						Labels: map[string]string{
							"istio.io/rev": "test",
						},
					},
				},
			},
			rev:           "test",
			injectEnabled: true,
			expectedCounter: map[string]revisionCount{
				"test": {
					pods: 1,
				},
			},
		},
		{
			name: "policy enabled, need restart",
			pods: []v1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test",
						Labels: map[string]string{
							"istio.io/rev": "test1",
						},
					},
				},
			},
			rev:           "test",
			injectEnabled: true,
			expectedCounter: map[string]revisionCount{
				"test1": {
					pods:         1,
					needsRestart: 1,
				},
			},
		},
		{
			name: "policy enabled, disabled, need restart",
			pods: []v1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test1",
						Labels: map[string]string{
							"istio.io/rev": "test",
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test2",
						Labels: map[string]string{
							"istio.io/rev": "test",
						},
						Annotations: map[string]string{
							"sidecar.istio.io/inject": "false",
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test3",
						Labels: map[string]string{
							"istio.io/rev": "test1",
						},
					},
				},
			},
			rev:           "test",
			injectEnabled: true,
			expectedCounter: map[string]revisionCount{
				"test": {
					pods:     2,
					disabled: 1,
				},
				"test1": {
					pods:         1,
					needsRestart: 1,
				},
			},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			res := podCountByRevision(c.pods, c.rev, c.injectEnabled)
			if !reflect.DeepEqual(res, c.expectedCounter) {
				t.Fatalf("podCountByRevision want: %v, but got: %v", c.expectedCounter, res)
			}
		})
	}
}
