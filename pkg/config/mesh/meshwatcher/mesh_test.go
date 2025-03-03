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

package meshwatcher_test

import (
	"os"
	"path/filepath"
	"testing"

	"google.golang.org/protobuf/proto"

	meshconfig "istio.io/api/mesh/v1alpha1"
	"istio.io/istio/pkg/config/mesh"
	"istio.io/istio/pkg/config/mesh/meshwatcher"
	"istio.io/istio/pkg/filewatcher"
	"istio.io/istio/pkg/kube/krt/krttest"
	"istio.io/istio/pkg/test/util/assert"
	"istio.io/istio/pkg/util/protomarshal"
)

func TestWatcherShouldNotifyHandlers(t *testing.T) {
	tracker := assert.NewTracker[string](t)
	removeTracker := assert.NewTracker[string](t)
	path := newTempFile(t)

	m := mesh.DefaultMeshConfig()
	writeMessage(t, path, m)

	w := newWatcher(t, path)
	assert.Equal(t, w.Mesh(), m)

	var newM *meshconfig.MeshConfig
	w.AddMeshHandler(func() {
		newM = w.Mesh()
		tracker.Record("event")
	})
	removeReg := w.AddMeshHandler(func() {
		removeTracker.Record("event")
	})
	tracker.Empty()

	// Change the file to trigger the update.
	m.IngressClass = "foo"
	writeMessage(t, path, m)

	tracker.WaitOrdered("event")
	assert.Equal(t, newM, m)
	assert.Equal(t, w.Mesh(), newM)
	removeTracker.WaitOrdered("event")

	// Remove the tracker
	removeReg.Remove()

	// Change the file to trigger the update.
	m.IngressClass = "not-foo"
	writeMessage(t, path, m)

	tracker.WaitOrdered("event")
	removeTracker.Empty()
}

func newWatcher(t testing.TB, filename string) mesh.Watcher {
	t.Helper()
	w := filewatcher.NewWatcher()
	t.Cleanup(func() {
		w.Close()
	})
	opts := krttest.Options(t)
	fs, err := meshwatcher.NewFileSource(w, filename, opts)
	assert.NoError(t, err)
	col := meshwatcher.NewCollection(opts, fs)

	col.AsCollection().WaitUntilSynced(opts.Stop())
	return meshwatcher.ConfigAdapter(col)
}

func newTempFile(t testing.TB) string {
	t.Helper()

	f, err := os.CreateTemp(t.TempDir(), t.Name())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = f.Close() })

	path, err := filepath.Abs(f.Name())
	if err != nil {
		t.Fatal(err)
	}
	return path
}

func writeMessage(t testing.TB, path string, msg proto.Message) {
	t.Helper()
	yml, err := protomarshal.ToYAML(msg)
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, path, yml)
}

func writeFile(t testing.TB, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o666); err != nil {
		t.Fatal(err)
	}
}
