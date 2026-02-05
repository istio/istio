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

package multicluster

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/types"

	"istio.io/istio/pkg/kube/controllers"
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/util/assert"
	"istio.io/istio/pkg/test/util/file"
	"istio.io/istio/pkg/test/util/retry"
)

func TestFileConfigSource(t *testing.T) {
	stop := test.NewStop(t)
	root := t.TempDir()
	source := newFileConfigSource(root)

	tracker := assert.NewTracker[string](t)
	source.AddEventHandler(func(key types.NamespacedName, event controllers.EventType) {
		tracker.Record(fmt.Sprintf("%s/%s", event.String(), key.String()))
	})

	assert.Equal(t, source.HasSynced(), false)
	source.Start(stop)
	retry.UntilOrFail(t, source.HasSynced, retry.Timeout(2*time.Second))

	assert.Equal(t, source.Get(types.NamespacedName{Name: "remote-1", Namespace: "istio-system"}), (*remoteConfig)(nil))

	kubeconfig := kubeconfigYAML("remote-1")
	file.WriteOrFail(t, filepath.Join(root, "remote.yaml"), kubeconfig)
	tracker.WaitOrdered("add//remote-1")

	got := source.Get(types.NamespacedName{Name: "remote-1", Namespace: "istio-system"})
	if got == nil {
		t.Fatal("expected kubeconfig entry to be present")
	}
	if !bytes.Equal(got.Data["remote-1"], kubeconfig) {
		t.Fatalf("unexpected kubeconfig data: got %q", string(got.Data["remote-1"]))
	}

	if err := os.Remove(filepath.Join(root, "remote.yaml")); err != nil {
		t.Fatalf("failed to remove kubeconfig file: %v", err)
	}
	tracker.WaitOrdered("delete//remote-1")
}

func kubeconfigYAML(clusterID string) []byte {
	return []byte(fmt.Sprintf(`apiVersion: v1
kind: Config
clusters:
- name: %s
  cluster:
    server: https://%s.example.com
contexts:
- name: %s-context
  context:
    cluster: %s
    user: %s-user
current-context: %s-context
users:
- name: %s-user
  user:
    token: token
`, clusterID, clusterID, clusterID, clusterID, clusterID, clusterID, clusterID))
}
