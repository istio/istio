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
	"testing"
	"time"

	meshconfig "istio.io/api/mesh/v1alpha1"
	"istio.io/istio/pkg/config/mesh"
	"istio.io/istio/pkg/config/mesh/meshwatcher"
	"istio.io/istio/pkg/filewatcher"
	"istio.io/istio/pkg/kube/krt/krttest"
	"istio.io/istio/pkg/test/util/assert"
)

func TestNetworksWatcherShouldNotifyHandlers(t *testing.T) {
	path := newTempFile(t)

	n := meshconfig.MeshNetworks{
		Networks: make(map[string]*meshconfig.Network),
	}
	writeMessage(t, path, &n)

	w := newNetworksWatcher(t, path)
	assert.Equal(t, w.Networks(), &n)

	doneCh := make(chan struct{}, 1)

	var newN *meshconfig.MeshNetworks
	w.AddNetworksHandler(func() {
		newN = w.Networks()
		close(doneCh)
	})

	// Change the file to trigger the update.
	n.Networks["test"] = &meshconfig.Network{}
	writeMessage(t, path, &n)

	select {
	case <-doneCh:
		assert.Equal(t, newN, &n)
		assert.Equal(t, w.Networks(), newN)
		break
	case <-time.After(time.Second * 5):
		t.Fatal("timed out waiting for update")
	}
}

func newNetworksWatcher(t *testing.T, filename string) mesh.NetworksWatcher {
	t.Helper()
	w := filewatcher.NewWatcher()
	t.Cleanup(func() {
		w.Close()
	})
	opts := krttest.Options(t)
	fs, err := meshwatcher.NewFileSource(w, filename, opts)
	assert.NoError(t, err)
	col := meshwatcher.NewNetworksCollection(opts, fs)
	col.AsCollection().WaitUntilSynced(opts.Stop())
	return meshwatcher.NetworksAdapter(col)
}
