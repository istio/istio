//  Copyright 2018 Istio Authors
//
//  Licensed under the Apache License, Version 2.0 (the "License");
//  you may not use this file except in compliance with the License.
//  You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
//  Unless required by applicable law or agreed to in writing, software
//  distributed under the License is distributed on an "AS IS" BASIS,
//  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//  See the License for the specific language governing permissions and
//  limitations under the License.

package crd

import (
	"fmt"
	"testing"

	ext "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1beta1"
	"k8s.io/apimachinery/pkg/apis/meta/v1"

	"istio.io/istio/galley/pkg/common"
)

func TestEquals(t *testing.T) {
	tests := []struct {
		c1       *ext.CustomResourceDefinition
		c2       *ext.CustomResourceDefinition
		expected bool
	}{
		{
			c1:       &ext.CustomResourceDefinition{},
			c2:       &ext.CustomResourceDefinition{},
			expected: true,
		},
		{
			c1: &ext.CustomResourceDefinition{
				ObjectMeta: v1.ObjectMeta{Name: "foo"},
				TypeMeta:   v1.TypeMeta{Kind: "foo"},
			},
			c2: &ext.CustomResourceDefinition{
				ObjectMeta: v1.ObjectMeta{Name: "bar"},
				TypeMeta:   v1.TypeMeta{Kind: "bar"},
			},
			expected: true,
		},
		{
			c1: &ext.CustomResourceDefinition{
				ObjectMeta: v1.ObjectMeta{Labels: map[string]string{
					"foo": "bar",
				}},
			},
			c2: &ext.CustomResourceDefinition{
				ObjectMeta: v1.ObjectMeta{Labels: map[string]string{
					"foo": "notbar",
				}},
			},
			expected: false,
		},
		{
			c1: &ext.CustomResourceDefinition{
				ObjectMeta: v1.ObjectMeta{Labels: map[string]string{
					"foo": "bar",
				}},
			},
			c2: &ext.CustomResourceDefinition{
				ObjectMeta: v1.ObjectMeta{Labels: map[string]string{
					"foo": "bar",
				}},
			},
			expected: true,
		},
		{
			c1: &ext.CustomResourceDefinition{
				ObjectMeta: v1.ObjectMeta{Annotations: map[string]string{
					"foo": "bar",
				}},
			},
			c2: &ext.CustomResourceDefinition{
				ObjectMeta: v1.ObjectMeta{Annotations: map[string]string{
					"foo": "bar",
				}},
			},
			expected: true,
		},
		{
			c1: &ext.CustomResourceDefinition{
				ObjectMeta: v1.ObjectMeta{Annotations: map[string]string{
					"foo": "bar",
				}},
			},
			c2: &ext.CustomResourceDefinition{
				ObjectMeta: v1.ObjectMeta{Annotations: map[string]string{
					"foo": "notbar",
				}},
			},
			expected: false,
		},
		{
			c1: &ext.CustomResourceDefinition{
				ObjectMeta: v1.ObjectMeta{Annotations: map[string]string{
					"foo": "bar",
					common.AnnotationKeySyncedAtVersion:    "2",
					common.KubectlLastAppliedConfiguration: "cfg2",
				}},
			},
			c2: &ext.CustomResourceDefinition{
				ObjectMeta: v1.ObjectMeta{Annotations: map[string]string{
					"foo": "bar",
					common.AnnotationKeySyncedAtVersion:    "1",
					common.KubectlLastAppliedConfiguration: "cfg1",
				}},
			},
			expected: true,
		},
		{
			c1: &ext.CustomResourceDefinition{
				Spec: ext.CustomResourceDefinitionSpec{
					Version: "v1",
				},
			},
			c2: &ext.CustomResourceDefinition{
				Spec: ext.CustomResourceDefinitionSpec{
					Version: "v1",
				},
			},
			expected: true,
		},
		{
			c1: &ext.CustomResourceDefinition{
				Spec: ext.CustomResourceDefinitionSpec{
					Version: "v1",
				},
			},
			c2: &ext.CustomResourceDefinition{
				Spec: ext.CustomResourceDefinitionSpec{
					Version: "v2",
				},
			},
			expected: false,
		},
	}

	for i, tst := range tests {
		t.Run(fmt.Sprintf("%d", i), func(tt *testing.T) {
			actual := equals(tst.c1, tst.c2)
			if actual != tst.expected {
				tt.Fatalf("Unexpected result: got:'%v', wanted:'%v'", actual, tst.expected)
			}
		})
	}
}
