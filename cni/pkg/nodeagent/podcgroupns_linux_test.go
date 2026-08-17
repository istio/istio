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
	"errors"
	"net"
	"net/netip"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	"istio.io/istio/pkg/slices"
	"istio.io/istio/pkg/test/util/assert"
)

func TestWithProcFs(t *testing.T) {
	n, err := NewPodNetnsProcFinder(fakeFs(true))
	assert.NoError(t, err)
	// the fake fs's netns fds can't be entered; ownership checks are tested separately
	n.netnsPodIPChecker = func(Netns, []netip.Addr) (bool, error) { return true, nil }

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "foo",
			Namespace: "bar",
			UID:       types.UID("863b91d4-4b68-4efa-917f-4b560e3e86aa"),
		},
		Status: corev1.PodStatus{
			PodIP:  "10.0.0.42",
			PodIPs: []corev1.PodIP{{IP: "10.0.0.42"}},
		},
	}
	podUIDNetns, err := n.FindNetnsForPods(map[types.UID]*corev1.Pod{
		pod.UID: pod,
	})
	if err != nil {
		panic(err)
	}
	defer podUIDNetns.Close()

	if len(podUIDNetns) == 0 {
		t.Fatal("expected to find pod netns")
	}

	expectedUID := "863b91d4-4b68-4efa-917f-4b560e3e86aa"
	if podUIDNetns[expectedUID] == (WorkloadInfo{}) {
		t.Fatal("expected to find pod netns under pod uid")
	}

	foundStart := podUIDNetns[expectedUID].Netns.OwnerProcStarttime()
	// See testdata/cgroupns/1/stat
	if foundStart != 70298968 {
		t.Fatalf("didn't find expected starttime, found %d", foundStart)
	}
}

// The fake procfs holds three processes of the same pod, each in its own netns
// (testdata/cgroupns/{0,1,2}), scanned in that order with starttimes 70298999, 70298968,
// 70298977. Proc 1 is the oldest and wins: proc 0's entry is replaced by it, and proc 2
// loses to it. Both losing candidates' netns fds must be closed by the scan itself; only
// the winner's stays open until the returned result is closed.
func TestFindNetnsForPodsClosesLosingCandidates(t *testing.T) {
	ffs := fakeFs(true)
	n, err := NewPodNetnsProcFinder(ffs)
	assert.NoError(t, err)
	n.netnsPodIPChecker = func(Netns, []netip.Addr) (bool, error) { return true, nil }

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "foo",
			Namespace: "bar",
			UID:       types.UID("863b91d4-4b68-4efa-917f-4b560e3e86aa"),
		},
		Status: corev1.PodStatus{
			PodIP:  "10.0.0.42",
			PodIPs: []corev1.PodIP{{IP: "10.0.0.42"}},
		},
	}
	podUIDNetns, err := n.FindNetnsForPods(map[types.UID]*corev1.Pod{
		pod.UID: pod,
	})
	assert.NoError(t, err)

	wl, ok := podUIDNetns[string(pod.UID)]
	if !ok {
		t.Fatal("expected pod to be paired with a netns")
	}
	assert.Equal(t, wl.Netns.OwnerProcStarttime(), uint64(70298968))
	assert.Equal(t, ffs.openNetnsFiles(), 1)

	podUIDNetns.Close()
	assert.Equal(t, ffs.openNetnsFiles(), 0)
}

