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
	"reflect"
	"testing"

	ext "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1beta1"
	"k8s.io/apimachinery/pkg/apis/meta/v1"

	"istio.io/istio/galley/pkg/common"
)

func TestRewrite(t *testing.T) {
	tests := []struct {
		crd     *ext.CustomResourceDefinition
		group   string
		version string

		expected *ext.CustomResourceDefinition
	}{
		{
			crd: &ext.CustomResourceDefinition{
				ObjectMeta: v1.ObjectMeta{Name: "foo.bar", ResourceVersion: "rv1"},
				Spec: ext.CustomResourceDefinitionSpec{
					Group:   "g1",
					Version: "v1",
				},
			},
			group:   "g2",
			version: "v2",
			expected: &ext.CustomResourceDefinition{
				ObjectMeta: v1.ObjectMeta{
					Name: "foo.g2",
					Annotations: map[string]string{
						common.AnnotationKeySyncedAtVersion: "rv1",
					},
				},
				Spec: ext.CustomResourceDefinitionSpec{
					Group:   "g2",
					Version: "v2",
				},
			},
		},

		{
			crd: &ext.CustomResourceDefinition{
				ObjectMeta: v1.ObjectMeta{
					Name:            "foo.bar",
					ResourceVersion: "rv1",
					Annotations: map[string]string{
						common.AnnotationKeySyncedAtVersion:    "rv5",
						common.KubectlLastAppliedConfiguration: "some-config-here",
					},
					UID: "uuuiiiddd",
				},
				Spec: ext.CustomResourceDefinitionSpec{
					Group:   "g1",
					Version: "v1",
				},
			},
			group:   "g2",
			version: "v2",
			expected: &ext.CustomResourceDefinition{
				ObjectMeta: v1.ObjectMeta{
					Name: "foo.g2",
					Annotations: map[string]string{
						common.AnnotationKeySyncedAtVersion: "rv1",
					},
				},
				Spec: ext.CustomResourceDefinitionSpec{
					Group:   "g2",
					Version: "v2",
				},
			},
		},
	}

	for i, tst := range tests {
		t.Run(fmt.Sprintf("%d", i), func(tt *testing.T) {
			actual := rewrite(tst.crd, tst.group, tst.version)

			if !reflect.DeepEqual(actual, tst.expected) {
				tt.Fatalf("result mismatch: got:\n%v\nwanted:\n%v\n", actual, tst.expected)
			}
		})
	}
}
