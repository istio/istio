// Copyright 2020 Istio Authors
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
	"testing"
	"time"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"
)

func createElection(t *testing.T, name string, expectLeader bool, client kubernetes.Interface) chan struct{} {
	t.Helper()
	l := NewLeaderElection("ns", name, client)
	gotLeader := make(chan struct{})
	l.AddRunFunction(func(stop <-chan struct{}) {
		gotLeader <- struct{}{}
	})
	stop := make(chan struct{})
	if err := l.Run(stop); err != nil {
		t.Fatal(err)
	}

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
	return stop
}

func TestLeaderElection(t *testing.T) {
	client := fake.NewSimpleClientset()
	// First pod becomes the leader
	stop := createElection(t, "pod1", true, client)
	// A new pod is not the leader
	stop2 := createElection(t, "pod2", false, client)
	// The first pod exists, now the new pod becomes the leader
	close(stop2)
	close(stop)
	_ = createElection(t, "pod2", true, client)
}
