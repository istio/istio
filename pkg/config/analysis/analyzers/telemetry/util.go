// Copyright Istio Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
package telemetry

import (
	"istio.io/api/mesh/v1alpha1"
	"istio.io/istio/pkg/config/analysis"
	"istio.io/istio/pkg/config/analysis/analyzers/util"
	"istio.io/istio/pkg/config/resource"
	"istio.io/istio/pkg/config/schema/gvk"
)

func getNames(entries []*resource.Instance) []string {
	names := make([]string, 0, len(entries))
	for _, rs := range entries {
		names = append(names, string(rs.Metadata.FullName.Name))
	}
	return names
}

func fetchMeshConfig(c analysis.Context) *v1alpha1.MeshConfig {
	var meshConfig *v1alpha1.MeshConfig
	c.ForEach(gvk.MeshConfig, func(r *resource.Instance) bool {
		meshConfig = r.Message.(*v1alpha1.MeshConfig)
		return r.Metadata.FullName.Name != util.MeshConfigName
	})

	return meshConfig
}
