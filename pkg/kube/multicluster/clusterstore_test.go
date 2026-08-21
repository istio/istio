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
	"testing"

	uberatomic "go.uber.org/atomic"

	"istio.io/istio/pkg/cluster"
	"istio.io/istio/pkg/test/util/assert"
)

const testClusterID cluster.ID = "c0"

func testStoreCluster(seed string) *Cluster {
	return &Cluster{
		ID:                       testClusterID,
		kubeConfigSha:            sha256.Sum256([]byte(seed)),
		stop:                     make(chan struct{}),
		initialSync:              uberatomic.NewBool(false),
		initialSyncTimeout:       uberatomic.NewBool(false),
		remoteClusterCollections: uberatomic.NewPointer[remoteClusterCollections](nil),
	}
}

// TestClusterStoreSwapServesPreviousUntilSynced verifies that while an update is in flight
// (the new cluster has not synced yet), AllReady keeps returning the previous synced cluster
// instead of dropping the cluster ID from the collection. Once the new cluster syncs, AllReady
// switches to it. This is what turns the update into a single Update event downstream rather
// than a Delete followed by an Add.
func TestClusterStoreSwapServesPreviousUntilSynced(t *testing.T) {
	store := NewClustersStore()
	secret := "istio-system/s0"
	id := testClusterID

	// Initial cluster, synced.
	old := testStoreCluster("kubeconfig-old")
	store.Swap(secret, id, old)
	old.initialSync.Store(true)

	ready := store.AllReady()
	if got := ready[secret][id]; got == nil || got.kubeConfigSha != old.kubeConfigSha {
		t.Fatalf("expected old cluster to be ready, got %v", got)
	}

	// Update: new cluster for the same ID, not synced yet.
	newCluster := testStoreCluster("kubeconfig-new")
	store.Swap(secret, id, newCluster)

	// While the new cluster is syncing, AllReady must still serve the previous (old) cluster.
	ready = store.AllReady()
	got := ready[secret][id]
	if got == nil {
		t.Fatalf("cluster %s was dropped during in-flight update; expected the previous cluster to keep serving", id)
	}
	assert.Equal(t, got.kubeConfigSha, old.kubeConfigSha)

	// New cluster finishes syncing.
	newCluster.initialSync.Store(true)

	ready = store.AllReady()
	got = ready[secret][id]
	if got == nil {
		t.Fatalf("expected the new cluster to be ready after syncing")
	}
	assert.Equal(t, got.kubeConfigSha, newCluster.kubeConfigSha)

	// The previous-cluster tracking must be cleared once the new cluster is serving.
	assert.Equal(t, store.getPreviousCluster(id), nil)
}

// TestClusterStoreSwapWithoutSyncedPreviousDrops verifies that if the previous cluster is not
// itself serviceable (e.g. it never synced), AllReady does not attempt to serve it and the
// cluster is skipped until the new cluster syncs, matching the pre-existing behavior.
func TestClusterStoreSwapWithoutSyncedPreviousDrops(t *testing.T) {
	store := NewClustersStore()
	secret := "istio-system/s0"
	id := testClusterID

	// Initial cluster that never synced.
	old := testStoreCluster("kubeconfig-old")
	store.Swap(secret, id, old)

	// Update before the old cluster ever synced.
	newCluster := testStoreCluster("kubeconfig-new")
	store.Swap(secret, id, newCluster)

	ready := store.AllReady()
	if _, ok := ready[secret]; ok {
		if got := ready[secret][id]; got != nil {
			t.Fatalf("did not expect a cluster to be served when neither old nor new is synced, got %v", got)
		}
	}
	// Nothing should be tracked as a serviceable previous cluster.
	assert.Equal(t, store.getPreviousCluster(id), nil)
}
