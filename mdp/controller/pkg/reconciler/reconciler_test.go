package reconciler

import (
	v1 "k8s.io/api/core/v1"
	v12 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"testing"
)

func Test_getElligiblePods(t *testing.T) {
	// I need the address of a bool that is true, and it doesn't work with literals
	controllerTrue := true
	pods := map[string]*v1.Pod{
		"elligible": {
			ObjectMeta: v12.ObjectMeta{
				OwnerReferences: []v12.OwnerReference{
					{
						Kind: "ReplicaSet",
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
						Kind: "StatefulSet",
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
						Kind: "StatefulSet",
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
