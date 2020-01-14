// Copyright 2019 Istio Authors
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

package fswatch

import (
	"log"
	"math"
	"os"
	"path/filepath"
	"time"

	"github.com/fsnotify/fsnotify"
)

const (
	// debounceDelay is the amount of time that must elapse after a file changes without any other file changes before
	// a signal is sent.
	debounceDelay = time.Second
)

// WatchDirRecursively recursively watches the given directory path. If any file under dir changes, a debounced signal
// is sent on the returned channel. The debounce delay prevents flooding when multiple files are updated at once e.g.
// during upgrade.
func WatchDirRecursively(dir string) (<-chan struct{}, error) {
	subdirs, err := recursiveSubdirs(dir)
	if err != nil {
		return nil, err
	}

	// Raw notifies that must be debounced.
	notifyRaw := make(chan struct{})
	for _, d := range subdirs {
		if err := watchDir(d, notifyRaw); err != nil {
			return nil, err
		}
	}

	// Debounced notifies.
	notify := make(chan struct{})
	go func() {
		timer := time.NewTimer(time.Duration(math.MaxInt64))
		for {
			select {
			case <-notifyRaw:
				if timer == nil {
					timer = time.NewTimer(debounceDelay)
					break
				}
				timer.Reset(debounceDelay)
			case <-timer.C:
				notify <- struct{}{}
			}
		}
	}()

	return notify, nil
}

func watchDir(dir string, notify chan<- struct{}) error {
	var err error
	doneInit := make(chan struct{}, 1)
	go func() {
		for {
			// All errors are likely to be same reason here, just pick one.
			var watcher *fsnotify.Watcher
			watcher, err = fsnotify.NewWatcher()
			if err != nil {
				log.Print(err)
				return
			}
			defer watcher.Close()

			done := make(chan struct{}, 1)
			go func() {
				for {
					select {
					case event, ok := <-watcher.Events:
						if !ok {
							log.Printf("watcher channel closed for %s", dir)
							done <- struct{}{}
							return
						}
						if event.Op&fsnotify.Write == fsnotify.Write || event.Op&fsnotify.Create == fsnotify.Create ||
							event.Op&fsnotify.Remove == fsnotify.Remove {
							log.Println("modified file:", event.Name)
							select {
							// Send non-blocking notification.
							case notify <- struct{}{}:
							default:
							}
						}
					case err, ok := <-watcher.Errors:
						if !ok {
							log.Printf("watcher channel closed for %s", dir)
							done <- struct{}{}
							return
						}
						log.Printf("error for watcher on %s:%s", dir, err)
					}
				}
			}()

			err = watcher.Add(dir)
			if err != nil {
				log.Print(err)
				return
			}
			// Don't block the second time around.
			select {
			case doneInit <- struct{}{}:
			default:
			}
			// Wait in case we need to restart the watcher.
			<-done
		}
	}()
	// Wait for any init type of errors.
	<-doneInit
	return err
}

func recursiveSubdirs(dir string) ([]string, error) {
	var dirs []string

	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if info.IsDir() {
			dirs = append(dirs, path)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return dirs, nil
}
