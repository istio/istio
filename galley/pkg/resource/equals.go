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
	"reflect"

	"istio.io/istio/galley/pkg/common"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// Equals checks whether the given two CRDs are equal or not.
func equals(u1 *unstructured.Unstructured, u2 *unstructured.Unstructured) bool {
	return common.MapEquals(u1.GetLabels(), u2.GetLabels()) &&
		common.MapEquals(u1.GetAnnotations(), u2.GetAnnotations(), common.KnownAnnotations...) &&
		reflect.DeepEqual(u1.Object["spec"], u2.Object["spec"])
}
