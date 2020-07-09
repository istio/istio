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

	"github.com/spf13/cobra"

	"istio.io/istio/pkg/kube"
)

// Mesh contains the clusters within the Istio mesh.
type Mesh struct {
	id             string
	clustersByName map[string]*Cluster
	subject        *Cluster
}

// ID returns the unique identifier of this Mesh.
func (m *Mesh) ID() string {
	return m.id
}

// Subject returns the cluster that is the subject of the current command.
func (m *Mesh) Subject() *Cluster {
	return m.subject
}

// GetCluster by name.
func (m *Mesh) GetCluster(name string) *Cluster {
	return m.clustersByName[name]
}

// AddCluster to this Mesh.
func (m *Mesh) AddCluster(c *Cluster) {
	m.clustersByName[c.Name] = c
}

// SortedCluster returns the list of Cluster instances sorted by name.
func (m *Mesh) SortedClusters() []*Cluster {
	sortedClusters := make([]*Cluster, 0, len(m.clustersByName))
	for _, other := range m.clustersByName {
		sortedClusters = append(sortedClusters, other)
	}
	sort.Slice(sortedClusters, func(i, j int) bool {
		return strings.Compare(sortedClusters[i].Name, sortedClusters[j].Name) < 0
	})
	return sortedClusters
}

// NewMesh creates a new Mesh instance from the given clusters.
func NewMesh(md *MeshDesc, subject *Cluster, clusters ...*Cluster) *Mesh {
	mesh := &Mesh{
		id:             md.MeshID,
		subject:        subject,
		clustersByName: make(map[string]*Cluster),
	}
	for _, cluster := range clusters {
		mesh.AddCluster(cluster)
	}
	return mesh
}

func meshFromFileDesc(filename, kubeconfig, subjectContext string, cfact ClientFactory, printer Printer) (*Mesh, error) {
	md, err := LoadMeshDesc(filename)
	if err != nil {
		return nil, err
	}

	var subjectCluster *Cluster
	clusters := make([]*Cluster, 0, len(md.Clusters))
	for context, clusterDesc := range md.Clusters {
		client, err := cfact.NewClient(kube.BuildClientCmd(kubeconfig, subjectContext))
		if err != nil {
			return nil, err
		}

		cluster, err := NewCluster(clusterDesc, context, client, printer)
		if err != nil {
			return nil, fmt.Errorf("error discovering %v: %v", context, err)
		}
		clusters = append(clusters, cluster)

		if context == subjectContext {
			subjectCluster = cluster
		}
	}

	if subjectCluster == nil {
		return nil, fmt.Errorf("unable to locate subject cluster for context %s", subjectCluster)
	}

	return NewMesh(md, subjectCluster, clusters...), nil
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
