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

package fs

import (
	"bytes"
	"crypto/sha1"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"sync"

	"github.com/ghodss/yaml"
	"github.com/howeyc/fsnotify"

	kubeMeta "istio.io/istio/galley/pkg/metadata/kube"
	"istio.io/istio/galley/pkg/runtime"
	"istio.io/istio/galley/pkg/runtime/resource"
	"istio.io/istio/galley/pkg/source/kube/builtin"
	"istio.io/istio/galley/pkg/source/kube/dynamic"
	"istio.io/istio/galley/pkg/source/kube/dynamic/converter"
	"istio.io/istio/galley/pkg/source/kube/log"
	"istio.io/istio/galley/pkg/source/kube/schema"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	kubeJson "k8s.io/apimachinery/pkg/runtime/serializer/json"
)

const (
	yamlSeparator = "\n---\n"
)

var (
	supportedExtensions = map[string]bool{
		".yaml": true,
		".yml":  true,
	}
)

// source is source implementation for filesystem.
type source struct {
	// configuration for the converters.
	config *converter.Config

	// Config File Path
	root string

	donec chan struct{}

	mu sync.RWMutex

	// map to store namespace/name : shas
	shas map[resource.FullName][sha1.Size]byte

	handler resource.EventHandler

	// map to store kind : bool to indicate whether we need to deal with the resource or not
	kinds map[string]bool

	// map to store filename: []{namespace/name,kind} to indicate whether the resources has been deleted from one file
	fileResourceKeys map[string][]*fileResourceKey

	// fsresource version
	version int64

	watcher *fsnotify.Watcher
}

func (s *source) readFiles(root string) map[resource.FullName]*fileResource {
	results := map[resource.FullName]*fileResource{}

	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		result := s.readFile(path, info, true)
		if result != nil && len(result) != 0 {
			for k, r := range result {
				results[k] = r
			}
		}
		//add watcher for sub folders
		if info.Mode().IsDir() {
			_ = s.watcher.Watch(path)
		}
		return nil
	})
	if err != nil {
		log.Scope.Errorf("failure during filepath.Walk: %v", err)
	}
	return results
}

func (s *source) readFile(path string, info os.FileInfo, initial bool) map[resource.FullName]*fileResource {
	result := map[resource.FullName]*fileResource{}
	if mode := info.Mode() & os.ModeType; !supportedExtensions[filepath.Ext(path)] || (mode != 0 && mode != os.ModeSymlink) {
		return nil
	}
	data, err := ioutil.ReadFile(path)
	if err != nil {
		log.Scope.Warnf("Failed to read %s: %v", path, err)
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	resourceKeyList := make([]*fileResourceKey, 1)
	for _, r := range s.parseFile(path, data) {
		if !s.kinds[r.spec.Kind] {
			continue
		}
		result[r.entry.Key] = r
		resourceKeyList = append(resourceKeyList, r.newKey())
	}
	if initial {
		s.fileResourceKeys[path] = resourceKeyList
	}
	return result
}

//process delete part of the resources in a file
func (s *source) processPartialDelete(fileName string, newData *map[resource.FullName]*fileResource) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if fileResorceKeys, ok := s.fileResourceKeys[fileName]; ok {
		for i := len(fileResorceKeys) - 1; i >= 0; i-- {
			if fileResorceKeys[i] != nil {
				if _, ok := (*newData)[(*fileResorceKeys[i]).key]; !ok {
					delete(s.shas, (*fileResorceKeys[i]).key)
					s.process(resource.Deleted, (*fileResorceKeys[i]).key, (*fileResorceKeys[i]).kind, nil)
					fileResorceKeys = append(fileResorceKeys[:i], fileResorceKeys[i+1:]...)
				}
			}
		}
		if len(fileResorceKeys) > 0 {
			s.fileResourceKeys[fileName] = fileResorceKeys
		}
		if len(fileResorceKeys) == 0 {
			delete(s.fileResourceKeys, fileName)
		}
	}

}

