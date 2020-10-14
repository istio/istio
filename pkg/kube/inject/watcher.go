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
	v1 "k8s.io/api/core/v1"

	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/kube/configmapwatcher"
	"istio.io/pkg/log"
)

// Watcher watches for and reacts to injection config updates.
type Watcher interface {
	// Run starts the Watcher.
	Run(<-chan struct{})
}

var _ Watcher = &fileWatcher{}

var _ Watcher = &configMapWatcher{}

type fileWatcher struct {
	watcher    *fsnotify.Watcher
	configFile string
	valuesFile string
	callback   func(*Config, string)
}

type configMapWatcher struct {
	c *configmapwatcher.Controller
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

// NewConfigMapWatcher creates a new Watcher for changes to the given ConfigMap.
func NewConfigMapWatcher(client kube.Client, namespace, name, configKey, valuesKey string, callback func(*Config, string)) Watcher {
	c := configmapwatcher.NewController(client, namespace, name, func(cm *v1.ConfigMap) {
		sidecarConfig, valuesConfig, err := readConfigMap(cm, configKey, valuesKey)
		if err != nil {
			log.Warnf("failed to read injection config from ConfigMap: %v", err)
			return
		}
		callback(sidecarConfig, valuesConfig)
	})
	return &configMapWatcher{c: c}
}

func (w *configMapWatcher) Run(stop <-chan struct{}) {
	w.c.Run(stop)
}

func readConfigMap(cm *v1.ConfigMap, configKey, valuesKey string) (*Config, string, error) {
	if cm == nil {
		return nil, "", fmt.Errorf("no ConfigMap found")
	}

	configYaml, exists := cm.Data[configKey]
	if !exists {
		return nil, "", fmt.Errorf("missing ConfigMap config key %q", configKey)
	}
	c, err := unmarshalConfig([]byte(configYaml))
	if err != nil {
		return nil, "", fmt.Errorf("failed reading config: %v. YAML:\n%s", err, configYaml)
	}

	valuesConfig, exists := cm.Data[valuesKey]
	if !exists {
		return nil, "", fmt.Errorf("missing ConfigMap values key %q", valuesKey)
	}
	return c, valuesConfig, nil
}
