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
	"sync"

	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/tools/cache"

	k8scluster "istio.io/istio/pkg/cluster"
	"istio.io/istio/pkg/kube/controllers"
	"istio.io/istio/pkg/kube/kclient"
	"istio.io/istio/pkg/kube/krt"
	"istio.io/istio/pkg/kube/kubetypes"
	"istio.io/istio/pkg/maps"
	"istio.io/istio/pkg/slices"
	"istio.io/istio/pkg/util/sets"
)

type ComponentConstraint interface {
	Close()
	HasSynced() bool
}

// pendingSwap wraps a new component during an update operation.
// It tracks both the old and new components, completing the swap only after the new one syncs.
//
// Thread safety: parent, clusterID, and new are immutable after construction.
// mu protects old and hasOld which can be modified by HasSynced().
type pendingSwap[T ComponentConstraint] struct {
	// mu protects old and hasOld fields only.
	mu sync.Mutex
	// Immutable after construction - no mutex needed.
	parent    *Component[T]
	clusterID k8scluster.ID
	new       T
	// Mutable fields protected by mu.
	old    T
	hasOld bool
}

func (p *pendingSwap[T]) Close() {
	p.new.Close()
}

func (p *pendingSwap[T]) HasSynced() bool {
	if !p.new.HasSynced() {
		return false
	}
	// New component is synced, finalize the swap by closing the old one
	p.mu.Lock()
	if p.hasOld {
		p.old.Close()
		p.hasOld = false
	}
	p.mu.Unlock()
	// Clear the pending swap from the parent's map since swap is complete
	if p.parent != nil {
		p.parent.mu.Lock()
		delete(p.parent.pendingSwaps, p.clusterID)
		p.parent.mu.Unlock()
	}
	return true
}

// active returns the active component. During an update, this returns the old component
// until the new one has synced, ensuring seamless access without gaps.
func (p *pendingSwap[T]) active() T {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.new.HasSynced() && p.hasOld {
		// New component hasn't synced yet, return old component for seamless access
		return p.old
	}
	// New component has synced, return new component
	return p.new
}

type Component[T ComponentConstraint] struct {
	mu           sync.RWMutex
	constructor  func(cluster *Cluster) T
	clusters     map[k8scluster.ID]T
	pendingSwaps map[k8scluster.ID]*pendingSwap[T]
}

func (m *Component[T]) ForCluster(clusterID k8scluster.ID) *T {
	m.mu.RLock()
	defer m.mu.RUnlock()
	// Check if there's a pending swap for this cluster
	if ps, hasSwap := m.pendingSwaps[clusterID]; hasSwap {
		// Return the active component (old while syncing, new after sync)
		active := ps.active()
		return &active
	}
	// No pending swap, return the component from the map
	t, f := m.clusters[clusterID]
	if !f {
		return nil
	}
	return &t
}

func (m *Component[T]) All() []T {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return maps.Values(m.clusters)
}

func (m *Component[T]) clusterAdded(cluster *Cluster) ComponentConstraint {
	comp := m.constructor(cluster)
	m.mu.Lock()
	defer m.mu.Unlock()
	m.clusters[cluster.ID] = comp
	return comp
}

func (m *Component[T]) clusterUpdated(cluster *Cluster) ComponentConstraint {
	// Get old component (need lock to read from map)
	m.mu.Lock()
	old, hasOld := m.clusters[cluster.ID]
	m.mu.Unlock()

	// Store old component temporarily so constructor can access it for seamless migration
	cluster.prevComponent = old
	// Build outside of the lock, in case its slow
	comp := m.constructor(cluster)
	// Clear the temporary reference
	cluster.prevComponent = nil

	// Create pendingSwap to track both old and new components
	ps := &pendingSwap[T]{
		parent:    m,
		clusterID: cluster.ID,
		old:       old,
		new:       comp,
		hasOld:    hasOld,
	}

	// Store new component and pendingSwap (need lock to write to maps)
	m.mu.Lock()
	defer m.mu.Unlock()
	m.clusters[cluster.ID] = comp
	if m.pendingSwaps == nil {
		m.pendingSwaps = make(map[k8scluster.ID]*pendingSwap[T])
	}
	m.pendingSwaps[cluster.ID] = ps

	// Don't close old immediately - return a pendingSwap that will close old after new syncs.
	// This ensures zero service disruption during credential rotation.
	return ps
}

func (m *Component[T]) clusterDeleted(cluster k8scluster.ID) {
	m.mu.Lock()
	defer m.mu.Unlock()
	// If there is an old one, close it
	if old, f := m.clusters[cluster]; f {
		old.Close()
	}
	delete(m.clusters, cluster)
}

