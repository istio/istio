//go:build linux
// +build linux

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

package nodeagent

import (
	"reflect"
	"sync/atomic"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func openNsTestOverride(s string) (NetnsCloser, error) {
	return newFakeNs(inc()), nil
}

func openNsTestOverrideWithInodes(inodes ...uint64) func(s string) (NetnsCloser, error) {
	i := 0
	return func(s string) (NetnsCloser, error) {
		inode := inodes[i]
		i++
		return newFakeNsInode(inc(), inode), nil
	}
}

func TestUpsertPodCache(t *testing.T) {
	counter.Store(0)

	p := newPodNetnsCache(openNsTestOverrideWithInodes(1, 1))

	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{UID: "testUID"}}
	nspath1 := "/path/to/netns/1"
	nspath2 := "/path/to/netns/2"

	netns1, err := p.UpsertPodCache(pod, nspath1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	netns2, err := p.UpsertPodCache(pod, nspath2)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !reflect.DeepEqual(netns1, netns2) {
		t.Fatalf("Expected the same Netns for the same uid, got %v and %v", netns1, netns2)
	}

	if counter.Load() != 2 {
		t.Fatalf("Expected openNetns to be called twice, got %d", counter.Load())
	}
}

func TestUpsertPodCacheWithNewInode(t *testing.T) {
	counter.Store(0)

	p := newPodNetnsCache(openNsTestOverrideWithInodes(1, 2))

	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{UID: "testUID"}}
	nspath1 := "/path/to/netns/1"
	nspath2 := "/path/to/netns/2"

	netns1, err := p.UpsertPodCache(pod, nspath1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	netns2, err := p.UpsertPodCache(pod, nspath2)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if reflect.DeepEqual(netns1, netns2) {
		t.Fatalf("Expected the same Netns for the same uid, got %v and %v", netns1, netns2)
	}

	if counter.Load() != 2 {
		t.Fatalf("Expected openNetns to be called twice, got %d", counter.Load())
	}
}

func TestPodsAppearsWithNilNetnsWhenEnsureIsUsed(t *testing.T) {
	p := newPodNetnsCache(openNsTestOverride)

	p.Ensure("123")

	found := false
	snap := p.ReadCurrentPodSnapshot()
	for k, v := range snap {
		if k == "123" && v == (workloadInfo{}) {
			found = true
		}
	}
	if !found {
		t.Fatal("expected pod 123 to be in the cache")
	}
}

func TestUpsertPodCacheWithLiveNetns(t *testing.T) {
	p := newPodNetnsCache(openNsTestOverride)

	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{UID: "testUID"}}
	ns := newFakeNsInode(inc(), 1)
	wl := workloadInfo{
		workload: podToWorkload(pod),
		netns:    ns,
	}
	netns1 := p.UpsertPodCacheWithNetns(string(pod.UID), wl)
	if !reflect.DeepEqual(netns1, ns) {
		t.Fatalf("Expected the same Netns for the same uid, got %v and %v", netns1, ns)
	}

	ns2 := newFakeNsInode(inc(), 1)
	wl2 := workloadInfo{
		workload: podToWorkload(pod),
		netns:    ns2,
	}
	// when using same uid, the original netns should be returned
	netns2 := p.UpsertPodCacheWithNetns(string(pod.UID), wl2)
	if netns2 != ns {
		t.Fatalf("Expected the original Netns for the same uid, got %p and %p", netns2, ns)
	}
	if !ns2.closed.Load() {
		t.Fatal("Expected the second Netns to be closed")
	}
}

func TestDoubleTake(t *testing.T) {
	p := newPodNetnsCache(openNsTestOverride)

	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{UID: "testUID"}}
	ns := newFakeNs(inc())
	wl := workloadInfo{
		workload: podToWorkload(pod),
		netns:    ns,
	}
	netns1 := p.UpsertPodCacheWithNetns(string(pod.UID), wl)
	netnsTaken := p.Take(string(pod.UID))
	if netns1 != netnsTaken {
		t.Fatalf("Expected the original Netns for the same uid, got %p and %p", netns1, ns)
	}
	if nil != p.Take(string(pod.UID)) {
		// expect nil because we already took it
		t.Fatal("Expected nil Netns for the same uid twice")
	}
}

var counter atomic.Int64

func inc() uintptr {
	return uintptr(counter.Add(1))
}
