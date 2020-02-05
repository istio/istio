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
	"fmt"
	"testing"
	"time"

	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"

	"istio.io/istio/pkg/test/util/retry"
)

func createElection(t *testing.T, name string, expectLeader bool, client kubernetes.Interface, fns ...func(stop <-chan struct{})) chan struct{} {
	t.Helper()
	l := NewLeaderElection("ns", name, client)
	l.ttl = time.Second
	gotLeader := make(chan struct{})
	l.AddRunFunction(func(stop <-chan struct{}) {
		gotLeader <- struct{}{}
	})
	for _, fn := range fns {
		l.AddRunFunction(fn)
	}
	stop := make(chan struct{})
	if err := l.Build(); err != nil {
		t.Fatal(err)
	}
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

func TestLeaderElectionConfigMapRemoved(t *testing.T) {
	client := fake.NewSimpleClientset()
	stop := createElection(t, "pod1", true, client)
	if err := client.CoreV1().ConfigMaps("ns").Delete("istio-leader", &v1.DeleteOptions{}); err != nil {
		t.Fatal(err)
	}
	retry.UntilSuccessOrFail(t, func() error {
		l, err := client.CoreV1().ConfigMaps("ns").List(v1.ListOptions{})
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
	completed := make(chan struct{})
	stop := createElection(t, "pod1", true, client, func(stop <-chan struct{}) {
		// Send on "completed" if we haven't been told to stop within 3s
		select {
		case <-stop:
			return
		case <-time.After(time.Second * 3):
		}
		completed <- struct{}{}
	})
	// Immediately drop RBAC permssions to update the configmap
	client.Fake.PrependReactor("update", "*", func(action k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, fmt.Errorf("nope, out of luck")
	})
	// We would expect this to complete
	select {
	case <-time.After(time.Second * 15):
		t.Fatalf("failed to complete function")
	case <-completed:
	}
	close(stop)
}
