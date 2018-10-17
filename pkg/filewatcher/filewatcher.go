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

package filewatcher

import (
	"fmt"
	"path/filepath"
	"sync"

	"github.com/fsnotify/fsnotify"
	"github.com/hashicorp/go-multierror"
)

type FileWatcher interface {
	Add(path string) error
	Remove(path string) error
	Close() error
	Events(path string) chan fsnotify.Event
	Errors(path string) chan error
}

type fsNotifyWatcher struct {
	mu sync.RWMutex
	// The watcher maintain a map of fsnotify watchers,
	// keyed by path.
	watchers map[string]*fsnotify.Watcher
	// The watcher maintain a map of event channel,
	// keyed by path.
	// The watcher watches parent path of given path,
	// and filter out events of given path, then redirect
	// to the result channel.
	// Note that for symlink files, the content in received events
	// do not have to be related to the file itself.
	events map[string]chan fsnotify.Event
}

// NewWatcher return with a FileWatcher instance that implemented with fsnotify.
func NewWatcher() FileWatcher {
	w := &fsNotifyWatcher{
		watchers: map[string]*fsnotify.Watcher{},
		events:   map[string]chan fsnotify.Event{},
	}

	return w
}

// Add is implementation of Add interface of FileWatcher.
func (w *fsNotifyWatcher) Add(path string) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if _, exist := w.watchers[path]; exist {
		return fmt.Errorf("path %s already added", path)
	}

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}

	events := make(chan fsnotify.Event, 1)

	cleanedPath := filepath.Clean(path)
	parentPath, _ := filepath.Split(cleanedPath)
	// Ignore the error here as the file can be an "usual" file.
	realPath, _ := filepath.EvalSymlinks(path)

	watcher.Add(parentPath)

	go func() {
		for {
			select {
			case event, ok := <-watcher.Events:
				if !ok { // 'Events' channel is closed
					return
				}
				// Ignore the error here as the file can be an "usual" file.
				currentRealPath, _ := filepath.EvalSymlinks(path)

				// Filter out events that related to the path to watch:
				// for usual files, event name should be equal to its path;
				// For k8s configmap/secret file, real path of the symlink
				// would change on update.
				if filepath.Clean(event.Name) == cleanedPath ||
					currentRealPath != "" && currentRealPath != realPath {
					realPath = currentRealPath
					events <- event
				}
			case <-watcher.Errors:
				return
			}
		}
	}()

	w.watchers[path] = watcher
	w.events[path] = events

	return nil
}

// Remove is implementation of Remove interface of FileWatcher.
func (w *fsNotifyWatcher) Remove(path string) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	return w.remove(path)
}

func (w *fsNotifyWatcher) remove(path string) error {
	var errors *multierror.Error

	// Remove its watcher from the map.
	watcher, exist := w.watchers[path]
	if !exist {
		return fmt.Errorf("path %s already removed", path)
	}
	err := watcher.Close()
	delete(w.watchers, path)
	errors = multierror.Append(errors, err)

	// Remove its event channel from the map.
	events, exist := w.events[path]
	if !exist {
		// This should never happen.
		err := fmt.Errorf("path %s already removed", path)
		errors = multierror.Append(errors, err)
		return errors.ErrorOrNil()
	}
	delete(w.events, path)
	close(events)

	return errors.ErrorOrNil()
}

// Close is implementation of Close interface of FileWatcher.
func (w *fsNotifyWatcher) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()

	var errors *multierror.Error
	for path := range w.watchers {
		errors = multierror.Append(errors, w.remove(path))
	}

	return errors.ErrorOrNil()
}

// Events is implementation of Events interface of FileWatcher.
func (w *fsNotifyWatcher) Events(path string) chan fsnotify.Event {
	w.mu.RLock()
	defer w.mu.RUnlock()

	return w.events[path]
}

// Errors is implementation of Errors interface of FileWatcher.
func (w *fsNotifyWatcher) Errors(path string) chan error {
	w.mu.RLock()
	defer w.mu.RUnlock()

	watcher, exist := w.watchers[path]
	if !exist {
		return nil
	}

	return watcher.Errors
}
