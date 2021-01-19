package mesh

import (
	"errors"
	"time"

	meshconfig "istio.io/api/mesh/v1alpha1"
)

// only used for testing, exposes a blocking Update method that allows test environments to trigger meshConfig updates
type TestWatcher struct {
	InternalWatcher
	doneCh chan struct{} // used to implement a blocking Update method
}

func NewTestWatcher(meshConfig *meshconfig.MeshConfig) *TestWatcher {
	w := &TestWatcher{
		InternalWatcher: InternalWatcher{MeshConfig: meshConfig},
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