func (s *source) processAddOrUpdate(fileName string, newData *map[resource.FullName]*fileResource) {
	s.mu.Lock()
	defer s.mu.Unlock()
	// need versionUpdated as sometimes when fswatcher fires events, there is actually no change on the file content
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
				s.fileResourceKeys[fileName] = append(s.fileResourceKeys[fileName], r.newKey())
				s.process(resource.Updated, k, "", r)
			}
			s.shas[k] = r.sha
			continue
		}
		if s.fileResourceKeys != nil {
			s.fileResourceKeys[fileName] = append(s.fileResourceKeys[fileName], r.newKey())
		}
		s.process(resource.Added, k, "", r)
		if s.shas != nil {
			s.shas[k] = r.sha
		}
	}
}

//process delete all resources in a file
func (s *source) processDelete(fileName string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if fileResourceKeys, ok := s.fileResourceKeys[fileName]; ok {
		for _, fileResourceKey := range fileResourceKeys {
			if fileResourceKey != nil {
				delete(s.shas, (*fileResourceKey).key)
				s.process(resource.Deleted, (*fileResourceKey).key, (*fileResourceKey).kind, nil)
			}
		}
		delete(s.fileResourceKeys, fileName)
	}
}

func (s *source) initialCheck() {
	newData := s.readFiles(s.root)
	s.mu.Lock()
	defer s.mu.Unlock()
	for k, r := range newData {
		s.process(resource.Added, k, "", r)
		s.shas[k] = r.sha
	}
	s.handler(resource.FullSyncEvent)
}

// Stop implements runtime.Source
func (s *source) Stop() {
	close(s.donec)
	_ = s.watcher.Close()
}

func (s *source) process(eventKind resource.EventKind, key resource.FullName, resourceKind string, r *fileResource) {
	version := resource.Version(fmt.Sprintf("v%d", s.version))

	var event resource.Event
	switch eventKind {
	case resource.Added, resource.Updated:
		event = resource.Event{
			Kind: eventKind,
			Entry: resource.Entry{
				ID: resource.VersionedKey{
					Key: resource.Key{
						Collection: r.spec.Target.Collection,
						FullName:   key,
					},
					Version: version,
				},
				Item:     r.entry.Resource,
				Metadata: r.entry.Metadata,
			},
		}
	case resource.Deleted:
		spec := kubeMeta.Types.Get(resourceKind)
		event = resource.Event{
			Kind: eventKind,
			Entry: resource.Entry{
				ID: resource.VersionedKey{
					Key: resource.Key{
						Collection: spec.Target.Collection,
						FullName:   key,
					},
					Version: version,
				},
			},
		}
	}

	log.Scope.Debugf("Dispatching source event: %v", event)
	s.handler(event)
}

// Start implements runtime.Source
func (s *source) Start(handler resource.EventHandler) error {
	s.handler = handler
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}
	s.watcher = watcher
	_ = s.watcher.Watch(s.root)
	s.initialCheck()
	go func() {
		for {
			select {
			// watch for events
			case ev, more := <-s.watcher.Event:
				if !more {
					break
				}
				if ev.IsDelete() {
					s.processDelete(ev.Name)
				} else if ev.IsCreate() {
					fi, err := os.Stat(ev.Name)
					if err != nil {
						log.Scope.Warnf("error occurs for watching %s", ev.Name)
					} else {
						if fi.Mode().IsDir() {
							log.Scope.Debugf("add watcher for new folder %s", ev.Name)
							_ = s.watcher.Watch(ev.Name)
						} else {
							newData := s.readFile(ev.Name, fi, true)
							if newData != nil && len(newData) != 0 {
								s.processAddOrUpdate(ev.Name, &newData)
							}
						}
					}
				} else if ev.IsModify() {
					fi, err := os.Stat(ev.Name)
					if err != nil {
						log.Scope.Warnf("error occurs for watching %s", ev.Name)
					} else {
						if fi.Mode().IsDir() {
							_ = s.watcher.RemoveWatch(ev.Name)
						} else {
							newData := s.readFile(ev.Name, fi, false)
							if newData != nil && len(newData) != 0 {
								s.processPartialDelete(ev.Name, &newData)
								s.processAddOrUpdate(ev.Name, &newData)
							} else {
								s.processDelete(ev.Name)
							}
						}
					}
				}
			case <-s.donec:
				return
			}
		}
	}()
	return nil
}

