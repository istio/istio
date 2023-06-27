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

package filewatcher

import (
	"errors"
	"fmt"
	"path/filepath"
	"sync"

	"github.com/fsnotify/fsnotify"
)

// FileWatcher is an interface that watches a set of files,
// delivering events to related channel.
type FileWatcher interface {
	// Start watching a path. Calling Add multiple times on the same path panics.
	Add(path string) error

	// Stop watching a path. Removing a path that's not currently being watched panics.
	Remove(path string) error
	Close() error
	Events(path string) chan fsnotify.Event
	Errors(path string) chan error
}

type fileWatcher struct {
	mu sync.RWMutex

	// The watcher maintain a map of workers,
	// keyed by watched dir (parent dir of watched files).
	workers map[string]*workerState

	funcs *patchTable
}

type workerState struct {
	worker *worker
	count  int
}

// functions that can be replaced in a test setting
type patchTable struct {
	newWatcher     func() (*fsnotify.Watcher, error)
	addWatcherPath func(*fsnotify.Watcher, string) error
}

// NewWatcher return with a FileWatcher instance that implemented with fsnotify.
func NewWatcher() FileWatcher {
	return &fileWatcher{
		workers: map[string]*workerState{},

		// replaceable functions for tests
		funcs: &patchTable{
			newWatcher: fsnotify.NewWatcher,
			addWatcherPath: func(watcher *fsnotify.Watcher, path string) error {
				return watcher.Add(path)
			},
		},
	}
}

// Close releases all resources associated with the watcher
func (fw *fileWatcher) Close() error {
	fw.mu.Lock()
	defer fw.mu.Unlock()

	for _, ws := range fw.workers {
		ws.worker.terminate()
	}
	fw.workers = nil

	return nil
}

// Add a path to watch
func (fw *fileWatcher) Add(path string) error {
	fw.mu.Lock()
	defer fw.mu.Unlock()

	ws, cleanedPath, _, err := fw.getWorker(path)
	if err != nil {
		return err
	}

	if err = ws.worker.addPath(cleanedPath); err == nil {
		ws.count++
	}

	return err
}

// Stop watching a path
func (fw *fileWatcher) Remove(path string) error {
	fw.mu.Lock()
	defer fw.mu.Unlock()

	ws, cleanedPath, parentPath, err := fw.getWorker(path)
	if err != nil {
		return err
	}

	if err = ws.worker.removePath(cleanedPath); err == nil {
		ws.count--
		if ws.count == 0 {
			ws.worker.terminate()
			delete(fw.workers, parentPath)
		}
	}

	return err
}

// Events returns an event notification channel for a path
func (fw *fileWatcher) Events(path string) chan fsnotify.Event {
	fw.mu.RLock()
	defer fw.mu.RUnlock()

	ws, cleanedPath, err := fw.findWorker(path)
	if err != nil {
		return nil
	}

	return ws.worker.eventChannel(cleanedPath)
}

// Errors returns an error notification channel for a path
func (fw *fileWatcher) Errors(path string) chan error {
	fw.mu.RLock()
	defer fw.mu.RUnlock()

	ws, cleanedPath, err := fw.findWorker(path)
	if err != nil {
		return nil
	}

	return ws.worker.errorChannel(cleanedPath)
}

func (fw *fileWatcher) getWorker(path string) (*workerState, string, string, error) {
	if fw.workers == nil {
		return nil, "", "", errors.New("using a closed watcher")
	}

	cleanedPath := filepath.Clean(path)
	parentPath, _ := filepath.Split(cleanedPath)

	ws, workerExists := fw.workers[parentPath]
	if !workerExists {
		wk, err := newWorker(parentPath, fw.funcs)
		if err != nil {
			return nil, "", "", err
		}

		ws = &workerState{
			worker: wk,
		}

		fw.workers[parentPath] = ws
	}

	return ws, cleanedPath, parentPath, nil
}

func (fw *fileWatcher) findWorker(path string) (*workerState, string, error) {
	if fw.workers == nil {
		return nil, "", errors.New("using a closed watcher")
	}

	cleanedPath := filepath.Clean(path)
	parentPath, _ := filepath.Split(cleanedPath)

	ws, workerExists := fw.workers[parentPath]
	if !workerExists {
		return nil, "", fmt.Errorf("no path registered for %s", path)
	}

	return ws, cleanedPath, nil
}
