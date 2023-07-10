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

package cluster

import (
	"fmt"
	"sort"

	"istio.io/istio/pkg/util/sets"
)

// Clusters is an ordered list of Cluster instances.
type Clusters []Cluster

func (c Clusters) Len() int {
	return len(c)
}

// IsMulticluster is a utility method that indicates whether there are multiple Clusters available.
func (c Clusters) IsMulticluster() bool {
	return c.Len() > 1
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

// GetByName returns the Cluster with the given name or nil if it is not in the list.
func (c Clusters) GetByName(name string) Cluster {
	for _, cc := range c {
		if cc.Name() == name {
			return cc
		}
	}
	return nil
}

// Contains returns true if a cluster with the given name is found in the list.
func (c Clusters) Contains(cc Cluster) bool {
	return c.GetByName(cc.Name()) != nil
}

// Names returns the deduped list of names of the clusters.
func (c Clusters) Names() []string {
	dedup := sets.String{}
	for _, cc := range c {
		dedup.Insert(cc.Name())
	}
	return dedup.UnsortedList()
}

type ClustersByNetwork map[string]Clusters

func (c ClustersByNetwork) Networks() []string {
	out := make([]string, 0, len(c))
	for n := range c {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// ByNetwork returns a map of network name to a subset of clusters
func (c Clusters) ByNetwork() ClustersByNetwork {
	out := make(ClustersByNetwork)
	for _, cc := range c {
		out[cc.NetworkName()] = append(out[cc.NetworkName()], cc)
	}
	return out
}

// Networks returns the list of network names for the clusters.
func (c Clusters) Networks() []string {
	return c.ByNetwork().Networks()
}

// ForNetworks returns the list of clusters in the given networks.
func (c Clusters) ForNetworks(networks ...string) Clusters {
	out := make(Clusters, 0, len(c))
	for _, cc := range c {
		for _, network := range networks {
			if cc.NetworkName() == network {
				out = append(out, cc)
				break
			}
		}
	}
	return out
}

// Primaries returns the subset that are primary clusters.
func (c Clusters) Primaries(excluded ...Cluster) Clusters {
	return c.filterClusters(func(cc Cluster) bool {
		return cc.IsPrimary()
	}, exclude(excluded...))
}

// Exclude returns all clusters not given as input.
func (c Clusters) Exclude(excluded ...Cluster) Clusters {
	return c.filterClusters(func(cc Cluster) bool {
		return true
	}, exclude(excluded...))
}

// Configs returns the subset that are config clusters.
func (c Clusters) Configs(excluded ...Cluster) Clusters {
	return c.filterClusters(func(cc Cluster) bool {
		return cc.IsConfig()
	}, exclude(excluded...))
}

// Remotes returns the subset that are remote clusters.
func (c Clusters) Remotes(excluded ...Cluster) Clusters {
	return c.filterClusters(func(cc Cluster) bool {
		return cc.IsRemote()
	}, exclude(excluded...))
}

// MeshClusters returns the subset that are not external control plane clusters.
func (c Clusters) MeshClusters(excluded ...Cluster) Clusters {
	return c.filterClusters(func(cc Cluster) bool {
		return !cc.IsExternalControlPlane()
	}, exclude(excluded...))
}

// IsExternalControlPlane indicates whether the clusters are set up in an enternal
// control plane configuration. An external control plane is a primary cluster that
// gets its Istio configuration from a different cluster.
func (c Clusters) IsExternalControlPlane() bool {
	for _, cc := range c {
		if cc.IsExternalControlPlane() {
			return true
		}
	}
	return false
}

// Kube returns OfKind(cluster.Kubernetes)
func (c Clusters) Kube() Clusters {
	return c.OfKind(Kubernetes)
}

// OfKind filters clusters by their Kind.
func (c Clusters) OfKind(kind Kind) Clusters {
	return c.filterClusters(func(cc Cluster) bool {
		return cc.Kind() == kind
	}, none)
}

func none(Cluster) bool {
	return false
}

func exclude(exclude ...Cluster) func(Cluster) bool {
	return func(cc Cluster) bool {
		for _, e := range exclude {
			if cc.Name() == e.Name() {
				return true
			}
		}
		return false
	}
}

func (c Clusters) filterClusters(included func(Cluster) bool,
	excluded func(Cluster) bool,
) Clusters {
	var out Clusters
	for _, cc := range c {
		if !excluded(cc) && included(cc) {
			out = append(out, cc)
		}
	}
	return out
}

func (c Clusters) String() string {
	return fmt.Sprintf("%v", c.Names())
}
