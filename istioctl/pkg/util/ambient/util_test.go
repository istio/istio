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

package ambient

import (
	"context"
	"testing"

	"istio.io/istio/pkg/kube"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

func TestIsZtunnelPod(t *testing.T) {
	client := kube.NewFakeClient()
	t.Cleanup(client.Shutdown)
	generateMockK8sPods(t, client.Kube())

	testCases := []struct {
		name             string
		argsPodName      string
		argsPodNamespace string
		want             bool
	}{
		{
			name:             "app label is foo",
			argsPodName:      "foo-001",
			argsPodNamespace: "foo",
			want:             false,
		},
		{
			name:             "app label is ztunnel",
			argsPodName:      "foo-002",
			argsPodNamespace: "foo",
			want:             true,
		},
		{
			name:             "app label is ztunnel and version label is v1",
			argsPodName:      "foo-003",
			argsPodNamespace: "foo",
			want:             true,
		},
		{
			name:             "no label",
			argsPodName:      "bar-001",
			argsPodNamespace: "bar",
			want:             false,
		},
		{
			name:             "no app label",
			argsPodName:      "bar-002",
			argsPodNamespace: "bar",
			want:             false,
		},
		{
			name:             "pod does not exist",
			argsPodName:      "bar-003",
			argsPodNamespace: "bar",
			want:             false,
		},
	}
	for _, tt := range testCases {
		if got := IsZtunnelPod(client, tt.argsPodName, tt.argsPodNamespace); got != tt.want {
			t.Errorf("IsZtunnelPod() got %v, want %v", got, tt.want)
		}
	}
}

func generateMockK8sPods(t *testing.T, client kubernetes.Interface) {
	t.Helper()
	pods := []*corev1.Pod{
		{
			ObjectMeta: metav1.ObjectMeta{
				Name: "foo-001", Namespace: "foo",
				Labels: map[string]string{"app": "foo"},
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Name: "foo-002", Namespace: "foo",
				Labels: map[string]string{"app": "ztunnel"},
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Name: "foo-003", Namespace: "foo",
				Labels: map[string]string{
					"app":     "ztunnel",
					"version": "v1",
				},
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{Name: "bar-001", Namespace: "bar"},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Name: "bar-002", Namespace: "bar",
				Labels: map[string]string{"bar": "ztunnel"},
			},
		},
	}
	for _, pod := range pods {
		if _, err := client.CoreV1().Pods(pod.Namespace).Create(context.TODO(), pod, metav1.CreateOptions{}); err != nil {
			t.Fatal(err)
		}
	}
}
