//  Copyright 2018 Istio Authors
//
//  Licensed under the Apache License, Version 2.0 (the "License");
//  you may not use this file except in compliance with the License.
//  You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
//  Unless required by applicable law or agreed to in writing, software
//  distributed under the License is distributed on an "AS IS" BASIS,
//  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//  See the License for the specific language governing permissions and
//  limitations under the License.

package server

import (
	"fmt"
	"io/ioutil"
	"path"
	"sync"

	"github.com/howeyc/fsnotify"
	"gopkg.in/yaml.v2"

	"istio.io/istio/pkg/log"
	"istio.io/istio/pkg/mcp/server"
)

const (
	accesslistfilename = "accesslist.yaml"
)

type accessList struct {
	Allowed []string
}

func watchAccessList(stopCh <-chan struct{}, folder string) (*server.ListAuthChecker, error) {

	accesslistfile := path.Join(folder, accesslistfilename)

	// Do the initial read.
	list, err := readAccessList(accesslistfile)
	if err != nil {
		return nil, err
	}

	checker := server.NewListAuthChecker()
	checker.Set(list.Allowed...)

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}

	if err = watcher.Watch(accesslistfile); err != nil {
		return nil, fmt.Errorf("unable to watch accesslist file %q: %v", accesslistfile, err)
	}

	// Coordinate the goroutines for orderly shutdown
	var exitSignal sync.WaitGroup
	exitSignal.Add(1)
	exitSignal.Add(1)

	go func() {
		defer exitSignal.Done()

		for {
			select {
			case e := <-watcher.Event:
				if e.IsCreate() || e.IsModify() {
					if list, err = readAccessList(accesslistfile); err != nil {
						log.Errorf("Error reading access list %q: %v", accesslistfile, err)
					} else {
						checker.Set(list.Allowed...)
					}
				}
				break

			case <-stopCh:
				return
			}
		}
	}()

	// Watch error events in a separate go routine See:
	// https://github.com/fsnotify/fsnotify#faq
	go func() {
		defer exitSignal.Done()

		for {
			select {
			case e := <-watcher.Error:
				log.Errorf("error event while watching access list file: %v", e)
				break

			case <-stopCh:
				return
			}
		}
	}()

	go func() {
		exitSignal.Wait()
		_ = watcher.Close()
	}()

	return checker, nil
}

func readAccessList(accesslistfile string) (accessList, error) {
	b, err := ioutil.ReadFile(accesslistfile)
	if err != nil {
		return accessList{}, fmt.Errorf("unable to read access list file %q: %v", accesslistfile, err)
	}

	var list accessList
	if err = yaml.Unmarshal(b, &list); err != nil {
		return accessList{}, fmt.Errorf("unable to parse access list file %q: %v", accesslistfile, err)
	}

	return list, nil
}
