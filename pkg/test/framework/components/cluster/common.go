package cluster

import (
	"fmt"
	"istio.io/istio/pkg/test/framework/resource"
)

func NewTopology(
	name string,
	networkName string,
	controlPlaneCluster string,
	configCluster string,
	index resource.ClusterIndex,
	clusters *resource.Clusters,
) Topology {
	return Topology{
		name:                name,
		networkName:         networkName,
		controlPlaneCluster: controlPlaneCluster,
		configCluster:       configCluster,
		index:               index,
		clusters:            clusters,
	}
}

// Topology gives information about the relationship between clusters.
// Cluster implementations can embed this struct to include common functionality.
type Topology struct {
	name                string
	networkName         string
	controlPlaneCluster string
	configCluster       string
	index               resource.ClusterIndex
	// clusters should contain all clusters in the context
	clusters *resource.Clusters
}

// NetworkName the cluster is on
func (c Topology) NetworkName() string {
	return c.networkName
}

// Name provides the name this cluster used by Istio.
func (c Topology) Name() string {
	return c.name
}

// Index of this cluster within the Environment.
func (c Topology) Index() resource.ClusterIndex {
	return c.index
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
	for _, cluster := range *c.clusters {
		if cluster.Name() == c.controlPlaneCluster {
			return cluster
		}
	}
	panic(fmt.Errorf("cannot find %s, the primary cluster for %s", c.controlPlaneCluster, c.Name()))
}

func (c Topology) Config() resource.Cluster {
	for _, cluster := range *c.clusters {
		if cluster.Name() == c.configCluster {
			return cluster
		}
	}
	panic(fmt.Errorf("cannot find %s, the config cluster for %s", c.configCluster, c.Name()))
}
