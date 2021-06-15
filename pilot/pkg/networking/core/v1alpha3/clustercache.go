package v1alpha3

import (
	"sync"

	cluster "github.com/envoyproxy/go-control-plane/envoy/config/cluster/v3"
)

type CDSCache struct {
	mutex       sync.RWMutex
	blackHole   *cluster.Cluster
	passthrough *cluster.Cluster
}

func (c *CDSCache) GetBlackHoleCluster() *cluster.Cluster {
	c.mutex.RLock()
	defer c.mutex.RUnlock()

	return c.blackHole
}

func (c *CDSCache) SetBlackHoleCluster(cluster *cluster.Cluster) {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	c.blackHole = cluster
}

func (c *CDSCache) GetPassthroughCluster() *cluster.Cluster {
	c.mutex.RLock()
	defer c.mutex.RUnlock()

	return c.passthrough
}

func (c *CDSCache) SetPassthroughCluster(cluster *cluster.Cluster) {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	c.passthrough = cluster
}
