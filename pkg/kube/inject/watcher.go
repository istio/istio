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
	"context"
	"fmt"
	"path/filepath"
	"time"

	"github.com/fsnotify/fsnotify"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/kube/configmapwatcher"
	"istio.io/pkg/log"
)

// Watcher watches for and reacts to injection config updates.
type Watcher interface {
	// SetHandler sets the handler that is run when the config changes.
	// Must call this before Run.
	SetHandler(func(*Config, string) error)

	// Run starts the Watcher. Must call this after SetHandler.
	Run(<-chan struct{})

	// Get returns the sidecar and values configuration.
	Get() (*Config, string, error)
}

var _ Watcher = &fileWatcher{}

var _ Watcher = &configMapWatcher{}

type fileWatcher struct {
	watcher    *fsnotify.Watcher
	configFile string
	valuesFile string
	handler    func(*Config, string) error
}

type configMapWatcher struct {
	c         *configmapwatcher.Controller
	client    kube.Client
	namespace string
	name      string
	configKey string
	valuesKey string
	handler   func(*Config, string) error
}

// NewFileWatcher creates a Watcher for local config and values files.
func NewFileWatcher(configFile, valuesFile string) (Watcher, error) {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}
	// watch the parent directory of the target files so we can catch
	// symlink updates of k8s ConfigMaps volumes.
	watchDir, _ := filepath.Split(configFile)
	if err := watcher.Add(watchDir); err != nil {
		return nil, fmt.Errorf("could not watch %v: %v", watchDir, err)
	}
	return &fileWatcher{
		watcher:    watcher,
		configFile: configFile,
		valuesFile: valuesFile,
	}, nil
}

func (w *fileWatcher) Run(stop <-chan struct{}) {
	defer w.watcher.Close()
	var timerC <-chan time.Time
	for {
		select {
		case <-timerC:
			timerC = nil
			sidecarConfig, valuesConfig, err := w.Get()
			if err != nil {
				log.Errorf("update error: %v", err)
				break
			}
			if w.handler != nil {
				if err := w.handler(sidecarConfig, valuesConfig); err != nil {
					log.Errorf("update error: %v", err)
				}
			}
		case event, ok := <-w.watcher.Events:
			if !ok {
				return
			}
			log.Debugf("Injector watch update: %+v", event)
			// use a timer to debounce configuration updates
			if ((event.Op&fsnotify.Write == fsnotify.Write) || (event.Op&fsnotify.Create == fsnotify.Create)) && timerC == nil {
				timerC = time.After(watchDebounceDelay)
			}
		case err, ok := <-w.watcher.Errors:
			if !ok {
				return
			}
			log.Errorf("Watcher error: %v", err)
		case <-stop:
			return
		}
	}
}

func (w *fileWatcher) Get() (*Config, string, error) {
	return loadConfig(w.configFile, w.valuesFile)
}

func (w *fileWatcher) SetHandler(handler func(*Config, string) error) {
	w.handler = handler
}

// NewConfigMapWatcher creates a new Watcher for changes to the given ConfigMap.
func NewConfigMapWatcher(client kube.Client, namespace, name, configKey, valuesKey string) Watcher {
	w := &configMapWatcher{
		client:    client,
		namespace: namespace,
		name:      name,
		configKey: configKey,
		valuesKey: valuesKey,
	}
	w.c = configmapwatcher.NewController(client, namespace, name, func(cm *v1.ConfigMap) {
		sidecarConfig, valuesConfig, err := readConfigMap(cm, configKey, valuesKey)
		if err != nil {
			log.Warnf("failed to read injection config from ConfigMap: %v", err)
			return
		}
		if w.handler != nil {
			if err := w.handler(sidecarConfig, valuesConfig); err != nil {
				log.Errorf("update error: %v", err)
			}
		}
	})
	return w
}

func (w *configMapWatcher) Run(stop <-chan struct{}) {
	w.c.Run(stop)
}

func (w *configMapWatcher) Get() (*Config, string, error) {
	cms := w.client.CoreV1().ConfigMaps(w.namespace)
	cm, err := cms.Get(context.TODO(), w.name, metav1.GetOptions{})
	if err != nil {
		return nil, "", err
	}
	return readConfigMap(cm, w.configKey, w.valuesKey)
}

func (w *configMapWatcher) SetHandler(handler func(*Config, string) error) {
	w.handler = handler
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