// A process whose cgroup ties it to pod A but which sits in pod B's netns (procs 2 and 3
// share an inode here) must not pair A with that netns — and the rejected pairing must not
// stop pod B's own process, scanned later, from claiming it. Pod A's own netns (procs 0
// and 1) is validated and paired independently: the foreign candidate loses without
// poisoning either pod's real pairing.
func TestProcScanRejectsForeignNetns(t *testing.T) {
	const sharedIno = 4242
	podA := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "foo",
			Namespace: "bar",
			UID:       types.UID("863b91d4-4b68-4efa-917f-4b560e3e86aa"),
		},
		Status: corev1.PodStatus{PodIPs: []corev1.PodIP{{IP: "10.0.0.1"}}},
	}
	podB := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "baz",
			Namespace: "bar",
			// see testdata/cgroupns/3/cgroup
			UID: types.UID("aaaabbbb-cccc-dddd-eeee-ffff00001111"),
		},
		Status: corev1.PodStatus{PodIPs: []corev1.PodIP{{IP: "10.0.0.2"}}},
	}

	ffs := fakeFsWithNetnsInos(map[string]int{
		"2/ns/net": sharedIno, // pod A's process transiting pod B's netns
		"3/ns/net": sharedIno, // pod B's own process
	})
	n, err := NewPodNetnsProcFinder(ffs)
	assert.NoError(t, err)

	aIP := netip.MustParseAddr("10.0.0.1")
	bIP := netip.MustParseAddr("10.0.0.2")
	foreignRejected := false
	n.netnsPodIPChecker = func(ns Netns, podIPs []netip.Addr) (bool, error) {
		// the shared netns belongs to pod B; every other netns in the fake fs is pod A's
		if ns.Inode() == sharedIno {
			owned := slices.Contains(podIPs, bIP)
			if !owned {
				foreignRejected = true
			}
			return owned, nil
		}
		return slices.Contains(podIPs, aIP), nil
	}

	podUIDNetns, err := n.FindNetnsForPods(map[types.UID]*corev1.Pod{
		podA.UID: podA,
		podB.UID: podB,
	})
	assert.NoError(t, err)

	// pod A reached the ownership check for pod B's netns and was refused
	assert.Equal(t, foreignRejected, true)
	assert.Equal(t, len(podUIDNetns), 2)
	// pod A keeps its own oldest netns (testdata/cgroupns/1), not pod B's
	a, ok := podUIDNetns[string(podA.UID)]
	if !ok {
		t.Fatal("expected pod A to be paired with its own netns")
	}
	assert.Equal(t, a.Netns.Inode() != uint64(sharedIno), true)
	assert.Equal(t, a.Netns.OwnerProcStarttime(), uint64(70298968))
	// pod B claims the shared netns even though the foreign process was scanned first
	b, ok := podUIDNetns[string(podB.UID)]
	if !ok {
		t.Fatal("expected pod B to be paired with its netns")
	}
	assert.Equal(t, b.Netns.Inode(), uint64(sharedIno))
	// losing and rejected candidates' netns fds were closed by the scan
	assert.Equal(t, ffs.openNetnsFiles(), 2)
	podUIDNetns.Close()
	assert.Equal(t, ffs.openNetnsFiles(), 0)
}

// A pod with no IPs yet cannot be ownership-verified: the scan must not enroll it, and
// with nothing to match it should skip entering the netns (a no-match there would log a
// misleading foreign-process warning). It will enroll normally later, via CNI ADD or a rescan.
func TestProcScanDefersPodWithoutIPs(t *testing.T) {
	ffs := fakeFs(true)
	n, err := NewPodNetnsProcFinder(ffs)
	assert.NoError(t, err)
	checkerCalls := 0
	n.netnsPodIPChecker = func(Netns, []netip.Addr) (bool, error) { checkerCalls++; return true, nil }

	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{
		Name:      "foo",
		Namespace: "bar",
		UID:       types.UID("863b91d4-4b68-4efa-917f-4b560e3e86aa"),
	}}
	podUIDNetns, err := n.FindNetnsForPods(map[types.UID]*corev1.Pod{pod.UID: pod})
	assert.NoError(t, err)
	assert.Equal(t, len(podUIDNetns), 0)
	assert.Equal(t, checkerCalls, 0)
	assert.Equal(t, ffs.openNetnsFiles(), 0)
}

// A dual-stack pod is accepted when any one of its IPs is found — here only the v6 one.
func TestProcScanAcceptsDualStackPodByAnyIP(t *testing.T) {
	ffs := fakeFs(true)
	n, err := NewPodNetnsProcFinder(ffs)
	assert.NoError(t, err)
	v6 := netip.MustParseAddr("fd00::42")
	sawIPs := 0
	n.netnsPodIPChecker = func(ns Netns, podIPs []netip.Addr) (bool, error) {
		sawIPs = len(podIPs)
		// the netns carries only the pod's v6 address
		return slices.Contains(podIPs, v6), nil
	}

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "foo",
			Namespace: "bar",
			UID:       types.UID("863b91d4-4b68-4efa-917f-4b560e3e86aa"),
		},
		Status: corev1.PodStatus{
			PodIP:  "10.0.0.42",
			PodIPs: []corev1.PodIP{{IP: "10.0.0.42"}, {IP: "fd00::42"}},
		},
	}
	podUIDNetns, err := n.FindNetnsForPods(map[types.UID]*corev1.Pod{pod.UID: pod})
	assert.NoError(t, err)
	defer podUIDNetns.Close()
	assert.Equal(t, len(podUIDNetns), 1)
	// both IPs were offered to the check in a single call
	assert.Equal(t, sawIPs, 2)
}

