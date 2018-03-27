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
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"istio.io/istio/galley/pkg/common"
)

func TestEquals(t *testing.T) {
	u1 := &unstructured.Unstructured{}
	u2 := &unstructured.Unstructured{}
	checkUnstructuredEquals(t, true, u1, u2)

	u1.SetLabels(map[string]string{"foo": "bar"})
	u2.SetLabels(map[string]string{"foo": "bar"})
	checkUnstructuredEquals(t, true, u1, u2)

	u1.SetLabels(map[string]string{"foo": "bar"})
	u2.SetLabels(map[string]string{"bar": "foo"})
	checkUnstructuredEquals(t, false, u1, u2)

	u1.SetLabels(nil)
	u2.SetLabels(nil)

	u1.SetAnnotations(map[string]string{"foo": "bar"})
	u2.SetAnnotations(map[string]string{"foo": "bar"})
	checkUnstructuredEquals(t, true, u1, u2)

	u1.SetAnnotations(map[string]string{"foo": "bar"})
	u2.SetAnnotations(map[string]string{"bar": "foo"})
	checkUnstructuredEquals(t, false, u1, u2)

	u1.SetAnnotations(map[string]string{common.KubectlLastAppliedConfiguration: "bar"})
	u2.SetAnnotations(map[string]string{common.KubectlLastAppliedConfiguration: "foo"})
	checkUnstructuredEquals(t, true, u1, u2)

	u1.SetAnnotations(map[string]string{common.AnnotationKeySyncedAtVersion: "bar"})
	u2.SetAnnotations(map[string]string{common.AnnotationKeySyncedAtVersion: "foo"})
	checkUnstructuredEquals(t, true, u1, u2)

	u1.SetAnnotations(nil)
	u2.SetAnnotations(nil)

	u1.Object["spec"] = map[string]interface{}{"foo": "bar"}
	u2.Object["spec"] = map[string]interface{}{"foo": "bar"}
	checkUnstructuredEquals(t, true, u1, u2)

	u1.Object["spec"] = map[string]interface{}{"foo": "bar"}
	u2.Object["spec"] = map[string]interface{}{"bar": "foo"}
	checkUnstructuredEquals(t, false, u1, u2)
}

func checkUnstructuredEquals(t *testing.T, expected bool, u1, u2 *unstructured.Unstructured) {
	actual := equals(u1, u2)

	if actual != expected {
		t.Fatalf("mismatch: got:%v, wanted:%v\nu1:\n%v\nu2:\n%v\n", actual, expected, u1, u2)
	}
}
