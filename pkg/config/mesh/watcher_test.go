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

package mesh_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	. "github.com/onsi/gomega"
	"google.golang.org/protobuf/proto"

	meshconfig "istio.io/api/mesh/v1alpha1"
	"istio.io/istio/pkg/config/mesh"
	"istio.io/istio/pkg/test/util/assert"
	"istio.io/istio/pkg/util/protomarshal"
	"istio.io/pkg/filewatcher"
)

func TestNewWatcherWithBadInputShouldFail(t *testing.T) {
	g := NewWithT(t)
	_, err := mesh.NewFileWatcher(filewatcher.NewWatcher(), "", false)
	g.Expect(err).ToNot(BeNil())
}

func TestWatcherShouldNotifyHandlers(t *testing.T) {
	watcherShouldNotifyHandlers(t, false)
}

func TestMultiWatcherShouldNotifyHandlers(t *testing.T) {
	watcherShouldNotifyHandlers(t, true)
}

func watcherShouldNotifyHandlers(t *testing.T, multi bool) {
	path := newTempFile(t)

	m := mesh.DefaultMeshConfig()
	writeMessage(t, path, m)

	w := newWatcher(t, path, multi)
	assert.Equal(t, w.Mesh(), m)

	doneCh := make(chan struct{}, 1)

	var newM *meshconfig.MeshConfig
	w.AddMeshHandler(func() {
		newM = w.Mesh()
		close(doneCh)
	})

	// Change the file to trigger the update.
	m.IngressClass = "foo"
	writeMessage(t, path, m)

	select {
	case <-doneCh:
		assert.Equal(t, newM, m)
		assert.Equal(t, w.Mesh(), newM)
		break
	case <-time.After(time.Second * 5):
		t.Fatal("timed out waiting for update")
	}
}

func newWatcher(t testing.TB, filename string, multi bool) mesh.Watcher {
	t.Helper()
	w, err := mesh.NewFileWatcher(filewatcher.NewWatcher(), filename, multi)
	if err != nil {
		t.Fatal(err)
	}
	return w
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

func removeSilent(path string) {
	_ = os.RemoveAll(path)
}

func BenchmarkGetMesh(b *testing.B) {
	b.StopTimer()

	path := newTempFile(b)
	defer removeSilent(path)

	m := mesh.DefaultMeshConfig()
	writeMessage(b, path, m)

	w := newWatcher(b, path, false)

	b.StartTimer()

	handler := func(mc *meshconfig.MeshConfig) {
		// Do nothing
	}

	for i := 0; i < b.N; i++ {
		handler(w.Mesh())
	}
}
