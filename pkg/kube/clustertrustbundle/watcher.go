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

package clustertrustbundle

import (
	"sync"
	"time"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/client-go/discovery"

	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/kube/kubetypes"
	"istio.io/istio/pkg/log"
)

var watcherLog = log.RegisterScope("clustertrustbundle", "ClusterTrustBundle API availability")

// PollInterval is how often discovery is probed while the API is not served. Overridable in tests.
var PollInterval = time.Minute

// StartupTimeout bounds startup retries for inconclusive discovery errors before falling back to regular polling.
// 30s covers an API server restarting or throttling while istiod starts.
var StartupTimeout = 30 * time.Second

// InformerSyncTimeout prevents missing list/watch permissions from blocking readiness and unrelated reconciliation.
// Listing a small cluster-scoped resource takes milliseconds; a false positive recovers once the informer syncs.
var InformerSyncTimeout = 15 * time.Second

const (
	startupInitialBackoff = 100 * time.Millisecond
	startupMaxBackoff     = 5 * time.Second
)

var _ kubetypes.DelayedFilter = &APIWatcher{}

// InformerState describes whether the bundles behind a ClusterTrustBundle informer can be relied upon.
type InformerState int

const (
	// InformerWaiting means discovery or informer sync is still pending.
	InformerWaiting InformerState = iota
	// InformerSynced means the informer has synced.
	InformerSynced
	// InformerUnreadable means the API is served but the informer did not sync within InformerSyncTimeout, typically
	// because istiod is not allowed to list and watch clustertrustbundles.
	InformerUnreadable
)

// APIWatcher discovers the v1 ClusterTrustBundle resource, not just its group (which also serves CSRs).
// Only NotFound or a successful response without the resource establishes absence; other errors are retried.
// Availability is one-way: polling stops once the API is found, leaving recovery from later outages to the informer.
type APIWatcher struct {
	client   kube.Client
	stop     <-chan struct{}
	interval time.Duration
	onStop   func()

	mu             sync.Mutex
	synced         bool
	available      bool
	availableSince time.Time
	callbacks      []func(stop <-chan struct{})
}

// NewAPIWatcher probes discovery immediately and then every PollInterval until the API is found or stop is closed.
// Prefer SharedAPIWatcher when more than one component of a process needs the answer for the same client.
func NewAPIWatcher(client kube.Client, stop <-chan struct{}) *APIWatcher {
	return newAPIWatcher(client, stop, nil)
}

func newAPIWatcher(client kube.Client, stop <-chan struct{}, onStop func()) *APIWatcher {
	w := &APIWatcher{client: client, stop: stop, interval: PollInterval, onStop: onStop}
	go w.run()
	return w
}

var (
	sharedMu sync.Mutex
	shared   = map[kube.Client]*APIWatcher{}
)

// SharedAPIWatcher gives SDS and Gateway translation one discovery result per client.
// The first caller's stop owns the watcher; closing it removes the registry entry.
func SharedAPIWatcher(client kube.Client, stop <-chan struct{}) *APIWatcher {
	sharedMu.Lock()
	defer sharedMu.Unlock()
	if w, f := shared[client]; f {
		select {
		case <-w.stop:
			// Stopped, but its cleanup has not yet run. Do not hand out a dead watcher.
		default:
			return w
		}
	}
	var w *APIWatcher
	w = newAPIWatcher(client, stop, func() {
		sharedMu.Lock()
		defer sharedMu.Unlock()
		if shared[client] == w {
			delete(shared, client)
		}
	})
	shared[client] = w
	return w
}

// HasSynced reports a definitive discovery answer or expiration of StartupTimeout.
func (w *APIWatcher) HasSynced() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.synced
}

// Available returns true if the API has been detected.
func (w *APIWatcher) Available() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.available
}

// InformerState classifies an informer created for this watcher, given its HasSynced.
func (w *APIWatcher) InformerState(hasSynced func() bool) InformerState {
	w.mu.Lock()
	available, since := w.available, w.availableSince
	w.mu.Unlock()
	if !available {
		return InformerWaiting
	}
	if hasSynced() {
		return InformerSynced
	}
	if time.Since(since) >= InformerSyncTimeout {
		return InformerUnreadable
	}
	return InformerWaiting
}

// KnownOrCallback returns true if the API is already available. Otherwise it registers f to be called once, with the
// watcher's stop channel, when the API is detected, and returns false. Any number of callbacks may be registered.
func (w *APIWatcher) KnownOrCallback(f func(stop <-chan struct{})) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.available {
		return true
	}
	w.callbacks = append(w.callbacks, f)
	return false
}

func (w *APIWatcher) run() {
	w.poll()
	if w.onStop != nil {
		// Polling may have ended because the API was found; cleanup still has to wait for stop.
		<-w.stop
		w.onStop()
	}
}

func (w *APIWatcher) poll() {
	start := time.Now()
	backoff := startupInitialBackoff
	synced := false
	markSynced := func() {
		synced = true
		w.mu.Lock()
		w.synced = true
		w.mu.Unlock()
	}
	for {
		delay := w.interval
		switch probeDiscovery(w.client.Kube().Discovery()) {
		case probeServed:
			w.markAvailable()
			return
		case probeNotServed:
			if !synced {
				markSynced()
			}
		case probeUnknown:
			if !synced {
				remaining := StartupTimeout - time.Since(start)
				if remaining <= 0 {
					watcherLog.Warnf("could not determine whether %v is served within %v; assuming it is not and continuing to poll",
						GVR.GroupResource(), StartupTimeout)
					markSynced()
				} else {
					delay = min(backoff, remaining)
					backoff = min(backoff*2, startupMaxBackoff)
				}
			}
		}
		t := time.NewTimer(delay)
		select {
		case <-w.stop:
			t.Stop()
			return
		case <-t.C:
		}
	}
}

func (w *APIWatcher) markAvailable() {
	w.mu.Lock()
	// Flipping available and draining callbacks under the same lock means a concurrent KnownOrCallback either
	// sees available==true or has its callback included here.
	w.available = true
	w.availableSince = time.Now()
	callbacks := w.callbacks
	w.callbacks = nil
	w.mu.Unlock()

	watcherLog.Infof("%v is available", GVR.GroupResource())
	for _, cb := range callbacks {
		cb(w.stop)
	}

	// Only report synced after callbacks ran, so the delayed informer has been swapped in by the time we say so.
	w.mu.Lock()
	w.synced = true
	w.mu.Unlock()
}

type probeResult int

const (
	// probeUnknown is any failure that does not prove the API is absent.
	probeUnknown probeResult = iota
	probeNotServed
	probeServed
)

func probeDiscovery(d discovery.DiscoveryInterface) probeResult {
	list, err := d.ServerResourcesForGroupVersion(GVR.GroupVersion().String())
	if err != nil {
		if errors.IsNotFound(err) {
			watcherLog.Debugf("%v is not served by the cluster", GVR.GroupResource())
			return probeNotServed
		}
		watcherLog.Warnf("failed to discover %v, will retry: %v", GVR.GroupResource(), err)
		return probeUnknown
	}
	for _, r := range list.APIResources {
		if r.Name == GVR.Resource {
			return probeServed
		}
	}
	return probeNotServed
}
