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

package meshwatcher

import (
	"errors"
	"time"

	meshconfig "istio.io/api/mesh/v1alpha1"
	"istio.io/istio/pkg/config/mesh"
	"istio.io/istio/pkg/kube/krt"
)

// NewFixedWatcher creates a new Watcher that always returns the given mesh config. It will never
// fire any events, since the config never changes.
func NewFixedWatcher(mesh *meshconfig.MeshConfig) mesh.Watcher {
	return adapter{krt.NewStatic(&MeshConfigResource{mesh}, true)}
}

type FixedNetworksWatcher struct {
	networksAdapter
	col krt.StaticSingleton[MeshNetworksResource]
}

func (w FixedNetworksWatcher) SetNetworks(n *meshconfig.MeshNetworks) {
	w.col.Set(&MeshNetworksResource{n})
}

// NewFixedNetworksWatcher creates a new NetworksWatcher that always returns the given config.
// It will never fire any events, since the config never changes.
func NewFixedNetworksWatcher(networks *meshconfig.MeshNetworks) FixedNetworksWatcher {
	col := krt.NewStatic(&MeshNetworksResource{networks}, true)
	a := networksAdapter{col}
	return FixedNetworksWatcher{
		networksAdapter: a,
		col:             col,
	}
}

// only used for testing, exposes a blocking Update method that allows test environments to trigger meshConfig updates
type TestWatcher struct {
	adapter
	col    krt.StaticSingleton[MeshConfigResource]
	doneCh chan struct{} // used to implement a blocking Update method
}

func NewTestWatcher(meshConfig *meshconfig.MeshConfig) *TestWatcher {
	c := krt.NewStatic(&MeshConfigResource{meshConfig}, true)
	w := &TestWatcher{
		adapter: adapter{c},
		col:     c,
		doneCh:  make(chan struct{}, 1),
	}
	w.doneCh = make(chan struct{}, 1)
	// TODO this is probably broken as we don't have ordering
	w.AddMeshHandler(func() {
		w.doneCh <- struct{}{}
	})
	return w
}

// Update blocks until watcher handlers trigger
func (t *TestWatcher) Update(meshConfig *meshconfig.MeshConfig, timeout time.Duration) error {
	t.col.Set(&MeshConfigResource{meshConfig})
	select {
	case <-t.doneCh:
		return nil
	case <-time.After(timeout):
		return errors.New("timed out waiting for mesh.Watcher handler to trigger")
	}
}
