// Copyright 2019 Istio Authors
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

package meshcfg

import (
	"istio.io/istio/pkg/config/mesh"

	"istio.io/api/mesh/v1alpha1"
)

// Default mesh configuration
func Default() *v1alpha1.MeshConfig {
	meshconfig := mesh.DefaultMeshConfig()
	meshconfig.IngressClass = "istio"
	meshconfig.IngressControllerMode = v1alpha1.MeshConfig_STRICT
	return &meshconfig
}