// New returns a File System implementation of runtime.Source.
func New(root string, schema *schema.Instance, config *converter.Config) (runtime.Source, error) {
	fs := &source{
		config:           config,
		root:             root,
		kinds:            map[string]bool{},
		fileResourceKeys: map[string][]*fileResourceKey{},
		donec:            make(chan struct{}),
		shas:             map[resource.FullName][sha1.Size]byte{},
		version:          0,
	}
	for _, spec := range schema.All() {
		fs.kinds[spec.Kind] = true
	}
	return fs, nil
}

type fileResource struct {
	entry converter.Entry
	spec  *schema.ResourceSpec
	sha   [sha1.Size]byte
}

func (r *fileResource) newKey() *fileResourceKey {
	return &fileResourceKey{
		kind: r.spec.Kind,
		key:  r.entry.Key,
	}
}

type fileResourceKey struct {
	key  resource.FullName
	kind string
}

func (s *source) parseFile(path string, data []byte) []*fileResource {
	chunks := bytes.Split(data, []byte(yamlSeparator))
	resources := make([]*fileResource, 0, len(chunks))
	for i, chunk := range chunks {
		chunk = bytes.TrimSpace(chunk)
		if len(chunk) == 0 {
			continue
		}
		r, err := s.parseChunk(chunk)
		if err != nil {
			log.Scope.Errorf("Error processing %s[%d]: %v", path, i, err)
			continue
		}
		if r == nil {
			continue
		}
		resources = append(resources, r)
	}
	return resources
}

func (s *source) parseChunk(yamlChunk []byte) (*fileResource, error) {
	// Convert to JSON
	jsonChunk, err := yaml.YAMLToJSON(yamlChunk)
	if err != nil {
		return nil, fmt.Errorf("failed converting YAML to JSON")
	}

	// Peek at the beginning of the JSON to
	groupVersionKind, err := kubeJson.DefaultMetaFactory.Interpret(jsonChunk)
	if err != nil {
		return nil, err
	}

	spec := kubeMeta.Types.Get(groupVersionKind.Kind)
	if spec == nil {
		return nil, fmt.Errorf("failed finding spec for kind: %s", groupVersionKind.Kind)
	}

	builtinType := builtin.GetType(groupVersionKind.Kind)
	if builtinType != nil {
		obj, err := builtinType.ParseJSON(jsonChunk)
		if err != nil {
			return nil, err
		}
		objMeta := builtinType.ExtractObject(obj)
		key := resource.FullNameFromNamespaceAndName(objMeta.GetNamespace(), objMeta.GetName())
		return &fileResource{
			spec: spec,
			sha:  sha1.Sum(yamlChunk),
			entry: converter.Entry{
				Metadata: resource.Metadata{
					CreateTime:  objMeta.GetCreationTimestamp().Time,
					Labels:      objMeta.GetLabels(),
					Annotations: objMeta.GetAnnotations(),
				},
				Key:      key,
				Resource: builtinType.ExtractResource(obj),
			},
		}, nil
	}

	// No built-in processor for this type. Use dynamic processing via unstructured...

	u := &unstructured.Unstructured{}
	if err := json.Unmarshal(jsonChunk, u); err != nil {
		return nil, err
	}
	if empty(u) {
		return nil, nil
	}

	key := resource.FullNameFromNamespaceAndName(u.GetNamespace(), u.GetName())
	entries, err := dynamic.ConvertAndLog(s.config, *spec, key, u.GetResourceVersion(), u)
	if err != nil {
		return nil, err
	}

	if len(entries) == 0 {
		return nil, fmt.Errorf("did not receive any entries from converter: kind=%v, key=%v, rv=%s",
			u.GetKind(), key, u.GetResourceVersion())
	}

	// TODO(nmittler): Will there ever be > 1 entries?
	return &fileResource{
		spec:  spec,
		sha:   sha1.Sum(yamlChunk),
		entry: entries[0],
	}, nil
}

// Check if the parsed resource is empty
func empty(r *unstructured.Unstructured) bool {
	if r.Object == nil || len(r.Object) == 0 {
		return true
	}
	return false
}
