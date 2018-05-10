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
	"reflect"

	ext "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1beta1"

	"istio.io/istio/galley/pkg/common"
	"istio.io/istio/pkg/log"
)

// Equals checks whether the given two CRDs are equal or not.
func equals(c1 *ext.CustomResourceDefinition, c2 *ext.CustomResourceDefinition) bool {
	result := common.MapEquals(c1.Labels, c2.Labels) &&
		common.MapEquals(c1.Annotations, c2.Annotations, common.KnownAnnotations...) &&
		reflect.DeepEqual(c1.Spec, c2.Spec)

	log.Debugf("CRD Equality Check: %s (%v)", c1.Name, result)
	return result

}
