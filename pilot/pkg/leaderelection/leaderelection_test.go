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

package leaderelection

import (
	"context"
	"fmt"
	"testing"
	"time"

	"go.uber.org/atomic"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"

	"istio.io/istio/pkg/revisions"
	"istio.io/istio/pkg/test/util/retry"
)

const testLock = "test-lock"

func createElection(t *testing.T,
	name string, revision string,
	watcher revisions.DefaultWatcher,
	prioritized, expectLeader bool,
	client kubernetes.Interface, fns ...func(stop <-chan struct{})) (*LeaderElection, chan struct{}) {
	return createElectionMulticluster(t, name, revision, false, watcher, nil, prioritized, expectLeader, client, fns...)
}

func createElectionMulticluster(t *testing.T,
	name, revision string,
	remote bool,
	watcher revisions.DefaultWatcher,
	cmpFunction LeaderComparison,
	prioritized, expectLeader bool,
	client kubernetes.Interface, fns ...func(stop <-chan struct{})) (*LeaderElection, chan struct{}) {
	t.Helper()
	l := &LeaderElection{
		namespace:      "ns",
		name:           name,
		electionID:     testLock,
		client:         client,
		revision:       revision,
		remote:         remote,
		prioritized:    prioritized,
		cmpFunction:    cmpFunction,
		defaultWatcher: watcher,
		ttl:            time.Second,
		cycle:          atomic.NewInt32(0),
	}
	gotLeader := make(chan struct{})
	l.AddRunFunction(func(stop <-chan struct{}) {
		gotLeader <- struct{}{}
	})
	for _, fn := range fns {
		l.AddRunFunction(fn)
	}
	stop := make(chan struct{})
	go l.Run(stop)

	retry.UntilOrFail(t, func() bool {
		return l.isLeader() == expectLeader
	}, retry.Converge(5), retry.Delay(time.Millisecond*100), retry.Timeout(time.Second*10))
	return l, stop
}

type fakeDefaultWatcher struct {
	defaultRevision string
}

func (w *fakeDefaultWatcher) setDefaultRevision(r string) {
	w.defaultRevision = r
}

func (w *fakeDefaultWatcher) Run(stop <-chan struct{}) {
}

func (w *fakeDefaultWatcher) HasSynced() bool {
	return true
}

func (w *fakeDefaultWatcher) GetDefault() string {
	return w.defaultRevision
}

func (w *fakeDefaultWatcher) AddHandler(handler revisions.DefaultHandler) {
	panic("unimplemented")
}

func TestLeaderElection(t *testing.T) {
	client := fake.NewSimpleClientset()
	watcher := &fakeDefaultWatcher{}
	// First pod becomes the leader
	_, stop := createElection(t, "pod1", "", watcher, true, true, client)
	// A new pod is not the leader
	_, stop2 := createElection(t, "pod2", "", watcher, true, false, client)
	close(stop2)
	close(stop)
}

func TestPrioritizedLeaderElection(t *testing.T) {
	client := fake.NewSimpleClientset()
	watcher := &fakeDefaultWatcher{defaultRevision: "red"}

	// First pod, revision "green" becomes the leader, but is not the default revision
	_, stop := createElection(t, "pod1", "green", watcher, true, true, client)
	// Second pod, revision "red", steals the leader lock from "green" since it is the default revision
	_, stop2 := createElection(t, "pod2", "red", watcher, true, true, client)
	// Third pod with revision "red" comes in and cannot take the lock since another revision with "red" has it
	_, stop3 := createElection(t, "pod3", "red", watcher, true, false, client)
	// Fourth pod with revision "green" cannot take the lock since a revision with "red" has it.
	_, stop4 := createElection(t, "pod4", "green", watcher, true, false, client)
	close(stop2)
	close(stop3)
	close(stop4)
	// Now that revision "green" has stopped acting as leader, revision "red" should be able to claim lock.
	_, stop5 := createElection(t, "pod2", "red", watcher, true, true, client)
	close(stop5)
	close(stop)
	// Revision "green" can reclaim once "red" releases.
	_, stop6 := createElection(t, "pod4", "green", watcher, true, true, client)
	// Test that "red" doesn't steal lock if "prioritized" is disabled
	_, stop7 := createElection(t, "pod5", "red", watcher, false, false, client)
	close(stop6)
	close(stop7)
}

