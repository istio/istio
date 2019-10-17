// Copyright 2019 Istio Authors.
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

	// Collection of clustersByContext in the multi-cluster mesh. Clusters are indexed by Context name and
	// reference clustersByContext defined in the Kubeconfig following kubectl precedence rules.
	Clusters map[string]ClusterDesc `json:"clusters,omitempty"`
}

// ClusterDesc describes attributes of a cluster and the desired state of joining the mesh.
type ClusterDesc struct {
	// Name of the cluster's network
	Network string `json:"network,omitempty"`

	// Optional Namespace override of the Istio control plane. `istio-system` if not set.
	Namespace string `json:"Namespace,omitempty"`

	// Optional service account to use for cross-cluster authentication. `istio-multi` if not set.
	ServiceAccountReader string `json:"serviceAccountReader"`

	// When true, disables linking the service registry of this cluster with other clustersByContext in the mesh.
	DisableServiceDiscovery bool `json:"joinServiceDiscovery,omitempty"`
}

type Mesh struct {
	meshID            string
	clustersByContext map[string]*Cluster // by context
	sortedClusters    []*Cluster
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

func NewMesh(kubeconfig string, md *MeshDesc, env Environment) (*Mesh, error) {
	clusters := make(map[string]*Cluster)
	for context, clusterDesc := range md.Clusters {
		cluster, err := NewCluster(context, clusterDesc, env)
		if err != nil {
			return nil, fmt.Errorf("error discovering %v: %v", context, err)
		}
		clusters[context] = cluster
	}

	sortedClusters := make([]*Cluster, 0, len(clusters))
	for _, other := range clusters {
		sortedClusters = append(sortedClusters, other)
	}
	sort.Slice(sortedClusters, func(i, j int) bool {
		return strings.Compare(string(sortedClusters[i].uid), string(sortedClusters[j].uid)) < 0
	})

	return &Mesh{
		meshID:            md.MeshID,
		clustersByContext: clusters,
		sortedClusters:    sortedClusters,
	}, nil
}

func meshFromFileDesc(filename, kubeconfig string, env Environment) (*Mesh, error) {
	md, err := LoadMeshDesc(filename, env)
	if err != nil {
		return nil, err
	}
	mesh, err := NewMesh(kubeconfig, md, env)
	if err != nil {
		return nil, err
	}
	return mesh, err
}

func NewMulticlusterCommand() *cobra.Command {
	c := &cobra.Command{
		Use:     "multicluster",
		Short:   `Commands to assist in managing a multi-cluster mesh`,
		Aliases: []string{"mc"},
	}

	c.AddCommand(
		NewGenerateCommand(),
		NewJoinCommand(),
		NewDescribeCommand(),
	)

	return c
}
