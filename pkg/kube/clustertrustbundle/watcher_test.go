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
	"fmt"
	"testing"
	"time"

	"go.uber.org/atomic"
	certificatesv1 "k8s.io/api/certificates/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	klabels "k8s.io/apimachinery/pkg/labels"
	fakediscovery "k8s.io/client-go/discovery/fake"

	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/kube/clustertrustbundle/clustertrustbundletest"
	"istio.io/istio/pkg/kube/kclient/clienttest"
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/util/assert"
)

func fastPoll(t *testing.T) {
	old := PollInterval
	PollInterval = 10 * time.Millisecond
	t.Cleanup(func() { PollInterval = old })
}

func TestAPIWatcherAvailableAtStart(t *testing.T) {
	fastPoll(t)
	c := kube.NewFakeClient()
	clustertrustbundletest.SetAPIAvailable(c)
	w := NewAPIWatcher(c, test.NewStop(t))
	assert.EventuallyEqual(t, w.HasSynced, true)
	assert.Equal(t, w.Available(), true)

	// Already available: callback is not stored, and the caller is told so.
	called := false
	assert.Equal(t, w.KnownOrCallback(func(stop <-chan struct{}) { called = true }), true)
	assert.Equal(t, called, false)
}

func TestAPIWatcherUnavailableAtStart(t *testing.T) {
	// Poll slowly so the callback cannot fire during this test.
	old := PollInterval
	PollInterval = time.Hour
	t.Cleanup(func() { PollInterval = old })

	c := kube.NewFakeClient()
	w := NewAPIWatcher(c, test.NewStop(t))
	// An unavailable API must not block startup.
	assert.EventuallyEqual(t, w.HasSynced, true)
	assert.Equal(t, w.Available(), false)
	called := atomic.NewInt32(0)
	assert.Equal(t, w.KnownOrCallback(func(stop <-chan struct{}) { called.Inc() }), false)
	assert.Equal(t, called.Load(), int32(0))
}

func TestAPIWatcherOtherResourcesInGroupNotEnough(t *testing.T) {
	fastPoll(t)
	c := kube.NewFakeClient()
	// A cluster serving certificates.k8s.io/v1 without clustertrustbundles, as with CSRs only.
	fd := c.Kube().Discovery().(*fakediscovery.FakeDiscovery)
	fd.Resources = append(fd.Resources, &metav1.APIResourceList{
		GroupVersion: GVR.GroupVersion().String(),
		APIResources: []metav1.APIResource{
			{Name: "certificatesigningrequests"},
			{Name: "clustertrustbundles/status"},
		},
	})
	w := NewAPIWatcher(c, test.NewStop(t))
	assert.EventuallyEqual(t, w.HasSynced, true)
	assert.Equal(t, w.Available(), false)
}

func TestAPIWatcherAppearsLater(t *testing.T) {
	fastPoll(t)
	c := kube.NewFakeClient()
	api := clustertrustbundletest.NewAPIToggle(c)
	w := NewAPIWatcher(c, test.NewStop(t))
	assert.EventuallyEqual(t, w.HasSynced, true)

	first := atomic.NewInt32(0)
	second := atomic.NewInt32(0)
	var gotStop <-chan struct{}
	assert.Equal(t, w.KnownOrCallback(func(stop <-chan struct{}) {
		gotStop = stop
		first.Inc()
	}), false)
	assert.Equal(t, w.KnownOrCallback(func(stop <-chan struct{}) { second.Inc() }), false)
	assert.Equal(t, w.Available(), false)

	api.Enable()
	assert.EventuallyEqual(t, w.Available, true)
	assert.EventuallyEqual(t, first.Load, int32(1))
	assert.EventuallyEqual(t, second.Load, int32(1))
	assert.Equal(t, gotStop != nil, true)

	// Callbacks fire exactly once, even though more polling intervals pass.
	time.Sleep(5 * PollInterval)
	assert.Equal(t, first.Load(), int32(1))
	assert.Equal(t, second.Load(), int32(1))
}

