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

package resource

import (
	"fmt"
	"reflect"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"istio.io/istio/galley/pkg/common"
)

func TestRewrite(t *testing.T) {
	tests := []struct {
		i  *unstructured.Unstructured
		av string
		e  *unstructured.Unstructured
	}{
		{
			i: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"apiVersion": "g1/v1",
					"kind":       "foo",
					"metadata": map[string]interface{}{
						"uid":             "uid",
						"selfLink":        "selfLink",
						"resourceVersion": "rv1",
						"annotations": map[string]interface{}{
							common.KubectlLastAppliedConfiguration: "foo",
						},
					},
					"spec": map[string]interface{}{
						"foo": "bar",
					},
				},
			},
			av: "g2/v2",
			e: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"apiVersion": "g2/v2",
					"kind":       "foo",
					"metadata": map[string]interface{}{
						"uid":             "",
						"selfLink":        "",
						"resourceVersion": "",
						"annotations": map[string]interface{}{
							common.AnnotationKeySyncedAtVersion: "rv1",
						},
					},
					"spec": map[string]interface{}{
						"foo": "bar",
					},
				},
			},
		},

		{
			i: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"apiVersion": "g1/v1",
					"kind":       "foo",
					"metadata": map[string]interface{}{
						"uid":             "uid",
						"selfLink":        "selfLink",
						"resourceVersion": "rv1",
						// no annotations
					},
					"spec": map[string]interface{}{
						"foo": "bar",
					},
				},
			},
			av: "g2/v2",
			e: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"apiVersion": "g2/v2",
					"kind":       "foo",
					"metadata": map[string]interface{}{
						"uid":             "",
						"selfLink":        "",
						"resourceVersion": "",
						"annotations": map[string]interface{}{
							common.AnnotationKeySyncedAtVersion: "rv1",
						},
					},
					"spec": map[string]interface{}{
						"foo": "bar",
					},
				},
			},
		},
	}

	for i, tst := range tests {
		t.Run(fmt.Sprintf("%d", i), func(tt *testing.T) {
			actual := rewrite(tst.i, tst.av)
			if !reflect.DeepEqual(actual, tst.e) {
				tt.Fatalf("mismatch. got:\n%v\nwanted:\n%v\n", actual, tst.e)
			}
		})
	}
}
