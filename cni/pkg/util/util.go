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
	"io/ioutil"

	"github.com/fsnotify/fsnotify"
	"github.com/pkg/errors"
)

// Creates a file watcher that watches for any changes to the directory
func CreateFileWatcher(dir string) (watcher *fsnotify.Watcher, fileModified chan bool, errChan chan error, err error) {
	watcher, err = fsnotify.NewWatcher()
	if err != nil {
		return
	}

	fileModified, errChan = make(chan bool), make(chan error)
	go watchFiles(watcher, fileModified, errChan)

	if err = watcher.Add(dir); err != nil {
		if closeErr := watcher.Close(); closeErr != nil {
			err = errors.Wrap(err, closeErr.Error())
		}
		return nil, nil, nil, err
	}

	return
}

func watchFiles(watcher *fsnotify.Watcher, fileModified chan bool, errChan chan error) {
	for {
		select {
		case _, ok := <-watcher.Events:
			if !ok {
				return
			}
			fileModified <- true
		case err, ok := <-watcher.Errors:
			if !ok {
				return
			}
			errChan <- err
		}
	}
}

// Waits until a file is modified (returns nil), the context is cancelled (returns context error), or returns error
func WaitForFileMod(ctx context.Context, fileModified chan bool, errChan chan error) error {
	select {
	case <-fileModified:
		return nil
	case err := <-errChan:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Read CNI config from file and return the unmarshalled JSON as a map
func ReadCNIConfigMap(path string) (map[string]interface{}, error) {
	cniConfig, err := ioutil.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var cniConfigMap map[string]interface{}
	if err = json.Unmarshal(cniConfig, &cniConfigMap); err != nil {
		return nil, errors.Wrap(err, path)
	}

	return cniConfigMap, nil
}

// Given an unmarshalled CNI config JSON map, return the plugin list asserted as a []interface{}
func GetPlugins(cniConfigMap map[string]interface{}) (plugins []interface{}, err error) {
	plugins, ok := cniConfigMap["plugins"].([]interface{})
	if !ok {
		err = errors.New("error reading plugin list from CNI config")
		return
	}
	return
}

// Given the raw plugin interface, return the plugin asserted as a map[string]interface{}
func GetPlugin(rawPlugin interface{}) (plugin map[string]interface{}, err error) {
	plugin, ok := rawPlugin.(map[string]interface{})
	if !ok {
		err = errors.New("error reading plugin from CNI config plugin list")
		return
	}
	return
}

// Marshal the CNI config map and append a new line
func MarshalCNIConfig(cniConfigMap map[string]interface{}) ([]byte, error) {
	cniConfig, err := json.MarshalIndent(cniConfigMap, "", "  ")
	if err != nil {
		return nil, err
	}
	cniConfig = append(cniConfig, "\n"...)
	return cniConfig, nil
}
