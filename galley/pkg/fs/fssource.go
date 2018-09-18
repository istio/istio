//  Copyright 2018 Istio Authors
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

package fs

import (
	"crypto/sha1"
	"fmt"

	"io/ioutil"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/howeyc/fsnotify"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"istio.io/istio/pkg/log"

	"istio.io/istio/galley/pkg/kube"
	"istio.io/istio/galley/pkg/kube/source"
	kube_meta "istio.io/istio/galley/pkg/metadata/kube"
	"istio.io/istio/galley/pkg/runtime"
	"istio.io/istio/galley/pkg/runtime/resource"
)

var supportedExtensions = map[string]bool{
	".yaml": true,
	".yml":  true,
}
var scope = log.RegisterScope("file-source", "Source for File System", 0)
var watcher *fsnotify.Watcher

// fsSource is source implementation for filesystem.
type fsSource struct {
	root            string
	checkDuration   time.Duration
	donec           chan struct{}
	mu              sync.RWMutex
	shas            map[string][sha1.Size]byte
	ch              chan resource.Event
	kinds           map[string]bool
	fileResorceKeys map[string][]fileResourceKey
	version         int64
}

func (s *fsSource) readFiles(root string) map[string]*istioResource {
	results := map[string]*istioResource{}

	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		result := s.readFile(path, info)
		if result != nil && len(result) != 0 {
			for k, r := range result {
				results[k] = r
			}
		}
		//add watcher for sub folders
		if info.Mode().IsDir() {
			watcher.Watch(path)
		}
		return nil
	})
	if err != nil {
		scope.Errorf("failure during filepath.Walk: %v", err)
	}
	return results
}

func (s *fsSource) readFile(path string, info os.FileInfo) map[string]*istioResource {
	result := map[string]*istioResource{}
	if mode := info.Mode() & os.ModeType; !supportedExtensions[filepath.Ext(path)] || (mode != 0 && mode != os.ModeSymlink) {
		return nil
	}
	data, err := ioutil.ReadFile(path)
	if err != nil {
		scope.Warnf("Failed to read %s: %v", path, err)
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	resourceKeyList := make([]fileResourceKey, 1)
	for _, r := range parseFile(path, data) {
		if !s.kinds[r.u.GetKind()] {
			continue
		}
		result[r.key()] = r
		resourceKeyList = append(resourceKeyList, fileResourceKey{r.key(), r.u.GetKind()})
	}

	s.fileResorceKeys[path] = resourceKeyList
	return result
}

func (s *fsSource) pubEvent(newData *map[string]*istioResource) {
	s.mu.Lock()
	defer s.mu.Unlock()
	versionUpdated := false
	for k, r := range *newData {
		if _, ok := s.shas[k]; ok {
			if s.shas[k] != r.sha {
				versionUpdated = true
				break
			}
		} else {
			versionUpdated = true
			break
		}
	}
	if versionUpdated {
		s.version++
	}

	for k, r := range *newData {
		if _, ok := s.shas[k]; ok {
			if s.shas[k] != r.sha {
				s.process(resource.Updated, k, "", r)
			}
			s.shas[k] = r.sha
			continue
		}
		s.process(resource.Added, k, "", r)
		s.shas[k] = r.sha
	}
}

func (s *fsSource) deleteResource(fileName string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if fileResorceKeys, ok := s.fileResorceKeys[fileName]; ok {
		for _, fileResorceKey := range fileResorceKeys {
			if fileResorceKey != (fileResourceKey{}) {
				delete(s.shas, fileResorceKey.key)
				s.process(resource.Deleted, fileResorceKey.key, fileResorceKey.kind, nil)
			}
		}
		delete(s.fileResorceKeys, fileName)
	}
}

func (s *fsSource) initialCheck() {
	newData := s.readFiles(s.root)
	s.mu.Lock()
	defer s.mu.Unlock()
	for k, r := range newData {
		s.process(resource.Added, k, "", r)
		s.shas[k] = r.sha
	}
}

// Stop implements runtime.Source
func (s *fsSource) Stop() {
	s.fileResorceKeys = nil
	s.shas = nil
	close(s.donec)
	watcher.Close()
	s.donec = nil
}

func (s *fsSource) process(eventKind resource.EventKind, key, resourceKind string, r *istioResource) {
	var u *unstructured.Unstructured
	var spec kube.ResourceSpec
	var kind string
	//no need to care about real data when delete resources
	if eventKind == resource.Deleted {
		u = nil
		kind = resourceKind
	} else {
		u = r.u
		kind = r.u.GetKind()
	}
	for _, v := range kube_meta.Types.All() {
		if v.Kind == kind {
			spec = v
			break
		}
	}

	source.ProcessEvent(spec, eventKind, key, fmt.Sprintf("v%d", s.version), u, s.ch)
}

// Start implements runtime.Source
func (s *fsSource) Start() (chan resource.Event, error) {
	s.ch = make(chan resource.Event, 1024)
	watcher, _ = fsnotify.NewWatcher()
	watcher.Watch(s.root)
	s.initialCheck()
	go func() {
		for {
			select {
			// watch for events
			case ev, more := <-watcher.Event:
				if more && ev.IsDelete() {
					s.deleteResource(ev.Name)
				}
				if more && ev.IsCreate() {
					fi, err := os.Stat(ev.Name)
					if err != nil {
						scope.Warnf("error occurs for watching %s", ev.Name)
					} else {
						if fi.Mode().IsDir() {
							scope.Debugf("add wathcher for new folder %s", ev.Name)
							watcher.Watch(ev.Name)
						} else {
							newData := s.readFile(ev.Name, fi)
							if newData != nil && len(newData) != 0 {
								s.pubEvent(&newData)
							}
						}
					}
				}
				if more && ev.IsModify() {
					fi, err := os.Stat(ev.Name)
					if err != nil {
						scope.Warnf("error occurs for watching %s", ev.Name)
					} else {
						if !fi.Mode().IsDir() {
							newData := s.readFile(ev.Name, fi)
							if newData != nil && len(newData) != 0 {
								s.pubEvent(&newData)
							}
						}
					}
				}
			case <-s.donec:
				return
			}
		}
	}()
	return s.ch, nil

}

// New returns a File System implementation of runtime.Source.
func New(root string) (runtime.Source, error) {
	return newFsSource(root, kube_meta.Types.All())
}

func newFsSource(root string, specs []kube.ResourceSpec) (runtime.Source, error) {
	fs := &fsSource{
		root:            root,
		kinds:           map[string]bool{},
		fileResorceKeys: map[string][]fileResourceKey{},
		donec:           make(chan struct{}),
		shas:            map[string][sha1.Size]byte{},
		version:         0,
	}
	for _, spec := range specs {
		fs.kinds[spec.Kind] = true
	}
	return fs, nil
}