// An ownership check that fails outright (vs. returning no-match) must also reject the
// pairing: fail closed.
func TestProcScanRejectsWhenOwnershipCheckErrs(t *testing.T) {
	ffs := fakeFs(true)
	n, err := NewPodNetnsProcFinder(ffs)
	assert.NoError(t, err)
	n.netnsPodIPChecker = func(Netns, []netip.Addr) (bool, error) { return false, errors.New("bad netns") }

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "foo",
			Namespace: "bar",
			UID:       types.UID("863b91d4-4b68-4efa-917f-4b560e3e86aa"),
		},
		Status: corev1.PodStatus{PodIPs: []corev1.PodIP{{IP: "10.0.0.42"}}},
	}
	podUIDNetns, err := n.FindNetnsForPods(map[types.UID]*corev1.Pod{pod.UID: pod})
	assert.NoError(t, err)
	assert.Equal(t, len(podUIDNetns), 0)
	assert.Equal(t, ffs.openNetnsFiles(), 0)
}

func TestInterfaceAddrsContainAny(t *testing.T) {
	ips := func(ss ...string) []netip.Addr {
		out := make([]netip.Addr, 0, len(ss))
		for _, s := range ss {
			out = append(out, netip.MustParseAddr(s))
		}
		return out
	}
	for _, tt := range []struct {
		name   string
		addrs  []net.Addr
		podIPs []netip.Addr
		want   bool
	}{
		{
			name:   "v4 match",
			addrs:  []net.Addr{&net.IPNet{IP: net.IPv4(10, 0, 0, 5).To4()}},
			podIPs: ips("10.0.0.5"),
			want:   true,
		},
		{
			name: "v4-mapped-in-v6 interface addr matches v4 pod IP",
			// net.ParseIP returns the 16-byte mapped form for v4 addresses
			addrs:  []net.Addr{&net.IPNet{IP: net.ParseIP("10.0.0.5")}},
			podIPs: ips("10.0.0.5"),
			want:   true,
		},
		{
			name:   "v6 match",
			addrs:  []net.Addr{&net.IPNet{IP: net.ParseIP("fd00::42")}},
			podIPs: ips("fd00::42"),
			want:   true,
		},
		{
			name:   "any pod IP suffices",
			addrs:  []net.Addr{&net.IPNet{IP: net.ParseIP("fd00::42")}},
			podIPs: ips("10.0.0.5", "fd00::42"),
			want:   true,
		},
		{
			name:   "no match",
			addrs:  []net.Addr{&net.IPNet{IP: net.ParseIP("10.0.0.6")}},
			podIPs: ips("10.0.0.5"),
			want:   false,
		},
		{
			name:   "non-IPNet addrs ignored",
			addrs:  []net.Addr{&net.TCPAddr{IP: net.ParseIP("10.0.0.5")}},
			podIPs: ips("10.0.0.5"),
			want:   false,
		},
		{
			name:   "no interface addrs",
			addrs:  nil,
			podIPs: ips("10.0.0.5"),
			want:   false,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, interfaceAddrsContainAny(tt.addrs, tt.podIPs), tt.want)
		})
	}
}

func TestHostNetnsWithSameIno(t *testing.T) {
	n, err := NewPodNetnsProcFinder(fakeFs(false))
	assert.NoError(t, err)

	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{
		Name:      "foo",
		Namespace: "bar",
		UID:       types.UID("863b91d4-4b68-4efa-917f-4b560e3e86aa"),
	}}
	podUIDNetns, err := n.FindNetnsForPods(map[types.UID]*corev1.Pod{
		pod.UID: pod,
	})
	if err != nil {
		panic(err)
	}
	defer podUIDNetns.Close()

	if len(podUIDNetns) != 0 {
		t.Fatal("expected to find no pod netns")
	}
}

