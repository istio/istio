// Copyright Istio Authors
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
	"crypto/sha256"
	"time"

	"go.uber.org/atomic"
	corev1 "k8s.io/api/core/v1"

	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pkg/cluster"
	"istio.io/istio/pkg/config/mesh"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/kube/kclient"
	filter "istio.io/istio/pkg/kube/namespace"
	"istio.io/istio/pkg/log"
)

// Cluster defines cluster struct
type Cluster struct {
	// ID of the cluster.
	ID cluster.ID
	// Client for accessing the cluster.
	Client kube.Client

	kubeConfigSha [sha256.Size]byte

	stop chan struct{}
	// initialSync is marked when RunAndWait completes
	initialSync *atomic.Bool
	// initialSyncTimeout is set when RunAndWait timed out
	initialSyncTimeout *atomic.Bool
}

type ACTION int

const (
	Add ACTION = iota
	Update
)

func (a ACTION) String() string {
	switch a {
	case Add:
		return "Add"
	case Update:
		return "Update"
	}
	return "Unknown"
}

// Run starts the cluster's informers and waits for caches to sync. Once caches are synced, we mark the cluster synced.
// This should be called after each of the handlers have registered informers, and should be run in a goroutine.
func (c *Cluster) Run(mesh mesh.Watcher, handlers []handler, action ACTION) {
	if features.RemoteClusterTimeout > 0 {
		time.AfterFunc(features.RemoteClusterTimeout, func() {
			if !c.initialSync.Load() {
				log.Errorf("remote cluster %s failed to sync after %v", c.ID, features.RemoteClusterTimeout)
				timeouts.With(clusterLabel.Value(string(c.ID))).Increment()
			}
			c.initialSyncTimeout.Store(true)
		})
	}

	// Build a namespace watcher. This must have no filter, since this is our input to the filter itself.
	// This must be done before we build components, so they can access the filter.
	namespaces := kclient.New[*corev1.Namespace](c.Client)
	// This will start a namespace informer and wait for it to be ready. So we must start it in a go routine to avoid blocking.
	filter := filter.NewDiscoveryNamespacesFilter(namespaces, mesh, c.stop)
	kube.SetObjectFilter(c.Client, filter)

	syncers := make([]ComponentConstraint, 0, len(handlers))
	for _, h := range handlers {
		switch action {
		case Add:
			syncers = append(syncers, h.clusterAdded(c))
		case Update:
			syncers = append(syncers, h.clusterUpdated(c))
		}
	}
	if !c.Client.RunAndWait(c.stop) {
		log.Warnf("remote cluster %s failed to sync", c.ID)
		return
	}
	for _, h := range syncers {
		if !kube.WaitForCacheSync("cluster "+string(c.ID), c.stop, h.HasSynced) {
			log.Warnf("remote cluster %s failed to sync handler", c.ID)
			return
		}
	}

	c.initialSync.Store(true)
}

// Stop closes the stop channel, if is safe to be called multi times.
func (c *Cluster) Stop() {
	select {
	case <-c.stop:
		return
	default:
		close(c.stop)
	}
}

func (c *Cluster) HasSynced() bool {
	// It could happen when a wrong credential provide, this cluster has no chance to run.
	// In this case, the `initialSyncTimeout` will never be set
	// In order not block istiod start up, check close as well.
	if c.Closed() {
		return true
	}
	return c.initialSync.Load() || c.initialSyncTimeout.Load()
}

func (c *Cluster) Closed() bool {
	select {
	case <-c.stop:
		return true
	default:
		return false
	}
}

func (c *Cluster) SyncDidTimeout() bool {
	return !c.initialSync.Load() && c.initialSyncTimeout.Load()
}
