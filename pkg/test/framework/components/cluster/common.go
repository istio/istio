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
	clusters map[string]resource.Cluster,
) Topology {
	return Topology{
		name:                name,
		networkName:         networkName,
		controlPlaneCluster: controlPlaneCluster,
		configCluster:       configCluster,
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
	// clusters should contain all clusters in the context
	clusters map[string]resource.Cluster
}

// NetworkName the cluster is on
func (c Topology) NetworkName() string {
	return c.networkName
}

// Name provides the name this cluster used by Istio.
func (c Topology) Name() string {
	return c.name
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
	cluster, ok := c.clusters[c.controlPlaneCluster]
	if !ok || cluster == nil {
		panic(fmt.Errorf("cannot find %s, the primary cluster for %s", c.controlPlaneCluster, c.Name()))
	}
	return cluster
}

func (c Topology) Config() resource.Cluster {
	cluster, ok := c.clusters[c.configCluster]
	if !ok || cluster == nil {
		panic(fmt.Errorf("cannot find %s, the config cluster for %s", c.configCluster, c.Name()))
	}
	return cluster
}
