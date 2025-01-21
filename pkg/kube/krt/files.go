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

package krt

import (
	"fmt"
	"os"
	"time"

	"go.uber.org/atomic"
	"sigs.k8s.io/yaml"

	"istio.io/istio/pkg/filewatcher"
	"istio.io/istio/pkg/ptr"
)

type FileCollection[T any] struct {
	StaticCollection[T]
}

func NewFileCollection[T any](opts ...CollectionOption) FileCollection[T] {
	panic("not yet implemented")
}

type FileSingleton[T any] struct {
	Singleton[T]
}

// NewFileSingleton returns a collection that reads and watches a single file
// The `readFile` function is used to read and deserialize the file and will be called each time the file changes.
// This will also be called during the initial construction of the collection; if the initial readFile fails an error is returned.
func NewFileSingleton[T any](
	fileWatcher filewatcher.FileWatcher,
	filename string,
	readFile func(filename string) (T, error),
	opts ...CollectionOption,
) (FileSingleton[T], error) {
	cfg, err := readFile(filename)
	if err != nil {
		return FileSingleton[T]{}, err
	}

	o := buildCollectionOptions(opts...)

	cur := atomic.NewPointer(&cfg)
	trigger := NewRecomputeTrigger(true, opts...)
	sc := NewSingleton[T](func(ctx HandlerContext) *T {
		trigger.MarkDependant(ctx)
		return cur.Load()
	}, opts...)
	sc.AsCollection().WaitUntilSynced(o.stop)
	watchFile(fileWatcher, filename, o.stop, func() {
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
		return target, fmt.Errorf("failed to read file %s: %v", filename, err)
	}
	if err := yaml.Unmarshal(y, &target); err != nil {
		return target, fmt.Errorf("failed to read file %s: %v", filename, err)
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