func TestMulticlusterLeaderElection(t *testing.T) {
	client := fake.NewSimpleClientset()
	watcher := &fakeDefaultWatcher{}
	// First remote pod becomes the leader
	_, stop := createElectionMulticluster(t, "pod1", "", true, watcher, nil, false, true, client)
	// A new local pod cannot become leader
	_, stop2 := createElectionMulticluster(t, "pod2", "", false, watcher, nil, false, false, client)
	// A new remote pod cannot become leader
	_, stop3 := createElectionMulticluster(t, "pod3", "", true, watcher, nil, false, false, client)
	close(stop3)
	close(stop2)
	close(stop)
}

func SimpleRevisionComparison(currentLeaderRevision string, l *LeaderElection) bool {
	// Old key comparison impl for interoperablilty testing
	defaultRevision := l.defaultWatcher.GetDefault()
	return l.revision != currentLeaderRevision &&
		// empty default revision indicates that there is no default set
		defaultRevision != "" && defaultRevision == l.revision
}

func TestPrioritizedMulticlusterLeaderElection(t *testing.T) {
	// This test runs pairs of leaders in all combinations of remote/local, default/non-default revisions,
	// default/default revisions, non-default/non-default revisions, and tests new/old cmp function interoperablilty.
	// The tests confirm the election results in the expected leader in all cases and that and that lock steal-back
	// never happens after a leader changes.
	const (
		remote bool = true
		local  bool = false
	)
	client := fake.NewSimpleClientset()
	watcher := &fakeDefaultWatcher{defaultRevision: "def"}

	leaderTests := []struct {
		revision1   string           // revision of first leader pod
		remote1     bool             // true if first leader is remote
		revision2   string           // revision of second leader pod
		remote2     bool             // true second leader is remote
		cmpFunction LeaderComparison // leader comparison function for second leader
		shouldSteal bool             // true if second leader should steal from first
	}{
		{"def", local, "def", local, LocationPrioritizedComparison, false},
		{"def", local, "def", local, SimpleRevisionComparison, false},
		{"def", local, "def", remote, LocationPrioritizedComparison, false},
		{"def", local, "def", remote, SimpleRevisionComparison, false},
		{"def", local, "ndef1", local, LocationPrioritizedComparison, false},
		{"def", local, "ndef1", local, SimpleRevisionComparison, false},
		{"def", local, "ndef1", remote, LocationPrioritizedComparison, false},
		{"def", local, "ndef1", remote, SimpleRevisionComparison, false},
		{"def", local, "", local, nil, false},
		{"def", local, "", remote, nil, false},

		{"def", remote, "def", local, LocationPrioritizedComparison, true},
		{"def", remote, "def", local, SimpleRevisionComparison, true},
		{"def", remote, "def", remote, LocationPrioritizedComparison, false},
		{"def", remote, "def", remote, SimpleRevisionComparison, true}, // Old code doesn't recognize new remote key
		{"def", remote, "ndef1", local, LocationPrioritizedComparison, false},
		{"def", remote, "ndef1", local, SimpleRevisionComparison, false},
		{"def", remote, "ndef1", remote, LocationPrioritizedComparison, false},
		{"def", remote, "ndef1", remote, SimpleRevisionComparison, false},
		{"def", remote, "", local, nil, false},
		{"def", remote, "", remote, nil, false},

		{"ndef1", local, "def", local, LocationPrioritizedComparison, true},
		{"ndef1", local, "def", local, SimpleRevisionComparison, true},
		{"ndef1", local, "def", remote, LocationPrioritizedComparison, true},
		{"ndef1", local, "def", remote, SimpleRevisionComparison, true},
		{"ndef1", local, "ndef1", local, LocationPrioritizedComparison, false},
		{"ndef1", local, "ndef1", local, SimpleRevisionComparison, false},
		{"ndef1", local, "ndef1", remote, LocationPrioritizedComparison, false},
		{"ndef1", local, "ndef1", remote, SimpleRevisionComparison, false},
		{"ndef1", local, "ndef2", local, LocationPrioritizedComparison, false},
		{"ndef1", local, "ndef2", local, SimpleRevisionComparison, false},
		{"ndef1", local, "ndef2", remote, LocationPrioritizedComparison, false},
		{"ndef1", local, "ndef2", remote, SimpleRevisionComparison, false},
		{"ndef1", local, "", local, nil, false},
		{"ndef1", local, "", remote, nil, false},

		{"ndef1", remote, "def", local, LocationPrioritizedComparison, true},
		{"ndef1", remote, "def", local, SimpleRevisionComparison, true},
		{"ndef1", remote, "def", remote, LocationPrioritizedComparison, true},
		{"ndef1", remote, "def", remote, SimpleRevisionComparison, true},
		{"ndef1", remote, "ndef1", local, LocationPrioritizedComparison, true},
		{"ndef1", remote, "ndef1", local, SimpleRevisionComparison, false}, // Old code doesn't prioritize location
		{"ndef1", remote, "ndef1", remote, LocationPrioritizedComparison, false},
		{"ndef1", remote, "ndef1", remote, SimpleRevisionComparison, false},
		{"ndef1", remote, "ndef2", local, LocationPrioritizedComparison, false},
		{"ndef1", remote, "ndef2", local, SimpleRevisionComparison, false},
		{"ndef1", remote, "ndef2", remote, LocationPrioritizedComparison, false},
		{"ndef1", remote, "ndef2", remote, SimpleRevisionComparison, false},
		{"ndef1", remote, "", local, nil, false},
		{"ndef1", remote, "", remote, nil, false},
	}

	for _, test := range leaderTests {
		_, stop := createElectionMulticluster(t, "pod1", test.revision1, test.remote1, watcher, nil, true, true, client)
		_, stop2 := createElectionMulticluster(t, "pod2", test.revision2, test.remote2, watcher, test.cmpFunction, test.cmpFunction != nil, test.shouldSteal, client)
		if test.shouldSteal {
			// Make sure there's no steal-back
			_, stop3 := createElectionMulticluster(t, "pod1", test.revision1, test.remote1, watcher, nil, true, false, client)
			close(stop3)
		}
		close(stop2)
		close(stop)
	}
}

