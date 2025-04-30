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

package files

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	"go.uber.org/atomic"
	"sigs.k8s.io/yaml"

	"istio.io/istio/pkg/filewatcher"
	"istio.io/istio/pkg/kube/krt"
	"istio.io/istio/pkg/log"
	"istio.io/istio/pkg/ptr"
	"istio.io/istio/pkg/slices"
	"istio.io/istio/pkg/util/sets"
)

type FolderWatch[T any] struct {
	root  string
	parse func([]byte) ([]T, error)

	mu        sync.RWMutex
	state     []T
	callbacks []func()
}

func NewFolderWatch[T any](fileDir string, parse func([]byte) ([]T, error), stop <-chan struct{}) (*FolderWatch[T], error) {
	fw := &FolderWatch[T]{root: fileDir, parse: parse}
	// Read initial state
	if err := fw.readOnce(); err != nil {
		return nil, err
	}
	fw.watch(stop)
	return fw, nil
}

var supportedExtensions = sets.New(".yaml", ".yml")

func (f *FolderWatch[T]) get() []T {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return f.state
}

func (f *FolderWatch[T]) readOnce() error {
	var result []T

	err := filepath.Walk(f.root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		} else if !supportedExtensions.Contains(filepath.Ext(path)) || (info.Mode()&os.ModeType) != 0 {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			log.Warnf("Failed to readOnce %s: %v", path, err)
			return err
		}
		parsed, err := f.parse(data)
		if err != nil {
			log.Warnf("Failed to parse %s: %v", path, err)
			return err
		}
		result = append(result, parsed...)
		return nil
	})
	if err != nil {
		log.Warnf("failure during filepath.Walk: %v", err)
	}

	if err != nil {
		return err
	}

	f.mu.Lock()
	f.state = result
	cb := slices.Clone(f.callbacks)
	f.mu.Unlock()
	for _, c := range cb {
		c()
	}
	return nil
}

func (f *FolderWatch[T]) watch(stop <-chan struct{}) {
	c := make(chan struct{}, 1)
	if err := f.fileTrigger(c, stop); err != nil {
		log.Errorf("Unable to setup FileTrigger for %s: %v", f.root, err)
		return
	}
	// Run the close loop asynchronously.
	go func() {
		for {
			select {
			case <-c:
				log.Infof("Triggering reload of file configuration")
				if err := f.readOnce(); err != nil {
					log.Warnf("unable to reload file configuration %v: %v", f.root, err)
				}
			case <-stop:
				return
			}
		}
	}()
}

const watchDebounceDelay = 50 * time.Millisecond

// Trigger notifications when a file is mutated
func (f *FolderWatch[T]) fileTrigger(events chan struct{}, stop <-chan struct{}) error {
	fs, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}
	watcher := recursiveWatcher{fs}
	if err = watcher.watchRecursive(f.root); err != nil {
		return err
	}
	go func() {
		defer watcher.Close()
		var debounceC <-chan time.Time
		for {
			select {
			case <-debounceC:
				debounceC = nil
				events <- struct{}{}
			case e := <-watcher.Events:
				s, err := os.Stat(e.Name)
				if err == nil && s != nil && s.IsDir() {
					// If it's a directory, add a watch for it so we see nested files.
					if e.Op&fsnotify.Create != 0 {
						log.Debugf("add watch for %v: %v", s.Name(), watcher.watchRecursive(e.Name))
					}
				}
				// Can't stat a deleted directory, so attempt to remove it. If it fails it is not a problem
				if e.Op&fsnotify.Remove != 0 {
					_ = watcher.Remove(e.Name)
				}
				if debounceC == nil {
					debounceC = time.After(watchDebounceDelay)
				}
			case err := <-watcher.Errors:
				log.Warnf("Error watching file trigger: %v %v", f.root, err)
				return
			case signal := <-stop:
				log.Infof("Shutting down file watcher: %v %v", f.root, signal)
				return
			}
		}
	}()
	return nil
}