func TestAPIWatcherStop(t *testing.T) {
	fastPoll(t)
	c := kube.NewFakeClient()
	api := clustertrustbundletest.NewAPIToggle(c)
	stop := make(chan struct{})
	w := NewAPIWatcher(c, stop)
	assert.EventuallyEqual(t, w.HasSynced, true)
	called := atomic.NewInt32(0)
	w.KnownOrCallback(func(stop <-chan struct{}) { called.Inc() })

	close(stop)
	// Let the goroutine observe the stop; a tick racing with the close is harmless since the API is still absent.
	time.Sleep(3 * PollInterval)
	api.Enable()
	time.Sleep(5 * PollInterval)
	assert.Equal(t, w.Available(), false)
	assert.Equal(t, called.Load(), int32(0))
}

func TestDelayedInformer(t *testing.T) {
	fastPoll(t)
	ctb := &certificatesv1.ClusterTrustBundle{
		ObjectMeta: metav1.ObjectMeta{Name: "my-bundle"},
		Spec:       certificatesv1.ClusterTrustBundleSpec{TrustBundle: "unused"},
	}
	c := kube.NewFakeClient(ctb)
	api := clustertrustbundletest.NewAPIToggle(c)
	stop := test.NewStop(t)
	w := NewAPIWatcher(c, stop)
	inf := NewInformer(c, w)
	c.RunAndWait(stop)

	// Unavailable: empty, but synced so startup is not blocked.
	assert.EventuallyEqual(t, inf.HasSynced, true)
	assert.Equal(t, len(inf.List("", klabels.Everything())), 0)
	assert.Equal(t, inf.Get("my-bundle", "") == nil, true)

	api.Enable()
	assert.EventuallyEqual(t, func() int { return len(inf.List("", klabels.Everything())) }, 1)
	assert.Equal(t, inf.Get("my-bundle", "").Name, "my-bundle")
	assert.Equal(t, inf.HasSynced(), true)

	// Events flow through the real informer once swapped in.
	tracker := assert.NewTracker[string](t)
	inf.AddEventHandler(clienttest.TrackerHandler(tracker))
	tracker.WaitOrdered("add/my-bundle")
	clienttest.NewWriter[*certificatesv1.ClusterTrustBundle](t, c).Create(&certificatesv1.ClusterTrustBundle{
		ObjectMeta: metav1.ObjectMeta{Name: "other"},
		Spec:       certificatesv1.ClusterTrustBundleSpec{TrustBundle: "unused"},
	})
	tracker.WaitOrdered("add/other")
}

func TestDelayedInformerAvailableAtStart(t *testing.T) {
	ctb := &certificatesv1.ClusterTrustBundle{ObjectMeta: metav1.ObjectMeta{Name: "my-bundle"}}
	c := kube.NewFakeClient(ctb)
	clustertrustbundletest.SetAPIAvailable(c)
	stop := test.NewStop(t)
	w := NewAPIWatcher(c, stop)
	assert.EventuallyEqual(t, w.Available, true)
	inf := NewInformer(c, w)
	c.RunAndWait(stop)
	assert.EventuallyEqual(t, func() int { return len(inf.List("", klabels.Everything())) }, 1)
}

func TestAPIWatcherTransientErrorIsNotUnavailable(t *testing.T) {
	test.SetForTest(t, &StartupTimeout, 10*time.Second)
	fastPoll(t)
	c := kube.NewFakeClient()
	clustertrustbundletest.SetAPIAvailable(c)
	failing := atomic.NewInt32(3)
	calls := clustertrustbundletest.InjectDiscoveryErrors(c, func() error {
		if failing.Dec() >= 0 {
			return errors.NewTimeoutError("timeout", 1)
		}
		return nil
	})

	w := NewAPIWatcher(c, test.NewStop(t))
	deadline := time.Now().Add(10 * time.Second)
	for !w.Available() {
		// Synced is only ever set after available here, so synced without available would mean the watcher reported a
		// verdict of "not served" after a transient error.
		if w.HasSynced() && !w.Available() {
			t.Fatal("watcher reported synced as unavailable after a transient discovery error")
		}
		if time.Now().After(deadline) {
			t.Fatal("watcher never became available")
		}
		time.Sleep(time.Millisecond)
	}
	assert.EventuallyEqual(t, w.HasSynced, true)
	assert.Equal(t, calls.Load(), int32(4))
}