func TestLeaderElectionConfigMapRemoved(t *testing.T) {
	client := fake.NewSimpleClientset()
	watcher := &fakeDefaultWatcher{}
	_, stop := createElection(t, "pod1", "", watcher, true, true, client)
	if err := client.CoreV1().ConfigMaps("ns").Delete(context.TODO(), testLock, v1.DeleteOptions{}); err != nil {
		t.Fatal(err)
	}
	retry.UntilSuccessOrFail(t, func() error {
		l, err := client.CoreV1().ConfigMaps("ns").List(context.TODO(), v1.ListOptions{})
		if err != nil {
			return err
		}
		if len(l.Items) != 1 {
			return fmt.Errorf("got unexpected config map entry: %v", l.Items)
		}
		return nil
	})
	close(stop)
}

func TestLeaderElectionNoPermission(t *testing.T) {
	client := fake.NewSimpleClientset()
	watcher := &fakeDefaultWatcher{}
	allowRbac := atomic.NewBool(true)
	client.Fake.PrependReactor("update", "*", func(action k8stesting.Action) (bool, runtime.Object, error) {
		if allowRbac.Load() {
			return false, nil, nil
		}
		return true, nil, fmt.Errorf("nope, out of luck")
	})

	completions := atomic.NewInt32(0)
	l, stop := createElection(t, "pod1", "", watcher, true, true, client, func(stop <-chan struct{}) {
		completions.Add(1)
	})
	// Expect to run once
	expectInt(t, completions.Load, 1)

	// drop RBAC permssions to update the configmap
	// This simulates loosing an active lease
	allowRbac.Store(false)

	// We should start a new cycle at this point
	expectInt(t, l.cycle.Load, 2)

	// Add configmap permission back
	allowRbac.Store(true)

	// We should get the leader lock back
	expectInt(t, completions.Load, 2)

	close(stop)
}

func expectInt(t *testing.T, f func() int32, expected int32) {
	t.Helper()
	retry.UntilSuccessOrFail(t, func() error {
		got := f()
		if got != expected {
			return fmt.Errorf("unexpected count: %v, want %v", got, expected)
		}
		return nil
	}, retry.Timeout(time.Second))
}
