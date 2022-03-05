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

package mesh

import (
	"errors"
	"time"

	meshconfig "istio.io/api/mesh/v1alpha1"
)

// only used for testing, exposes a blocking Update method that allows test environments to trigger meshConfig updates
type TestWatcher struct {
	internalWatcher
	doneCh chan struct{} // used to implement a blocking Update method
}

func NewTestWatcher(meshConfig *meshconfig.MeshConfig) *TestWatcher {
	w := &TestWatcher{
		internalWatcher: internalWatcher{MeshConfig: meshConfig},
	}
	w.doneCh = make(chan struct{}, 1)
	w.AddMeshHandler(func() {
		w.doneCh <- struct{}{}
	})
	return w
}

// blocks until watcher handlers trigger
func (t *TestWatcher) Update(meshConfig *meshconfig.MeshConfig, timeout time.Duration) error {
	t.HandleMeshConfig(meshConfig)
	select {
	case <-t.doneCh:
		return nil
	case <-time.After(time.Second * timeout):
		return errors.New("timed out waiting for mesh.Watcher handler to trigger")
	}
}
