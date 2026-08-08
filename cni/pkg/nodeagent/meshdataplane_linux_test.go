// Copyright Istio Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package nodeagent

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/mock"

	set "istio.io/istio/cni/pkg/addressset"
	"istio.io/istio/cni/pkg/config"
	"istio.io/istio/cni/pkg/ipset"
	istiolog "istio.io/istio/pkg/log"
	"istio.io/istio/pkg/test/util/assert"
)

// fakeTrafficRuleManager records the sequence of EnsureHostRules/DeleteHostRules calls
// to verify the behavior of the host rules reconcile loop and the ordering guarantees
// on Stop.
type fakeTrafficRuleManager struct {
	mu     sync.Mutex
	events []string

	ensureCh chan struct{} // signals every EnsureHostRules invocation
	repaired bool
	err      error
}

func (f *fakeTrafficRuleManager) CreateInpodRules(*istiolog.Scope, config.PodLevelOverrides) error {
	return nil
}
func (f *fakeTrafficRuleManager) DeleteInpodRules(*istiolog.Scope) error { return nil }
func (f *fakeTrafficRuleManager) CreateHostRulesForHealthChecks() error  { return nil }
func (f *fakeTrafficRuleManager) ReconcileModeEnabled() bool             { return false }

func (f *fakeTrafficRuleManager) DeleteHostRules() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.events = append(f.events, "delete")
}

func (f *fakeTrafficRuleManager) EnsureHostRules() (bool, error) {
	f.mu.Lock()
	f.events = append(f.events, "ensure")
	f.mu.Unlock()
	select {
	case f.ensureCh <- struct{}{}:
	default:
	}
	return f.repaired, f.err
}

func (f *fakeTrafficRuleManager) snapshotEvents() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]string, len(f.events))
	copy(out, f.events)
	return out
}

func newReconcileTestDataplane(fakeTM *fakeTrafficRuleManager, interval time.Duration) *meshDataplane {
	// Stop(false) goes through hostAddrSet Flush/DestroySet, back them with mocks
	fakeIPSetDeps := ipset.FakeNLDeps()
	fakeIPSetDeps.On("flush", mock.Anything).Return(nil).Maybe()
	fakeIPSetDeps.On("destroySet", mock.Anything).Return(nil).Maybe()
	ipsetInstance := ipset.IPSet{V4Name: "foo-v4", Prefix: "foo", Deps: fakeIPSetDeps}

	return &meshDataplane{
		netServer:                  &fakeServer{},
		hostTrafficManager:         fakeTM,
		hostAddrSet:                set.NewIPSetWrapper(ipsetInstance),
		hostRulesReconcileInterval: interval,
	}
}

func waitForEnsureCalls(t *testing.T, fakeTM *fakeTrafficRuleManager, n int) {
	t.Helper()
	for i := 0; i < n; i++ {
		select {
		case <-fakeTM.ensureCh:
		case <-time.After(5 * time.Second):
			t.Fatalf("reconcile loop did not tick in time (got %d/%d calls)", i, n)
		}
	}
}

func TestHostRulesReconcileLoopTicksAndStops(t *testing.T) {
	fakeTM := &fakeTrafficRuleManager{ensureCh: make(chan struct{}, 64), repaired: true}
	dp := newReconcileTestDataplane(fakeTM, 5*time.Millisecond)

	dp.Start(context.Background())
	// The loop must invoke EnsureHostRules periodically at the configured interval
	waitForEnsureCalls(t, fakeTM, 3)

	dp.Stop(false)

	// Once Stop returns the loop must have exited:
	// 1. no ensure event may appear after the delete (cleanup) event, otherwise the
	//    cleaned-up rules would be re-installed
	events := fakeTM.snapshotEvents()
	deleteSeen := false
	for _, e := range events {
		if e == "delete" {
			deleteSeen = true
		} else if deleteSeen && e == "ensure" {
			t.Fatalf("EnsureHostRules was invoked after DeleteHostRules, cleanup would be undone: %v", events)
		}
	}
	assert.Equal(t, true, deleteSeen)

	// 2. no new ticks are produced
	before := len(fakeTM.snapshotEvents())
	time.Sleep(30 * time.Millisecond)
	assert.Equal(t, before, len(fakeTM.snapshotEvents()))
}

func TestHostRulesReconcileLoopToleratesErrors(t *testing.T) {
	// The loop must not exit when EnsureHostRules keeps failing; it must retry on the
	// next tick
	fakeTM := &fakeTrafficRuleManager{ensureCh: make(chan struct{}, 64), err: errors.New("boom")}
	dp := newReconcileTestDataplane(fakeTM, 5*time.Millisecond)

	dp.Start(context.Background())
	waitForEnsureCalls(t, fakeTM, 3)
	dp.Stop(true)
}

func TestHostRulesReconcileDisabled(t *testing.T) {
	fakeTM := &fakeTrafficRuleManager{ensureCh: make(chan struct{}, 1)}
	dp := newReconcileTestDataplane(fakeTM, 0)

	dp.Start(context.Background())
	// The loop must not be started when interval <= 0
	assert.Equal(t, true, dp.stopHostRulesReconcile == nil)

	time.Sleep(20 * time.Millisecond)
	assert.Equal(t, 0, len(fakeTM.snapshotEvents()))

	// Stop must not hang waiting for a loop that was never started
	dp.Stop(true)
}

func TestHostRulesReconcileStopIsIdempotentWithSkipCleanup(t *testing.T) {
	// With skipCleanup=true (upgrade) the loop must still be stopped first, and
	// DeleteHostRules must not be invoked
	fakeTM := &fakeTrafficRuleManager{ensureCh: make(chan struct{}, 64)}
	dp := newReconcileTestDataplane(fakeTM, 5*time.Millisecond)

	dp.Start(context.Background())
	waitForEnsureCalls(t, fakeTM, 1)
	dp.Stop(true)

	events := fakeTM.snapshotEvents()
	for _, e := range events {
		if e == "delete" {
			t.Fatalf("DeleteHostRules must not be invoked when skipCleanup=true: %v", events)
		}
	}
	before := len(events)
	time.Sleep(30 * time.Millisecond)
	assert.Equal(t, before, len(fakeTM.snapshotEvents()))
}
