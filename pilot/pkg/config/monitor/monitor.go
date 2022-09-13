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

package monitor

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"time"

	"github.com/fsnotify/fsnotify"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/config"
	istiolog "istio.io/pkg/log"
)

// Monitor will poll a config function in order to update a ConfigStore as
// changes are found.
type Monitor struct {
	name            string
	root            string
	store           model.ConfigStore
	configs         []*config.Config
	getSnapshotFunc func() ([]*config.Config, error)
	// channel to trigger updates on
	// generally set to a file watch, but used in tests as well
	updateCh chan struct{}
}

var log = istiolog.RegisterScope("monitor", "file configuration monitor", 0)

// NewMonitor creates a Monitor and will delegate to a passed in controller.
// The controller holds a reference to the actual store.
// Any func that returns a []*model.Config can be used with the Monitor
func NewMonitor(name string, delegateStore model.ConfigStore, getSnapshotFunc func() ([]*config.Config, error), root string) *Monitor {
	monitor := &Monitor{
		name:            name,
		root:            root,
		store:           delegateStore,
		getSnapshotFunc: getSnapshotFunc,
	}
	return monitor
}

const watchDebounceDelay = 50 * time.Millisecond

// Trigger notifications when a file is mutated
func fileTrigger(path string, ch chan struct{}, stop <-chan struct{}) error {
	if path == "" {
		return nil
	}
	fs, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}
	watcher := recursiveWatcher{fs}
	if err = watcher.watchRecursive(path); err != nil {
		return err
	}
	go func() {
		defer watcher.Close()
		var debounceC <-chan time.Time
		for {
			select {
			case <-debounceC:
				debounceC = nil
				ch <- struct{}{}
			case e := <-watcher.Events:
				s, err := os.Stat(e.Name)
				if err == nil && s != nil && s.IsDir() {
					// If it's a directory, add a watch for it so we see nested files.
					if e.Op&fsnotify.Create != 0 {
						log.Debugf("add watch for %v: %v", s.Name(), watcher.watchRecursive(e.Name))
					}
				}
				// Can't stat a deleted directory, so attempt to remove it. If it fails it is not a problem
				if e.Op&fsnotify.Remove != 0 {
					_ = watcher.Remove(e.Name)
				}
				if debounceC == nil {
					debounceC = time.After(watchDebounceDelay)
				}
			case err := <-watcher.Errors:
				log.Warnf("Error watching file trigger: %v %v", path, err)
				return
			case signal := <-stop:
				log.Infof("Shutting down file watcher: %v %v", path, signal)
				return
			}
		}
	}()
	return nil
}

// recursiveWatcher wraps a fsnotify wrapper to add a best-effort recursive directory watching in user
// space. See https://github.com/fsnotify/fsnotify/issues/18. The implementation is inherently racy,
// as files added to a directory immediately after creation may not trigger events; as such it is only useful
// when an event causes a full reconciliation, rather than acting on an individual event
type recursiveWatcher struct {
	*fsnotify.Watcher
}

// watchRecursive adds all directories under the given one to the watch list.
func (m recursiveWatcher) watchRecursive(path string) error {
	err := filepath.Walk(path, func(walkPath string, fi os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if fi.IsDir() {
			if err = m.Watcher.Add(walkPath); err != nil {
				return err
			}
		}
		return nil
	})
	return err
}

// Start starts a new Monitor. Immediately checks the Monitor getSnapshotFunc
// and updates the controller. It then kicks off an asynchronous event loop that
// periodically polls the getSnapshotFunc for changes until a close event is sent.
func (m *Monitor) Start(stop <-chan struct{}) {
	m.checkAndUpdate()

	c := make(chan struct{}, 1)
	m.updateCh = c
	if err := fileTrigger(m.root, m.updateCh, stop); err != nil {
		log.Errorf("Unable to setup FileTrigger for %s: %v", m.root, err)
	}
	// Run the close loop asynchronously.
	go func() {
		for {
			select {
			case <-c:
				log.Infof("Triggering reload of file configuration")
				m.checkAndUpdate()
			case <-stop:
				return
			}
		}
	}()
}

func (m *Monitor) checkAndUpdate() {
	newConfigs, err := m.getSnapshotFunc()
	// If an error exists then log it and return to running the check and update
	// Do not edit the local []*model.config until the connection has been reestablished
	// The error will only come from a directory read error or a gRPC connection error
	if err != nil {
		log.Warnf("checkAndUpdate Error Caught %s: %v\n", m.name, err)
		return
	}

	// make a deep copy of newConfigs to prevent data race
	copyConfigs := make([]*config.Config, 0)
	for _, config := range newConfigs {
		cpy := config.DeepCopy()
		copyConfigs = append(copyConfigs, &cpy)
	}

	// Compare the new list to the previous one and detect changes.
	oldLen := len(m.configs)
	newLen := len(newConfigs)
	oldIndex, newIndex := 0, 0
	for oldIndex < oldLen && newIndex < newLen {
		oldConfig := m.configs[oldIndex]
		newConfig := newConfigs[newIndex]
		if v := compareIds(oldConfig, newConfig); v < 0 {
			m.deleteConfig(oldConfig)
			oldIndex++
		} else if v > 0 {
			m.createConfig(newConfig)
			newIndex++
		} else {
			// version may change without content changing
			oldConfig.Meta.ResourceVersion = newConfig.Meta.ResourceVersion
			if !reflect.DeepEqual(oldConfig, newConfig) {
				m.updateConfig(newConfig)
			}
			oldIndex++
			newIndex++
		}
	}

	// Detect remaining deletions
	for ; oldIndex < oldLen; oldIndex++ {
		m.deleteConfig(m.configs[oldIndex])
	}

	// Detect remaining additions
	for ; newIndex < newLen; newIndex++ {
		m.createConfig(newConfigs[newIndex])
	}

	// Save the updated list.
	m.configs = copyConfigs
}

func (m *Monitor) createConfig(c *config.Config) {
	if _, err := m.store.Create(*c); err != nil {
		log.Warnf("Failed to create config %s %s/%s: %v (%+v)", c.GroupVersionKind, c.Namespace, c.Name, err, *c)
	}
}

func (m *Monitor) updateConfig(c *config.Config) {
	// Set the resource version and create timestamp based on the existing config.
	if prev := m.store.Get(c.GroupVersionKind, c.Name, c.Namespace); prev != nil {
		c.ResourceVersion = prev.ResourceVersion
		c.CreationTimestamp = prev.CreationTimestamp
	}

	if _, err := m.store.Update(*c); err != nil {
		log.Warnf("Failed to update config (%+v): %v ", *c, err)
	}
}

func (m *Monitor) deleteConfig(c *config.Config) {
	if err := m.store.Delete(c.GroupVersionKind, c.Name, c.Namespace, nil); err != nil {
		log.Warnf("Failed to delete config (%+v): %v ", *c, err)
	}
}

// compareIds compares the IDs (i.e. Namespace, GroupVersionKind, and Name) of the two configs and returns
// 0 if a == b, -1 if a < b, and 1 if a > b. Used for sorting config arrays.
func compareIds(a, b *config.Config) int {
	return strings.Compare(a.Key(), b.Key())
}