func (m *Component[T]) HasSynced() bool {
	for _, c := range m.All() {
		if !c.HasSynced() {
			return false
		}
	}
	return true
}

type KclientComponent[T controllers.ComparableObject] struct {
	internal *Component[kclientInternalComponent[T]]
}

type kclientInternalComponent[T controllers.ComparableObject] struct {
	client kclient.Client[T]
}

func (k kclientInternalComponent[T]) Close() {
	k.client.ShutdownHandlers()
}

func (k kclientInternalComponent[T]) HasSynced() bool {
	return k.client.HasSynced()
}

// BuildMultiClusterKclientComponent builds a simple Component that just wraps a single kclient.Client
func BuildMultiClusterKclientComponent[T controllers.ComparableObject](c ComponentBuilder, filter kubetypes.Filter) *KclientComponent[T] {
	res := BuildMultiClusterComponent[kclientInternalComponent[T]](c, func(cluster *Cluster) kclientInternalComponent[T] {
		return kclientInternalComponent[T]{kclient.NewFiltered[T](cluster.Client, filter)}
	})
	return &KclientComponent[T]{res}
}

// ForCluster returns the client for the requests cluster
// Note: this may return nil.
func (m *KclientComponent[T]) ForCluster(clusterID k8scluster.ID) kclient.Client[T] {
	c := m.internal.ForCluster(clusterID)
	if c == nil {
		return nil
	}
	return c.client
}

func (m *KclientComponent[T]) All() []kclient.Client[T] {
	return slices.Map(m.internal.All(), func(e kclientInternalComponent[T]) kclient.Client[T] {
		return e.client
	})
}

// wrappedEventHandler wraps an event handler to provide seamless migration during cluster updates.
// When a cluster is updated (e.g., during credential rotation), a new informer is created which
// emits ADD events for all existing objects in the cache. This wrapper intercepts those ADD events
// and converts them to the appropriate event types based on comparison with the old informer's state.
//
// This implementation is inspired by the state comparison pattern used in pkg/kube/krt/static.go's
// Reset method, which compares old and new states to emit appropriate UPDATE/DELETE/ADD events.
//
// The wrapper:
//   - Converts ADD → UPDATE for objects that exist in both old and new informers
//   - Keeps ADD for objects that only exist in the new informer
//   - Emits DELETE for objects that only exist in the old informer (after new informer syncs)
//
// This ensures that components consuming these events see a seamless transition without unnecessary
// re-processing of unchanged objects, and correctly detect removed objects.
func wrappedEventHandler[T controllers.ComparableObject](
	oldClient kclient.Informer[T],
	newClient kclient.Informer[T],
	handler cache.ResourceEventHandler,
	stop <-chan struct{},
) cache.ResourceEventHandler {
	if oldClient == nil {
		// No old state, pass through as-is
		return handler
	}

	var (
		seenKeys = sets.New[string]()
		oldState = make(map[string]T)
		mu       sync.Mutex
	)

	// Build old state map using object keys
	oldObjects := oldClient.List("", labels.Everything())
	for _, obj := range oldObjects {
		key := krt.GetKey(obj)
		oldState[key] = obj
	}

	// Monitor for sync completion and emit DELETE events for objects only in old
	go func() {
		// Wait for new informer to sync
		for {
			select {
			case <-stop:
				return
			default:
				if newClient.HasSynced() {
					mu.Lock()
					// Emit DELETE for objects that exist in old but not in new
					for key, oldObj := range oldState {
						if !seenKeys.Contains(key) {
							handler.OnDelete(oldObj)
						}
					}
					mu.Unlock()
					return
				}
				// Small delay before checking again
				select {
				case <-stop:
					return
				default:
				}
			}
		}
	}()

	return cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			newObj := controllers.Extract[T](obj)
			if controllers.IsNil(newObj) {
				return
			}

			key := krt.GetKey(newObj)
			mu.Lock()
			seenKeys.Insert(key)
			oldObj, existsInOld := oldState[key]
			mu.Unlock()

			if existsInOld {
				// Object exists in both old and new - emit UPDATE instead of ADD
				handler.OnUpdate(oldObj, newObj)
			} else {
				// Object only in new - emit ADD (isInInitialList=false since we're converting events)
				handler.OnAdd(obj, false)
			}
		},
		UpdateFunc: func(oldObj, newObj interface{}) {
			handler.OnUpdate(oldObj, newObj)
		},
		DeleteFunc: func(obj interface{}) {
			handler.OnDelete(obj)
		},
	}
}
