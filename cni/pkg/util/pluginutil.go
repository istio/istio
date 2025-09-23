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

package util

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/fsnotify/fsnotify"

	"istio.io/istio/pkg/file"
	"istio.io/istio/pkg/log"
)

type Watcher struct {
	watcher *fsnotify.Watcher
	Events  chan struct{}
	Errors  chan error
}

// Waits until a file is modified (returns nil), the context is cancelled (returns context error), or returns error
func (w *Watcher) Wait(ctx context.Context) error {
	select {
	case <-w.Events:
		return nil
	case err := <-w.Errors:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (w *Watcher) Close() {
	_ = w.watcher.Close()
}

// Creates a file watcher that watches for any changes to the directory
func CreateFileWatcher(paths ...string) (*Watcher, error) {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, fmt.Errorf("watcher create: %v", err)
	}

	fileModified, errChan := make(chan struct{}), make(chan error)
	go watchFiles(watcher, fileModified, errChan)

	for _, path := range paths {
		if !file.Exists(path) {
			log.Infof("file watcher skipping watch on non-existent path: %v", path)
			continue
		}
		if err := watcher.Add(path); err != nil {
			if closeErr := watcher.Close(); closeErr != nil {
				err = fmt.Errorf("%s: %w", closeErr.Error(), err)
			}
			return nil, err
		}
	}

	return &Watcher{
		watcher: watcher,
		Events:  fileModified,
		Errors:  errChan,
	}, nil
}

func watchFiles(watcher *fsnotify.Watcher, fileModified chan struct{}, errChan chan error) {
	for {
		select {
		case event, ok := <-watcher.Events:
			if !ok {
				return
			}
			if event.Op&(fsnotify.Create|fsnotify.Write|fsnotify.Remove) != 0 {
				log.Infof("file modified: %v", event.Name)
				fileModified <- struct{}{}
			}
		case err, ok := <-watcher.Errors:
			if !ok {
				return
			}
			errChan <- err
		}
	}
}

// Read CNI config from file and return the unmarshalled JSON as a map
func ReadCNIConfigMap(path string) (map[string]any, error) {
	cniConfig, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var cniConfigMap map[string]any
	if err = json.Unmarshal(cniConfig, &cniConfigMap); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}

	return cniConfigMap, nil
}

// Given an unmarshalled CNI config JSON map, return the plugin list asserted as a []interface{}
func GetPlugins(cniConfigMap map[string]any) (plugins []any, err error) {
	plugins, ok := cniConfigMap["plugins"].([]any)
	if !ok {
		err = fmt.Errorf("error reading plugin list from CNI config")
		return plugins, err
	}
	return plugins, err
}

// Given the raw plugin interface, return the plugin asserted as a map[string]interface{}
func GetPlugin(rawPlugin any) (plugin map[string]any, err error) {
	plugin, ok := rawPlugin.(map[string]any)
	if !ok {
		err = fmt.Errorf("error reading plugin from CNI config plugin list")
		return plugin, err
	}
	return plugin, err
}

// Marshal the CNI config map and append a new line
func MarshalCNIConfig(cniConfigMap map[string]any) ([]byte, error) {
	cniConfig, err := json.MarshalIndent(cniConfigMap, "", "  ")
	if err != nil {
		return nil, err
	}
	cniConfig = append(cniConfig, "\n"...)
	return cniConfig, nil
}
