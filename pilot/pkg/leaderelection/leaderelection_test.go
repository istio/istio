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
	client kubernetes.Interface, fns ...func(stop <-chan struct{}),
) (*LeaderElection, chan struct{}) {
	return createElectionMulticluster(t, name, revision, false, watcher, prioritized, expectLeader, client, fns...)
}

func createElectionMulticluster(t *testing.T,
	name, revision string,
	remote bool,
	watcher revisions.DefaultWatcher,
	prioritized, expectLeader bool,
	client kubernetes.Interface, fns ...func(stop <-chan struct{}),
) (*LeaderElection, chan struct{}) {
	t.Helper()
	l := &LeaderElection{
		namespace:      "ns",
		name:           name,
		electionID:     testLock,
		client:         client,
		revision:       revision,
		remote:         remote,
		prioritized:    prioritized,
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
	_, stop := createElectionMulticluster(t, "pod1", "", true, watcher, false, true, client)
	// A new local pod cannot become leader
	_, stop2 := createElectionMulticluster(t, "pod2", "", false, watcher, false, false, client)
	// A new remote pod cannot become leader
	_, stop3 := createElectionMulticluster(t, "pod3", "", true, watcher, false, false, client)
	close(stop3)
	close(stop2)
	close(stop)
}

func TestPrioritizedMulticlusterLeaderElection(t *testing.T) {
	client := fake.NewSimpleClientset()
	watcher := &fakeDefaultWatcher{defaultRevision: "red"}

	// First pod, revision "green" becomes the remote leader
	_, stop := createElectionMulticluster(t, "pod1", "green", true, watcher, true, true, client)
	// Second pod, revision "red", steals the leader lock from "green" since it is the default revision
	_, stop2 := createElectionMulticluster(t, "pod2", "red", true, watcher, true, true, client)
	// Third pod with revision "red" comes in and can take the lock since it is a local revision "red"
	_, stop3 := createElectionMulticluster(t, "pod3", "red", false, watcher, true, true, client)
	// Fourth pod with revision "red" cannot take the lock since it is remote
	_, stop4 := createElectionMulticluster(t, "pod4", "red", true, watcher, true, false, client)
	close(stop4)
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

type LeaderComparison func(string, *LeaderElection) bool

type instance struct {
	revision string
	remote   bool
	comp     string
}

func (i instance) GetComp() (LeaderComparison, string) {
	key := i.revision
	switch i.comp {
	case "location":
		if i.remote {
			key = remoteIstiodPrefix + key
		}
		return LocationPrioritizedComparison, key
	case "simple":
		return SimpleRevisionComparison, key
	default:
		panic("unknown comparison type")
	}
}

// TestPrioritizationCycles
func TestPrioritizationCycles(t *testing.T) {
	cases := []instance{}
	for _, rev := range []string{"", "default", "not-default"} {
		for _, loc := range []bool{false, true} {
			for _, comp := range []string{"location", "simple"} {
				cases = append(cases, instance{
					revision: rev,
					remote:   loc,
					comp:     comp,
				})
			}
		}
	}

	for _, start := range cases {
		t.Run(fmt.Sprint(start), func(t *testing.T) {
			checkCycles(t, start, cases, nil)
		})
	}
}

func alreadyHit(cur instance, chain []instance) bool {
	for _, cc := range chain {
		if cur == cc {
			return true
		}
	}
	return false
}

func checkCycles(t *testing.T, start instance, cases []instance, chain []instance) {
	if alreadyHit(start, chain) {
		t.Fatalf("cycle on leader election: cur %v, chain %v", start, chain)
	}
	for _, nextHop := range cases {
		next := LeaderElection{
			remote:         nextHop.remote,
			defaultWatcher: &fakeDefaultWatcher{defaultRevision: "default"},
			revision:       nextHop.revision,
		}
		cmpFunc, key := start.GetComp()
		if cmpFunc(key, &next) {
			nc := append([]instance{}, chain...)
			nc = append(nc, start)
			checkCycles(t, nextHop, cases, nc)
		}
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
