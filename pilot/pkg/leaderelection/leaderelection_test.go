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

	"istio.io/istio/pkg/test/util/retry"
)

const testLock = "test-lock"

func createElection(t *testing.T, name string, expectLeader bool, client kubernetes.Interface,
	fns ...func(stop <-chan struct{})) (*LeaderElection, chan struct{}) {
	t.Helper()
	l := NewLeaderElection("ns", name, testLock, client)
	l.ttl = time.Second
	gotLeader := make(chan struct{})
	l.AddRunFunction(func(stop <-chan struct{}) {
		gotLeader <- struct{}{}
	})
	for _, fn := range fns {
		l.AddRunFunction(fn)
	}
	stop := make(chan struct{})
	go l.Run(stop)

	if expectLeader {
		select {
		case <-gotLeader:
		case <-time.After(time.Second * 15):
			t.Fatal("failed to acquire lease")
		}
	} else {
		select {
		case <-gotLeader:
			t.Fatal("unexpectedly acquired lease")
		case <-time.After(time.Second * 1):
		}
	}
	return l, stop
}

func TestLeaderElection(t *testing.T) {
	client := fake.NewSimpleClientset()
	// First pod becomes the leader
	_, stop := createElection(t, "pod1", true, client)
	// A new pod is not the leader
	_, stop2 := createElection(t, "pod2", false, client)
	// The first pod exists, now the new pod becomes the leader
	close(stop2)
	close(stop)
	_, _ = createElection(t, "pod2", true, client)
}

func TestLeaderElectionConfigMapRemoved(t *testing.T) {
	client := fake.NewSimpleClientset()
	_, stop := createElection(t, "pod1", true, client)
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
	allowRbac := atomic.NewBool(true)
	client.Fake.PrependReactor("update", "*", func(action k8stesting.Action) (bool, runtime.Object, error) {
		if allowRbac.Load() {
			return false, nil, nil
		}
		return true, nil, fmt.Errorf("nope, out of luck")
	})

	completions := atomic.NewInt32(0)
	l, stop := createElection(t, "pod1", true, client, func(stop <-chan struct{}) {
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
	})
}
