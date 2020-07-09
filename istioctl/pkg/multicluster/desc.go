// Copyright Istio Authors.
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

package multicluster

import (
	"fmt"
	"io/ioutil"

	"github.com/ghodss/yaml"
)

// MeshDesc describes the topology of a multi-cluster mesh. The clustersByContext in the mesh reference the active
// Kubeconfig file as described by https://kubernetes.io/docs/concepts/configuration/organize-cluster-access-kubeconfig.
type MeshDesc struct {
	// Mesh Identifier.
	MeshID string `json:"mesh_id,omitempty"`

	// Collection of clusters in the multi-cluster mesh. Clusters are indexed by context name and
	// reference clusters defined in the Kubeconfig following kubectl precedence rules.
	Clusters map[string]ClusterDesc `json:"contexts,omitempty"`
}

// ClusterDesc describes attributes of a cluster and the desired state of joining the mesh.
type ClusterDesc struct {
	// Name of the cluster's network
	Network string `json:"network,omitempty"`

	// Optional Namespace override of the Istio control plane. `istio-system` if not set.
	Namespace string `json:"namespace,omitempty"`

	// Optional service account to use for cross-cluster authentication. `istio-multi` if not set.
	ServiceAccountReader string `json:"serviceAccountReader"`

	// When true, disables linking the service registry of this cluster with other clustersByContext in the mesh.
	DisableRegistryJoin bool `json:"disableRegistryJoin,omitempty"`
}

// LoadMeshDesc loads the MeshDesc from the given file.
func LoadMeshDesc(filename string) (*MeshDesc, error) {
	out, err := ioutil.ReadFile(filename)
	if err != nil {
		return nil, fmt.Errorf("cannot read %v: %v", filename, err)
	}
	md := &MeshDesc{}
	if err := yaml.Unmarshal(out, md); err != nil {
		return nil, err
	}
	return md, nil
}
