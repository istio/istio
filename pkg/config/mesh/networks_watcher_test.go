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
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/util/assert"
	"testing"
	"time"

	. "github.com/onsi/gomega"

	meshconfig "istio.io/api/mesh/v1alpha1"
	"istio.io/istio/pkg/config/mesh"
	"istio.io/istio/pkg/filewatcher"
)


func TestNetworksWatcherShouldNotifyHandlers(t *testing.T) {
	path := newTempFile(t)
	defer removeSilent(path)

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
	fs, err := mesh.NewFileSource(filewatcher.NewWatcher(), filename, test.NewStop(t))
	assert.NoError(t, err)
	col := mesh.NewNetworksCollection(&fs, nil, test.NewStop(t))
	return mesh.NetworksAdapter(col)
}
