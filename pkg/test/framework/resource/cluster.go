//  Copyright Istio Authors
//
//  Licensed under the Apache License, Version 2.0 (the "License");
//  you may not use this file except in compliance with the License.
//  You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
//  Unless required by applicable law or agreed to in writing, software
//  distributed under the License is distributed on an "AS IS" BASIS,
//  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//  See the License for the specific language governing permissions and
//  limitations under the License.

package resource

import (
	"fmt"

	"istio.io/istio/pkg/kube"
)

// ClusterIndex is the index of a cluster within the Environment
type ClusterIndex int

// Clusters is an ordered list of Cluster instances.
type Clusters []Cluster

// IsMulticluster is a utility method that indicates whether there are multiple Clusters available.
func (c Clusters) IsMulticluster() bool {
	return len(c) > 1
}

// Default returns the first cluster in the list.
func (c Clusters) Default() Cluster {
	return c[0]
}

// GetOrDefault returns the given cluster if non-nil. Otherwise returns the first
// Cluster in the list.
func (c Clusters) GetOrDefault(cluster Cluster) Cluster {
	if cluster != nil {
		return cluster
	}
	return c.Default()
}

// Cluster in a multicluster environment.
type Cluster interface {
	fmt.Stringer
	kube.ExtendedClient

	// Name of this cluster
	Name() string

	// NetworkName the cluster is on
	NetworkName() string

	// Index of this Cluster within the Environment
	Index() ClusterIndex
}

var _ Cluster = FakeCluster{}

// FakeCluster used for testing.
type FakeCluster struct {
	kube.ExtendedClient

	NameValue        string
	NetworkNameValue string
	IndexValue       int
}

func (m FakeCluster) String() string {
	panic("implement me")
}

func (m FakeCluster) Name() string {
	return m.NameValue
}

func (m FakeCluster) NetworkName() string {
	return m.NetworkNameValue
}

func (m FakeCluster) Index() ClusterIndex {
	return ClusterIndex(m.IndexValue)
}