func (f *FolderWatch[T]) subscribe(fn func()) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.callbacks = append(f.callbacks, fn)
}

// recursiveWatcher wraps a fsnotify wrapper to add a best-effort recursive directory watching in user
// space. See https://github.com/fsnotify/fsnotify/issues/18. The implementation is inherently racy,
// as files added to a directory immediately after creation may not trigger events; as such it is only useful
// when an event causes a full reconciliation, rather than acting on an individual event
type recursiveWatcher struct {
	*fsnotify.Watcher
}

// watchRecursive adds all directories under the given one to the watch list.
func (m recursiveWatcher) watchRecursive(path string) error {
	err := filepath.Walk(path, func(walkPath string, fi os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if fi.IsDir() {
			if err = m.Watcher.Add(walkPath); err != nil {
				return err
			}
		}
		return nil
	})
	return err
}

type FileCollection[T any] struct {
	krt.StaticCollection[T]
	read func() []T
}

func NewFileCollection[F any, O any](w *FolderWatch[F], transform func(F) *O, opts ...krt.CollectionOption) FileCollection[O] {
	res := FileCollection[O]{
		read: func() []O {
			return readSnapshot[F, O](w, transform)
		},
	}
	sc := krt.NewStaticCollection[O](nil, res.read(), opts...)
	w.subscribe(func() {
		now := res.read()
		sc.Reset(now)
	})
	res.StaticCollection = sc
	return res
}

func readSnapshot[F any, O any](w *FolderWatch[F], transform func(F) *O) []O {
	res := w.get()
	return slices.MapFilter(res, transform)
}

type FileSingleton[T any] struct {
	krt.Singleton[T]
}

// NewFileSingleton returns a collection that reads and watches a single file
// The `readFile` function is used to readOnce and deserialize the file and will be called each time the file changes.
// This will also be called during the initial construction of the collection; if the initial readFile fails an error is returned.
func NewFileSingleton[T any](
	fileWatcher filewatcher.FileWatcher,
	filename string,
	readFile func(filename string) (T, error),
	opts ...krt.CollectionOption,
) (FileSingleton[T], error) {
	cfg, err := readFile(filename)
	if err != nil {
		return FileSingleton[T]{}, err
	}

	stop := krt.GetStop(opts...)

	cur := atomic.NewPointer(&cfg)
	trigger := krt.NewRecomputeTrigger(true, opts...)
	sc := krt.NewSingleton[T](func(ctx krt.HandlerContext) *T {
		trigger.MarkDependant(ctx)
		return cur.Load()
	}, opts...)
	sc.AsCollection().WaitUntilSynced(stop)
	watchFile(fileWatcher, filename, stop, func() {
		cfg, err := readFile(filename)
		if err != nil {
			log.Warnf("failed to update: %v", err)
			return
		}
		cur.Store(&cfg)
		trigger.TriggerRecomputation()
	})
	return FileSingleton[T]{sc}, nil
}

func ReadFileAsYaml[T any](filename string) (T, error) {
	target := ptr.Empty[T]()
	y, err := os.ReadFile(filename)
	if err != nil {
		return target, fmt.Errorf("failed to readOnce file %s: %v", filename, err)
	}
	if err := yaml.Unmarshal(y, &target); err != nil {
		return target, fmt.Errorf("failed to readOnce file %s: %v", filename, err)
	}
	return target, nil
}

func watchFile(fileWatcher filewatcher.FileWatcher, file string, stop <-chan struct{}, callback func()) {
	_ = fileWatcher.Add(file)
	go func() {
		var timerC <-chan time.Time
		for {
			select {
			case <-stop:
				return
			case <-timerC:
				timerC = nil
				callback()
			case <-fileWatcher.Events(file):
				// Use a timer to debounce configuration updates
				if timerC == nil {
					timerC = time.After(100 * time.Millisecond)
				}
			}
		}
	}()
}
