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

// TestFileConfigSource verifies file-based kubeconfig add/delete events and sync behavior.
func TestFileConfigSource(t *testing.T) {
	stop := test.NewStop(t)
	root := t.TempDir()
	source := newFileConfigSource(root)

	tracker := assert.NewTracker[string](t)
	// Register before Start() to verify deferred handlers are replayed once the collection is initialized.
	source.AddEventHandler(func(key types.NamespacedName, event controllers.EventType) {
		// NamespacedName.String() renders an empty namespace, so file-based events show up as "add//remote-1".
		tracker.Record(fmt.Sprintf("%s/%s", event.String(), key.String()))
	})

	assert.Equal(t, source.HasSynced(), false)
	// Start initializes the file-based collection and begins watching the directory.
	source.Start(stop)
	retry.UntilOrFail(t, source.HasSynced, retry.Timeout(2*time.Second))

	// An empty directory should not return any kubeconfig entries.
	assert.Equal(t, source.Get(types.NamespacedName{Name: "remote-1", Namespace: ""}), (*remoteConfig)(nil))

	kubeconfig := kubeconfigFileYAML("remote-1")
	file.WriteOrFail(t, filepath.Join(root, "remote.yaml"), kubeconfig)
	// File-based sources ignore namespaces, so events show up as "add//remote-1".
	tracker.WaitOrdered("add//remote-1")

	got := source.Get(types.NamespacedName{Name: "remote-1", Namespace: ""})
	if got == nil {
		t.Fatal("expected kubeconfig entry to be present")
	}
	assert.Equal(t, got.Data["remote-1"], kubeconfig)

	assert.NoError(t, os.Remove(filepath.Join(root, "remote.yaml")))
	tracker.WaitOrdered("delete//remote-1")
}
