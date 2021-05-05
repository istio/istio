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

package reconciler

import (
	"testing"

	v1 "k8s.io/api/core/v1"
	v12 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func Test_getElligiblePods(t *testing.T) {
	// I need the address of a bool that is true, and it doesn't work with literals
	controllerTrue := true
	pods := map[string]*v1.Pod{
		"elligible": {
			ObjectMeta: v12.ObjectMeta{
				OwnerReferences: []v12.OwnerReference{
					{
						Kind:       "ReplicaSet",
						Controller: &controllerTrue,
					},
				},
			},
			Status: v1.PodStatus{
				Phase: v1.PodRunning,
			},
		},
		"dontinclude": {
			ObjectMeta: v12.ObjectMeta{
				OwnerReferences: []v12.OwnerReference{
					{
						Kind:       "StatefulSet",
						Controller: &controllerTrue,
					},
				},
			},
			Status: v1.PodStatus{
				Phase: v1.PodRunning,
			},
		},
		"notelligible": {
			ObjectMeta: v12.ObjectMeta{
				OwnerReferences: []v12.OwnerReference{
					{
						Kind:       "StatefulSet",
						Controller: &controllerTrue,
					},
				},
			},
			Status: v1.PodStatus{
				Phase: v1.PodFailed,
			},
		},
	}
	actual := getElligiblePods(pods)
	if _, ok := actual["elligible"]; !ok {
		t.Fatalf("The elligible pod was inappropriately filtered out.")
	}
	delete(actual, "elligible")
	if len(actual) > 0 {
		t.Fatalf("Found unexpectedly elligible pods: %v", actual)
	}
}
