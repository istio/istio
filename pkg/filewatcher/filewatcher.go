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
	"bufio"
	"crypto/md5"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"

	"github.com/fsnotify/fsnotify"
	multierror "github.com/hashicorp/go-multierror"
)

// FileWatcher is an interface that watches a set of files,
// delivering events to related channel.
type FileWatcher interface {
	Add(path string) error
	Remove(path string) error
	Close() error
	Events(path string) chan fsnotify.Event
	Errors(path string) chan error
}

type fsNotifyWatcher struct {
	mu sync.RWMutex
	// The watcher maintain a map of workers,
	// keyed by watched dir (parent dir of watched files).
	workers map[string]*worker
}

type worker struct {
	// watcher is a fsnotify watcher that watches the parent
	// dir of watchedFiles.
	watcher *fsnotify.Watcher
	// The worker maintain a map of event channel (included in fileTracker),
	// keyed by watched file path.
	// The worker watches parent path of given path,
	// and filter out events of given path, then redirect
	// to the result channel.
	// Note that for symlink files, the content in received events
	// do not have to be related to the file itself.
	watchedFiles map[string]*fileTracker
}

type fileTracker struct {
	events chan fsnotify.Event
	errors chan error
	// md5 sum to indicate if a file has been updated.
	md5Sum string
}

// NewWatcher return with a FileWatcher instance that implemented with fsnotify.
func NewWatcher() FileWatcher {
	w := &fsNotifyWatcher{
		workers: map[string]*worker{},
	}

	return w
}

// Add is implementation of Add interface of FileWatcher.
func (w *fsNotifyWatcher) Add(path string) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	cleanedPath, parentPath := formatPath(path)
	if worker, workerExist := w.workers[parentPath]; workerExist {
		if _, pathExist := worker.watchedFiles[cleanedPath]; pathExist {
			return fmt.Errorf("path %s already added", path)
		}

		// And the path into worker maps if not exist.
		md5Sum, err := getMd5Sum(cleanedPath)
		if err != nil {
			return fmt.Errorf("failed to get md5 sum for %s: %v", path, err)
		}
		worker.watchedFiles[cleanedPath] = &fileTracker{
			events: make(chan fsnotify.Event, 1),
			errors: make(chan error, 1),
			md5Sum: md5Sum,
		}

		return nil
	}

	// Build a new worker if not exist.
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}
	if err = watcher.Add(parentPath); err != nil {
		watcher.Close()
		return err
	}

	var md5Sum string

	if _, err = os.Stat(cleanedPath); err == nil {
		md5Sum, err = getMd5Sum(cleanedPath)
		if err != nil {
			watcher.Close()
			return fmt.Errorf("failed to get md5 sum for %s: %v", path, err)
		}
	} else {
		if !os.IsNotExist(err) {
			watcher.Close()
			return fmt.Errorf("failed to stat file: %s: %v", path, err)
		}

		// if the file doesn't exist, ignore the md5 sum.
	}

	wk := &worker{
		watcher: watcher,
		watchedFiles: map[string]*fileTracker{
			cleanedPath: {
				events: make(chan fsnotify.Event, 1),
				errors: make(chan error, 1),
				md5Sum: md5Sum,
			},
		},
	}
	w.workers[parentPath] = wk

	go func() {
		for {
			select {
			case event, ok := <-watcher.Events:
				if !ok { // 'Events' channel is closed
					return
				}

				for path, tracker := range wk.watchedFiles {
					newSum, _ := getMd5Sum(path)
					if newSum != "" && newSum != tracker.md5Sum {
						tracker.md5Sum = newSum
						tracker.events <- event
						break
					}
				}
			case err, ok := <-watcher.Errors:
				if !ok {
					// 'Errors' channel is closed. Construct an error for it.
					// We'll only close worker errors channel in "Remove" for consistency.
					err = errors.New("channel closed")
				}
				for _, tracker := range wk.watchedFiles {
					tracker.errors <- err
				}
				return
			}
		}
	}()

	return nil
}

// Remove is implementation of Remove interface of FileWatcher.
func (w *fsNotifyWatcher) Remove(path string) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	return w.remove(path)
}

func (w *fsNotifyWatcher) remove(path string) error {
	cleanedPath, parentPath := formatPath(path)

	worker, workerExist := w.workers[parentPath]
	if !workerExist {
		return fmt.Errorf("path %s already removed", path)
	}

	tracker, pathExist := worker.watchedFiles[cleanedPath]
	if !pathExist {
		return fmt.Errorf("path %s already removed", path)
	}

	delete(worker.watchedFiles, cleanedPath)
	close(tracker.events)
	close(tracker.errors)
	if len(worker.watchedFiles) == 0 {
		// Remove the watch if all of its paths have been removed.
		delete(w.workers, parentPath)
		return worker.watcher.Close()
	}

	return nil
}

// Close is implementation of Close interface of FileWatcher.
func (w *fsNotifyWatcher) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()

	var errors *multierror.Error
	for _, worker := range w.workers {
		for path := range worker.watchedFiles {
			errors = multierror.Append(errors, w.remove(path))
		}
	}

	return errors.ErrorOrNil()
}

// Events is implementation of Events interface of FileWatcher.
func (w *fsNotifyWatcher) Events(path string) chan fsnotify.Event {
	w.mu.RLock()
	defer w.mu.RUnlock()

	cleanedPath, parentPath := formatPath(path)
	worker, workerExist := w.workers[parentPath]
	if !workerExist {
		return nil
	}

	tracker, pathExist := worker.watchedFiles[cleanedPath]
	if !pathExist {
		return nil
	}

	return tracker.events
}

// Errors is implementation of Errors interface of FileWatcher.
func (w *fsNotifyWatcher) Errors(path string) chan error {
	w.mu.RLock()
	defer w.mu.RUnlock()

	cleanedPath, parentPath := formatPath(path)
	worker, workerExist := w.workers[parentPath]
	if !workerExist {
		return nil
	}

	tracker, pathExist := worker.watchedFiles[cleanedPath]
	if !pathExist {
		return nil
	}

	return tracker.errors
}

// getMd5Sum is a helper func to calculate md5 sum.
func getMd5Sum(file string) (string, error) {
	f, err := os.Open(file)
	if err != nil {
		return "", err
	}
	defer f.Close()
	r := bufio.NewReader(f)

	h := md5.New()

	_, err = io.Copy(h, r)
	if err != nil {
		return "", err
	}

	return fmt.Sprintf("%x", h.Sum(nil)), nil
}

// formatPath return with the path after Clean,
// as well as its parent path.
func formatPath(path string) (string, string) {
	cleanedPath := filepath.Clean(path)
	parentPath, _ := filepath.Split(cleanedPath)

	return cleanedPath, parentPath
}
