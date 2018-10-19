// Copyright 2017 Istio Authors
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

package kubernetesenv

import (
	"testing"

	"k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/tools/cache"

	"istio.io/istio/mixer/pkg/adapter/test"
)

func TestClusterInfoCache_Pod(t *testing.T) {
	clientset := fake.NewSimpleClientset(
		&v1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "default",
				Name:      "test",
			},
			Status: v1.PodStatus{PodIP: "10.1.10.1"},
		},
	)
	tests := []struct {
		name string
		key  string
		want bool
	}{
		{"found", "default/test", true},
		{"not found", "custom/missing", false},
		{"by IP", "10.1.10.1", true},
		{"by wrong IP", "10.1.10.2", false},
	}

	for _, v := range tests {
		t.Run(v.name, func(tt *testing.T) {
			stopCh := make(chan struct{})
			c := newCacheController(clientset, 0, test.NewEnv(t), stopCh)
			defer close(stopCh)
			go c.Run(stopCh)
			if !cache.WaitForCacheSync(stopCh, c.HasSynced) {
				tt.Fatal("Failed to sync")
			}

			_, got := c.Pod(v.key)
			if got != v.want {
				tt.Errorf("GetPod() => (_, %t), wanted (_, %t)", got, v.want)
			}
		})
	}
}
