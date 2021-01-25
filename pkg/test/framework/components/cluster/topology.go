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

	"istio.io/istio/pkg/test/framework/resource"
)

// Map can be given as a shared reference to multiple Topology/Cluster implemetations
// allowing clusters to find each other for lookups of Primary, ConfigCluster, etc.
type Map = map[string]resource.Cluster

// Topology gives information about the relationship between clusters.
// Cluster implementations can embed this struct to include common functionality.
type Topology struct {
	ClusterName        string
	Network            string
	PrimaryClusterName string
	ConfigClusterName  string
	// AllClusters should contain all AllClusters in the context
	AllClusters map[string]resource.Cluster
}

// NetworkName the cluster is on
func (c Topology) NetworkName() string {
	return c.Network
}

// Name provides the ClusterName this cluster used by Istio.
func (c Topology) Name() string {
	return c.ClusterName
}

func (c Topology) IsPrimary() bool {
	return c.Primary().Name() == c.Name()
}

func (c Topology) IsConfig() bool {
	return c.Config().Name() == c.Name()
}

func (c Topology) IsRemote() bool {
	return !c.IsPrimary()
}

func (c Topology) Primary() resource.Cluster {
	cluster, ok := c.AllClusters[c.PrimaryClusterName]
	if !ok || cluster == nil {
		panic(fmt.Errorf("cannot find %s, the primary cluster for %s", c.PrimaryClusterName, c.Name()))
	}
	return cluster
}

func (c Topology) PrimaryName() string {
	return c.PrimaryClusterName
}

func (c Topology) Config() resource.Cluster {
	cluster, ok := c.AllClusters[c.ConfigClusterName]
	if !ok || cluster == nil {
		panic(fmt.Errorf("cannot find %s, the config cluster for %s", c.ConfigClusterName, c.Name()))
	}
	return cluster
}

func (c Topology) ConfigName() string {
	return c.ConfigClusterName
}

func (c Topology) WithPrimary(primaryClusterName string) Topology {
	// TODO remove this, should only be provided by external config
	c.PrimaryClusterName = primaryClusterName
	return c
}

func (c Topology) WithConfig(configClusterName string) Topology {
	// TODO remove this, should only be provided by external config
	c.ConfigClusterName = configClusterName
	return c
}
