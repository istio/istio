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
	"sync"

	"github.com/fsnotify/fsnotify"
)

// NewFileWatcherFunc returns a function which creates a new file
// watcher. This may be used to provide test hooks for using the
// FakeWatcher implementation below.
type NewFileWatcherFunc func() FileWatcher

// FakeWatcher provides a fake file watcher implementation for unit
// tests. Production code should use the `NewWatcher()`.
type FakeWatcher struct {
	sync.Mutex

	events      map[string]chan fsnotify.Event
	errors      map[string]chan error
	changedFunc func(path string, added bool)
}

// InjectEvent injects an event into the fake file watcher.
func (w *FakeWatcher) InjectEvent(path string, event fsnotify.Event) {
	w.Lock()
	ch, ok := w.events[path]
	w.Unlock()

	if ok {
		ch <- event
	}
}

// InjectError injects an error into the fake file watcher.
func (w *FakeWatcher) InjectError(path string, err error) {
	w.Lock()
	ch, ok := w.errors[path]
	w.Unlock()

	if ok {
		ch <- err
	}
}

// NewFakeWatcher returns a function which creates a new fake watcher for unit
// testing. This allows observe callers to inject events and errors per-watched
// path. changedFunc() provides a callback notification when a new watch is added
// or removed. Production code should use `NewWatcher()`.
func NewFakeWatcher(changedFunc func(path string, added bool)) (NewFileWatcherFunc, *FakeWatcher) {
	w := &FakeWatcher{
		events:      make(map[string]chan fsnotify.Event),
		errors:      make(map[string]chan error),
		changedFunc: changedFunc,
	}
	return func() FileWatcher {
		return w
	}, w
}

// Add is a fake implementation of the FileWatcher interface.
func (w *FakeWatcher) Add(path string) error {
	w.Lock()

	// w.events and w.errors are always updated together. We only check
	// the first to determine existence.
	if _, ok := w.events[path]; ok {
		w.Unlock()
		return fmt.Errorf("path %v already exists", path)
	}

	w.events[path] = make(chan fsnotify.Event, 1000)
	w.errors[path] = make(chan error, 1000)

	w.Unlock()

	if w.changedFunc != nil {
		w.changedFunc(path, true)
	}
	return nil
}

// Remove is a fake implementation of the FileWatcher interface.
func (w *FakeWatcher) Remove(path string) error {
	w.Lock()
	defer w.Unlock()

	if _, ok := w.events[path]; !ok {
		return errors.New("path doesn't exist")
	}

	delete(w.events, path)
	delete(w.errors, path)
	if w.changedFunc != nil {
		w.changedFunc(path, false)
	}
	return nil
}

// Close is a fake implementation of the FileWatcher interface.
func (w *FakeWatcher) Close() error {
	w.Lock()
	for path, ch := range w.events {
		close(ch)
		delete(w.events, path)
	}
	for path, ch := range w.errors {
		close(ch)
		delete(w.errors, path)
	}
	defer w.Unlock()
	return nil
}

// Events is a fake implementation of the FileWatcher interface.
func (w *FakeWatcher) Events(path string) chan fsnotify.Event {
	w.Lock()
	defer w.Unlock()
	return w.events[path]
}

// Errors is a fake implementation of the FileWatcher interface.
func (w *FakeWatcher) Errors(path string) chan error {
	w.Lock()
	defer w.Unlock()
	return w.errors[path]
}
