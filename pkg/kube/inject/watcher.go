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

package inject

import (
	"fmt"
	"path/filepath"
	"time"

	"github.com/howeyc/fsnotify"

	"istio.io/pkg/log"
)

// Watcher watches for and reacts to injection config updates.
type Watcher interface {
	// Run starts the Watcher.
	Run(<-chan struct{})
}

var _ Watcher = &fileWatcher{}

type fileWatcher struct {
	watcher    *fsnotify.Watcher
	configFile string
	valuesFile string
	callback   func(*Config, string)
}

// NewFileWatcher creates a Watcher for local config and values files.
func NewFileWatcher(configFile, valuesFile string, callback func(*Config, string)) (Watcher, error) {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}
	// watch the parent directory of the target files so we can catch
	// symlink updates of k8s ConfigMaps volumes.
	watchDir, _ := filepath.Split(configFile)
	if err := watcher.Watch(watchDir); err != nil {
		return nil, fmt.Errorf("could not watch %v: %v", watchDir, err)
	}
	return &fileWatcher{
		watcher:    watcher,
		configFile: configFile,
		valuesFile: valuesFile,
		callback:   callback,
	}, nil
}

func (w *fileWatcher) Run(stop <-chan struct{}) {
	defer w.watcher.Close()
	var timerC <-chan time.Time
	for {
		select {
		case <-timerC:
			timerC = nil
			sidecarConfig, valuesConfig, err := loadConfig(w.configFile, w.valuesFile)
			if err != nil {
				log.Errorf("update error: %v", err)
				break
			}
			w.callback(sidecarConfig, valuesConfig)
		case event := <-w.watcher.Event:
			log.Debugf("Injector watch update: %+v", event)
			// use a timer to debounce configuration updates
			if (event.IsModify() || event.IsCreate()) && timerC == nil {
				timerC = time.After(watchDebounceDelay)
			}
		case err := <-w.watcher.Error:
			log.Errorf("Watcher error: %v", err)
		case <-stop:
			return
		}
	}
}
