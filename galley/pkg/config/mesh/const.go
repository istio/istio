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

package mesh

import (
	"istio.io/istio/pkg/config/resource"
)

// MeshConfigResourceName is the resource name for the Istio MeshConfig resource.
var MeshConfigResourceName = resource.NewFullName("istio-system", "meshconfig")

// MeshNetworksResourceName is the resource name for the Istio MeshNetworks resource.
var MeshNetworksResourceName = resource.NewFullName("istio-system", "meshnetworks")
