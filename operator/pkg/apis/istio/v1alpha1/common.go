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

package v1alpha1

import (
	"istio.io/api/operator/v1alpha1"
)

const (
	globalKey         = "global"
	istioNamespaceKey = "istioNamespace"
)

// Namespace returns the namespace of the containing CR.
func Namespace(iops *v1alpha1.IstioOperatorSpec) string {
	if iops.Namespace != "" {
		return iops.Namespace
	}
	if iops.Values == nil {
		return ""
	}
	v := iops.Values.AsMap()
	if v[globalKey] == nil {
		return ""
	}
	vg := v[globalKey].(map[string]any)
	n := vg[istioNamespaceKey]
	if n == nil {
		return ""
	}
	return n.(string)
}

// SetNamespace returns the namespace of the containing CR.
func SetNamespace(iops *v1alpha1.IstioOperatorSpec, namespace string) {
	if namespace != "" {
		iops.Namespace = namespace
	}
	// TODO implement
}
