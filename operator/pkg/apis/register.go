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

// NOTE: Boilerplate only.  Ignore this file.

// Package v1alpha1 contains API Schema definitions for the istio v1alpha1 API group
// +k8s:deepcopy-gen=package,register
// +groupName=install.istio.io
package apis

import (
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// IstioOperatorGVK is GVK for IstioOperator
var IstioOperatorGVK = schema.GroupVersionKind{
	Version: "v1alpha1",
	Group:   "install.istio.io",
	Kind:    "IstioOperator",
}
