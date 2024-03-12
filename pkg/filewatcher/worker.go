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
	"bufio"
	"bytes"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"sync"

	"github.com/fsnotify/fsnotify"
)

type worker struct {
	mu sync.RWMutex

	// watcher is an fsnotify watcher that watches the parent
	// dir of watchedFiles.
	dirWatcher *fsnotify.Watcher

	// The worker maintains a map of channels keyed by watched file path.
	// The worker watches parent path of given path,
	// and filters out events of given path, then redirect
	// to the result channel.
	// Note that for symlink files, the content in received events
	// do not have to be related to the file itself.
	watchedFiles map[string]*fileTracker

	// tracker lifecycle
	retireTrackerCh chan *fileTracker

	// tells the worker to exit
	terminateCh chan bool
}

type fileTracker struct {
	events chan fsnotify.Event
	errors chan error

	// Hash sum to indicate if a file has been updated.
	hash []byte
}

func newWorker(path string, funcs *patchTable) (*worker, error) {
	dirWatcher, err := funcs.newWatcher()
	if err != nil {
		return nil, err
	}

	if err = funcs.addWatcherPath(dirWatcher, path); err != nil {
		_ = dirWatcher.Close()
		return nil, err
	}

	wk := &worker{
		dirWatcher:      dirWatcher,
		watchedFiles:    make(map[string]*fileTracker),
		retireTrackerCh: make(chan *fileTracker),
		terminateCh:     make(chan bool),
	}

	go wk.listen()

	return wk, nil
}

func (wk *worker) listen() {
	wk.loop()

	_ = wk.dirWatcher.Close()

	// drain any retiring trackers that may be pending
	wk.drainRetiringTrackers()

	// clean up the rest
	for _, ft := range wk.watchedFiles {
		retireTracker(ft)
	}
}

func (wk *worker) loop() {
	for {
		select {
		case event := <-wk.dirWatcher.Events:
			// work on a copy of the watchedFiles map, so that we don't interfere
			// with the caller's use of the map
			for path, ft := range wk.getTrackers() {
				if ft.events == nil {
					// tracker has been retired, skip it
					continue
				}

				sum := getHashSum(path)
				if !bytes.Equal(sum, ft.hash) {
					ft.hash = sum

					select {
					case ft.events <- event:
						// nothing to do

					case ft := <-wk.retireTrackerCh:
						retireTracker(ft)

					case <-wk.terminateCh:
						return
					}
				}
			}

		case err := <-wk.dirWatcher.Errors:
			for _, ft := range wk.getTrackers() {
				if ft.errors == nil {
					// tracker has been retired, skip it
					continue
				}

				select {
				case ft.errors <- err:
					// nothing to do

				case ft := <-wk.retireTrackerCh:
					retireTracker(ft)

				case <-wk.terminateCh:
					return
				}
			}

		case ft := <-wk.retireTrackerCh:
			retireTracker(ft)

		case <-wk.terminateCh:
			return
		}
	}
}

// used only by the worker goroutine
func (wk *worker) drainRetiringTrackers() {
	// cleanup any trackers that were in the process
	// of being retired, but didn't get processed due
	// to termination
	for {
		select {
		case ft := <-wk.retireTrackerCh:
			retireTracker(ft)
		default:
			return
		}
	}
}

// make a local copy of the set of trackers to avoid contention with callers
// used only by the worker goroutine
func (wk *worker) getTrackers() map[string]*fileTracker {
	wk.mu.RLock()

	result := make(map[string]*fileTracker, len(wk.watchedFiles))
	for k, v := range wk.watchedFiles {
		result[k] = v
	}

	wk.mu.RUnlock()
	return result
}

// used only by the worker goroutine
func retireTracker(ft *fileTracker) {
	close(ft.events)
	close(ft.errors)
	ft.events = nil
	ft.errors = nil
}

func (wk *worker) terminate() {
	wk.terminateCh <- true
}

func (wk *worker) addPath(path string) error {
	wk.mu.Lock()

	ft := wk.watchedFiles[path]
	if ft != nil {
		wk.mu.Unlock()
		return fmt.Errorf("path %s is already being watched", path)
	}

	ft = &fileTracker{
		events: make(chan fsnotify.Event),
		errors: make(chan error),
		hash:   getHashSum(path),
	}

	wk.watchedFiles[path] = ft
	wk.mu.Unlock()

	return nil
}

func (wk *worker) removePath(path string) error {
	wk.mu.Lock()

	ft := wk.watchedFiles[path]
	if ft == nil {
		wk.mu.Unlock()
		return fmt.Errorf("path %s not found", path)
	}

	delete(wk.watchedFiles, path)
	wk.mu.Unlock()

	wk.retireTrackerCh <- ft
	return nil
}

func (wk *worker) eventChannel(path string) chan fsnotify.Event {
	wk.mu.RLock()
	defer wk.mu.RUnlock()

	if ft := wk.watchedFiles[path]; ft != nil {
		return ft.events
	}

	return nil
}

func (wk *worker) errorChannel(path string) chan error {
	wk.mu.RLock()
	defer wk.mu.RUnlock()

	if ft := wk.watchedFiles[path]; ft != nil {
		return ft.errors
	}

	return nil
}

// gets the hash of the given file, or nil if there's a problem
func getHashSum(file string) []byte {
	f, err := os.Open(file)
	if err != nil {
		return nil
	}
	defer f.Close()
	r := bufio.NewReader(f)

	h := sha256.New()
	_, _ = io.Copy(h, r)
	return h.Sum(nil)
}
