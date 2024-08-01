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

// Namespace returns the namespace of the containing CR.
func Namespace(iops *IstioOperatorSpec) string {
	if iops.Namespace != "" {
		return iops.Namespace
	}
	return iops.GetValues().GetGlobal().GetIstioNamespace()
}

// SetNamespace returns the namespace of the containing CR.
func SetNamespace(iops *IstioOperatorSpec, namespace string) {
	if namespace != "" {
		iops.Namespace = namespace
	}
	// TODO implement
}
