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
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	"istio.io/istio/pkg/test/util/assert"
)

func TestWithProcFs(t *testing.T) {
	n, err := NewPodNetnsProcFinder(fakeFs(true))
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

	if len(podUIDNetns) == 0 {
		t.Fatal("expected to find pod netns")
	}

	expectedUID := "863b91d4-4b68-4efa-917f-4b560e3e86aa"
	if podUIDNetns[expectedUID] == (workloadInfo{}) {
		t.Fatal("expected to find pod netns under pod uid")
	}

	foundStart := podUIDNetns[expectedUID].NetnsCloser().OwnerProcStarttime()
	// See testdata/cgroupns/1/stat
	if foundStart != 70298968 {
		t.Fatalf("didn't find expected starttime, found %d", foundStart)
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
