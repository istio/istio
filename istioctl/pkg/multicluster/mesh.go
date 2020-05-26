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
	"sort"
	"strings"

	"github.com/ghodss/yaml"
	"github.com/spf13/cobra"
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

func (m *Mesh) addCluster(c *Cluster) {
	m.clustersByContext[c.Context] = c
	m.clustersByClusterName[c.clusterName] = c
}

func (m *Mesh) SortedClusters() []*Cluster {
	sortedClusters := make([]*Cluster, 0, len(m.clustersByContext))
	for _, other := range m.clustersByContext {
		sortedClusters = append(sortedClusters, other)
	}
	sort.Slice(sortedClusters, func(i, j int) bool {
		return strings.Compare(sortedClusters[i].clusterName, sortedClusters[j].clusterName) < 0
	})
	return sortedClusters
}

type Mesh struct {
	meshID                string
	clustersByContext     map[string]*Cluster // by Context
	clustersByClusterName map[string]*Cluster
}

func LoadMeshDesc(filename string, env Environment) (*MeshDesc, error) {
	out, err := env.ReadFile(filename)
	if err != nil {
		return nil, fmt.Errorf("cannot read %v: %v", filename, err)
	}
	md := &MeshDesc{}
	if err := yaml.Unmarshal(out, md); err != nil {
		return nil, err
	}
	return md, nil
}

func NewMesh(md *MeshDesc, clusters ...*Cluster) *Mesh {
	mesh := &Mesh{
		meshID:                md.MeshID,
		clustersByContext:     make(map[string]*Cluster),
		clustersByClusterName: make(map[string]*Cluster),
	}
	for _, cluster := range clusters {
		mesh.addCluster(cluster)
	}
	return mesh
}

func meshFromFileDesc(filename string, env Environment) (*Mesh, error) {
	md, err := LoadMeshDesc(filename, env)
	if err != nil {
		return nil, err
	}

	clusters := make([]*Cluster, 0, len(md.Clusters))
	for context, clusterDesc := range md.Clusters {
		cluster, err := NewCluster(context, clusterDesc, env)
		if err != nil {
			return nil, fmt.Errorf("error discovering %v: %v", context, err)
		}
		clusters = append(clusters, cluster)
	}

	return NewMesh(md, clusters...), nil
}

func NewMulticlusterCommand() *cobra.Command {
	c := &cobra.Command{
		Use:     "multicluster",
		Short:   `Commands to assist in managing a multi-cluster mesh`,
		Aliases: []string{"mc"},
	}

	c.AddCommand(
		NewGenerateCommand(),
		NewApplyCommand(),
		NewDescribeCommand(),
	)

	return c
}