// copied and modified from spire

func TestGetContainerIDFromCGroups(t *testing.T) {
	makeCGroups := func(groupPaths []string) []Cgroup {
		var out []Cgroup
		for _, groupPath := range groupPaths {
			out = append(out, Cgroup{
				GroupPath: groupPath,
			})
		}
		return out
	}

	//nolint: lll
	for _, tt := range []struct {
		name              string
		cgroupPaths       []string
		expectPodUID      types.UID
		expectContainerID string
		expectMsg         string
	}{
		{
			name:              "no cgroups",
			cgroupPaths:       []string{},
			expectPodUID:      "",
			expectContainerID: "",
		},
		{
			name: "no container ID in cgroups",
			cgroupPaths: []string{
				"/user.slice",
			},
			expectPodUID:      "",
			expectContainerID: "",
		},
		{
			name: "one container ID in cgroups",
			cgroupPaths: []string{
				"/user.slice",
				"/kubepods/pod2c48913c-b29f-11e7-9350-020968147796/9bca8d63d5fa610783847915bcff0ecac1273e5b4bed3f6fa1b07350e0135961",
			},
			expectPodUID:      "2c48913c-b29f-11e7-9350-020968147796",
			expectContainerID: "9bca8d63d5fa610783847915bcff0ecac1273e5b4bed3f6fa1b07350e0135961",
		},
		{
			name: "pod UID canonicalized",
			cgroupPaths: []string{
				"/user.slice",
				"/kubepods/pod2c48913c_b29f_11e7_9350_020968147796/9bca8d63d5fa610783847915bcff0ecac1273e5b4bed3f6fa1b07350e0135961",
			},
			expectPodUID:      "2c48913c-b29f-11e7-9350-020968147796",
			expectContainerID: "9bca8d63d5fa610783847915bcff0ecac1273e5b4bed3f6fa1b07350e0135961",
		},
		{
			name: "cri-o",
			cgroupPaths: []string{
				"0::/../crio-45490e76e0878aaa4d9808f7d2eefba37f093c3efbba9838b6d8ab804d9bd814.scope",
			},
			expectPodUID:      "",
			expectContainerID: "45490e76e0878aaa4d9808f7d2eefba37f093c3efbba9838b6d8ab804d9bd814",
		},
		{
			name: "more than one container ID in cgroups",
			cgroupPaths: []string{
				"/user.slice",
				"/kubepods/pod2c48913c-b29f-11e7-9350-020968147796/9bca8d63d5fa610783847915bcff0ecac1273e5b4bed3f6fa1b07350e0135961",
				"/kubepods/kubepods/besteffort/pod2c48913c-b29f-11e7-9350-020968147796/a55d9ac3b312d8a2627824b6d6dd8af66fbec439bf4e0ec22d6d9945ad337a38",
			},
			expectPodUID:      "",
			expectContainerID: "",
			expectMsg:         "multiple container IDs found in cgroups (9bca8d63d5fa610783847915bcff0ecac1273e5b4bed3f6fa1b07350e0135961, a55d9ac3b312d8a2627824b6d6dd8af66fbec439bf4e0ec22d6d9945ad337a38)",
		},
		{
			name: "more than one pod UID in cgroups",
			cgroupPaths: []string{
				"/user.slice",
				"/kubepods/pod11111111-b29f-11e7-9350-020968147796/9bca8d63d5fa610783847915bcff0ecac1273e5b4bed3f6fa1b07350e0135961",
				"/kubepods/kubepods/besteffort/pod22222222-b29f-11e7-9350-020968147796/9bca8d63d5fa610783847915bcff0ecac1273e5b4bed3f6fa1b07350e0135961",
			},
			expectPodUID:      "",
			expectContainerID: "",
			expectMsg:         "multiple pod UIDs found in cgroups (11111111-b29f-11e7-9350-020968147796, 22222222-b29f-11e7-9350-020968147796)",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			podUID, containerID, err := getPodUIDAndContainerIDFromCGroups(makeCGroups(tt.cgroupPaths))

			if tt.expectMsg != "" {
				assert.Equal(t, tt.expectMsg, err.Error())
				return
			}
			assert.Equal(t, tt.expectPodUID, podUID)
			assert.Equal(t, tt.expectContainerID, containerID)
		})
	}
}
