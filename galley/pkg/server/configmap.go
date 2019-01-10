// Copyright 2018 Istio Authors
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

package server

import (
	"fmt"
	"io/ioutil"
	"os"

	"github.com/fsnotify/fsnotify"
	yaml "gopkg.in/yaml.v2"

	"istio.io/istio/pkg/filewatcher"
	"istio.io/istio/pkg/mcp/server"
)

type accessList struct {
	IsBlackList bool
	Allowed     []string
}

var (
	newFileWatcher         = filewatcher.NewWatcher
	readFile               = ioutil.ReadFile
	watchEventHandledProbe func()
)

func watchAccessList(stopCh <-chan struct{}, accessListFile string) (*server.ListAuthChecker, error) {
	// Do the initial read.
	list, err := readAccessList(accessListFile)
	if err != nil {
		return nil, err
	}

	checker := server.NewListAuthChecker()
	if list.IsBlackList {
		checker.SetMode(server.AuthBlackList)
	} else {
		checker.SetMode(server.AuthWhiteList)
	}
	checker.Set(list.Allowed...)

	watcher := newFileWatcher()

	if err = watcher.Add(accessListFile); err != nil {
		return nil, fmt.Errorf("unable to watch accesslist file %q: %v", accessListFile, err)
	}

	go func() {
		for {
			select {
			case e := <-watcher.Events(accessListFile):
				if e.Op&fsnotify.Write == fsnotify.Write || e.Op&fsnotify.Create == fsnotify.Create {
					if list, err = readAccessList(accessListFile); err != nil {
						scope.Errorf("Error reading access list %q: %v", accessListFile, err)
					} else {
						if list.IsBlackList {
							checker.SetMode(server.AuthBlackList)
						} else {
							checker.SetMode(server.AuthWhiteList)
						}
						checker.Set(list.Allowed...)
					}
				} else if e.Op&fsnotify.Remove == fsnotify.Remove {
					checker.SetMode(server.AuthBlackList)
					checker.Set()
				}
				if watchEventHandledProbe != nil {
					watchEventHandledProbe()
				}
			case e := <-watcher.Errors(accessListFile):
				scope.Errorf("error event while watching access list file: %v", e)
			case <-stopCh:
				_ = watcher.Close()
				return
			}
		}
	}()

	return checker, nil
}

func readAccessList(accessListFile string) (accessList, error) {
	b, err := readFile(accessListFile)
	if err != nil {
		if os.IsNotExist(err) {
			// Treat a non-existent access list file as default open
			return accessList{IsBlackList: true}, nil
		}

		return accessList{}, fmt.Errorf("unable to read access list file %q: %v", accessListFile, err)
	}

	var list accessList
	if err = yaml.Unmarshal(b, &list); err != nil {
		return accessList{}, fmt.Errorf("unable to parse access list file %q: %v", accessListFile, err)
	}

	return list, nil
}