func TestAPIWatcherPersistentErrorsGiveUpAfterTimeout(t *testing.T) {
	timeout := 300 * time.Millisecond
	test.SetForTest(t, &StartupTimeout, timeout)
	fastPoll(t)
	c := kube.NewFakeClient()
	clustertrustbundletest.SetAPIAvailable(c)
	failing := atomic.NewBool(true)
	clustertrustbundletest.InjectDiscoveryErrors(c, func() error {
		if failing.Load() {
			return errors.NewInternalError(fmt.Errorf("boom"))
		}
		return nil
	})

	start := time.Now()
	w := NewAPIWatcher(c, test.NewStop(t))
	assert.EventuallyEqual(t, w.HasSynced, true)
	if time.Since(start) < timeout {
		t.Fatalf("watcher gave up after %v, before the %v startup timeout", time.Since(start), timeout)
	}
	assert.Equal(t, w.Available(), false)

	// Once the errors stop, regular polling finds the API.
	failing.Store(false)
	assert.EventuallyEqual(t, w.Available, true)
}

func TestAPIWatcherErrorsAfterSyncKeepPolling(t *testing.T) {
	test.SetForTest(t, &StartupTimeout, time.Hour)
	fastPoll(t)
	c := kube.NewFakeClient()
	// Not served at first, so the first answer is a definitive NotFound.
	api := clustertrustbundletest.NewAPIToggle(c)
	failing := atomic.NewBool(false)
	calls := clustertrustbundletest.InjectDiscoveryErrors(c, func() error {
		if failing.Load() {
			return errors.NewInternalError(fmt.Errorf("boom"))
		}
		return nil
	})
	w := NewAPIWatcher(c, test.NewStop(t))
	assert.EventuallyEqual(t, w.HasSynced, true)
	// Then discovery starts failing. Synced is sticky, and polling continues until the API shows up.
	failing.Store(true)
	before := calls.Load()
	assert.EventuallyEqual(t, func() bool { return calls.Load() > before+2 }, true)
	assert.Equal(t, w.HasSynced(), true)
	assert.Equal(t, w.Available(), false)
	failing.Store(false)
	api.Enable()
	assert.EventuallyEqual(t, w.Available, true)
}

func TestInformerState(t *testing.T) {
	test.SetForTest(t, &InformerSyncTimeout, 100*time.Millisecond)
	fastPoll(t)
	c := kube.NewFakeClient()
	api := clustertrustbundletest.NewAPIToggle(c)
	w := NewAPIWatcher(c, test.NewStop(t))
	synced := atomic.NewBool(false)
	state := func() InformerState { return w.InformerState(synced.Load) }

	// Not served: nothing to wait for yet.
	assert.EventuallyEqual(t, w.HasSynced, true)
	assert.Equal(t, state(), InformerWaiting)

	api.Enable()
	assert.EventuallyEqual(t, w.Available, true)
	// Within the bound the informer is merely not synced yet; after it, the bundles are unreadable.
	assert.EventuallyEqual(t, state, InformerUnreadable)
	// Syncing late recovers.
	synced.Store(true)
	assert.Equal(t, state(), InformerSynced)
}

func TestSharedAPIWatcher(t *testing.T) {
	fastPoll(t)
	registered := func(c kube.Client) bool {
		sharedMu.Lock()
		defer sharedMu.Unlock()
		_, f := shared[c]
		return f
	}
	c1, c2 := kube.NewFakeClient(), kube.NewFakeClient()
	clustertrustbundletest.SetAPIAvailable(c1)
	stop1, stop2 := make(chan struct{}), make(chan struct{})
	w1 := SharedAPIWatcher(c1, stop1)
	assert.Equal(t, SharedAPIWatcher(c1, stop2) == w1, true)
	w2 := SharedAPIWatcher(c2, stop2)
	assert.Equal(t, w2 != w1, true)

	// The watcher that found the API has stopped polling, but must still clean up on stop.
	assert.EventuallyEqual(t, w1.Available, true)
	assert.Equal(t, registered(c1), true)
	assert.Equal(t, registered(c2), true)
	close(stop1)
	assert.EventuallyEqual(t, func() bool { return registered(c1) }, false)
	assert.Equal(t, registered(c2), true)
	// A watcher still polling cleans up too.
	close(stop2)
	assert.EventuallyEqual(t, func() bool { return registered(c2) }, false)

	// After stop a fresh call starts a new watcher.
	stop3 := make(chan struct{})
	w3 := SharedAPIWatcher(c1, stop3)
	assert.Equal(t, w3 != w1, true)
	assert.EventuallyEqual(t, w3.Available, true)
	close(stop3)
	assert.EventuallyEqual(t, func() bool { return registered(c1) }, false)
}
