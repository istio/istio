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

	"istio.io/istio/pkg/kube"
)

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

// GetByName returns the Cluster with the given name or nil if it is not in the list.
func (c Clusters) GetByName(name string) Cluster {
	for _, cc := range c {
		if cc.Name() == name {
			return cc
		}
	}
	return nil
}

// Names returns the deduped list of names of the clusters.
func (c Clusters) Names() []string {
	dedup := map[string]struct{}{}
	for _, cc := range c {
		dedup[cc.Name()] = struct{}{}
	}
	var names []string
	for n := range dedup {
		names = append(names, n)
	}
	return names
}

// ByNetwork returns a map of network name to a subset of clusters
func (c Clusters) ByNetwork() map[string]Clusters {
	out := map[string]Clusters{}
	for _, cc := range c {
		out[cc.NetworkName()] = append(out[cc.NetworkName()], cc)
	}
	return out
}

// Primaries returns the subset that are primary clusters.
func (c Clusters) Primaries(excluded ...Cluster) Clusters {
	return c.filterClusters(func(cc Cluster) bool {
		return cc.IsPrimary()
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
	excluded func(Cluster) bool) Clusters {
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

// Cluster in a multicluster environment.
type Cluster interface {
	fmt.Stringer
	kube.ExtendedClient

	// Name of this cluster. Use for interacting with the cluster or validation against clusters.
	// Use StableName instead of Name when creating subtests.
	Name() string

	// StableName gives a deterministic name for the cluster. Use this for test/subtest names to
	// allow test grid to compare runs, even when the underlying cluster names are dynamic.
	// Use Name for validation/interaction with the actual cluster.
	StableName() string

	// Kind of cluster
	Kind() Kind

	// NetworkName the cluster is on
	NetworkName() string

	// MinKubeVersion returns true if the cluster is at least the version specified,
	// false otherwise
	MinKubeVersion(major, minor int) bool

	// IsPrimary returns true if this is a primary cluster, containing an instance
	// of the Istio control plane.
	IsPrimary() bool

	// IsConfig returns true if this is a config cluster, used as the source of
	// Istio config for one or more control planes.
	IsConfig() bool

	// IsRemote returns true if this is a remote cluster, which uses a control plane
	// residing in another cluster.
	IsRemote() bool

	// Primary returns the primary cluster for this cluster. Will return itself if
	// IsPrimary.
	Primary() Cluster

	// PrimaryName returns the name of the primary cluster for this cluster.
	PrimaryName() string

	// Config returns the config cluster for this cluster. Will return itself if
	// IsConfig.
	Config() Cluster

	// ConfigName returns the name of the config cluster for this cluster.
	ConfigName() string
}
