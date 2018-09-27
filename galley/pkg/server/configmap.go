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
	"sync"

	"github.com/fsnotify/fsnotify"
	"gopkg.in/yaml.v2"

	"istio.io/istio/pkg/log"
	"istio.io/istio/pkg/mcp/server"
)

type accessList struct {
	Allowed []string
}

type fileWatcher interface {
	Add(path string) error
	Close() error
	Events() chan fsnotify.Event
	Errors() chan error
}

type fsNotifyWatcher struct {
	*fsnotify.Watcher
}

func (w *fsNotifyWatcher) Add(path string) error       { return w.Watcher.Add(path) }
func (w *fsNotifyWatcher) Close() error                { return w.Watcher.Close() }
func (w *fsNotifyWatcher) Events() chan fsnotify.Event { return w.Watcher.Events }
func (w *fsNotifyWatcher) Errors() chan error          { return w.Watcher.Errors }

func newFsnotifyWatcher() (fileWatcher, error) {
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}
	return &fsNotifyWatcher{Watcher: w}, nil
}

var (
	newFileWatcher         = newFsnotifyWatcher
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
	checker.Set(list.Allowed...)

	watcher, err := newFileWatcher()
	if err != nil {
		return nil, err
	}

	// TODO: https://github.com/istio/istio/issues/7877
	// It looks like fsnotify watchers have problems due to following symlinks. This needs to be handled.
	if err = watcher.Add(accessListFile); err != nil {
		return nil, fmt.Errorf("unable to watch accesslist file %q: %v", accessListFile, err)
	}

	// Coordinate the goroutines for orderly shutdown
	var exitSignal sync.WaitGroup
	exitSignal.Add(2)

	go func() {
		defer exitSignal.Done()

		for {
			select {
			case e := <-watcher.Events():
				if e.Op&fsnotify.Write == fsnotify.Write {
					if list, err = readAccessList(accessListFile); err != nil {
						log.Errorf("Error reading access list %q: %v", accessListFile, err)
					} else {
						checker.Set(list.Allowed...)
					}
				}
				if watchEventHandledProbe != nil {
					watchEventHandledProbe()
				}
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
			case e := <-watcher.Errors():
				log.Errorf("error event while watching access list file: %v", e)

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

func readAccessList(accessListFile string) (accessList, error) {
	b, err := readFile(accessListFile)
	if err != nil {
		return accessList{}, fmt.Errorf("unable to read access list file %q: %v", accessListFile, err)
	}

	var list accessList
	if err = yaml.Unmarshal(b, &list); err != nil {
		return accessList{}, fmt.Errorf("unable to parse access list file %q: %v", accessListFile, err)
	}

	return list, nil
}
