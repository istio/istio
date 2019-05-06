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
	"context"
	"crypto/sha1"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"sync"
	"syscall"

	"github.com/ghodss/yaml"

	kubeMeta "istio.io/istio/galley/pkg/metadata/kube"
	"istio.io/istio/galley/pkg/runtime"
	"istio.io/istio/galley/pkg/runtime/resource"
	"istio.io/istio/galley/pkg/source/kube/builtin"
	"istio.io/istio/galley/pkg/source/kube/dynamic"
	"istio.io/istio/galley/pkg/source/kube/dynamic/converter"
	"istio.io/istio/galley/pkg/source/kube/log"
	"istio.io/istio/galley/pkg/source/kube/schema"
	"istio.io/istio/galley/pkg/util"
	"istio.io/istio/pkg/appsignals"

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

	mu sync.RWMutex

	// map to store namespace/name : shas
	shas map[fileResourceKey][sha1.Size]byte

	handler resource.EventHandler

	// map to store kind : bool to indicate whether we need to deal with the resource or not
	kinds map[string]bool

	// fsresource version
	version int64

	worker *util.Worker
}

func (s *source) readFiles(root string) map[fileResourceKey]*fileResource {
	results := map[fileResourceKey]*fileResource{}

	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		result := s.readFile(path, info)
		if len(result) != 0 {
			for _, r := range result {
				results[r.newKey()] = r
			}
		}
		return nil
	})
	if err != nil {
		log.Scope.Errorf("failure during filepath.Walk: %v", err)
	}
	return results
}

func (s *source) readFile(path string, info os.FileInfo) []*fileResource {
	result := make([]*fileResource, 0)
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

	for _, r := range s.parseFile(path, data) {
		if !s.kinds[r.spec.Kind] {
			continue
		}
		result = append(result, r)
	}
	return result
}

func (s *source) initialCheck() {
	newData := s.readFiles(s.root)
	s.mu.Lock()
	defer s.mu.Unlock()
	for k, r := range newData {
		s.process(resource.Added, k, r)
		s.shas[k] = r.sha
	}
	s.handler(resource.FullSyncEvent)
}

func (s *source) reload() {
	newData := s.readFiles(s.root)
	s.mu.Lock()
	defer s.mu.Unlock()
	newShas := map[fileResourceKey][sha1.Size]byte{}
	// Compute the deltas using sha comparisons
	nextVersion := s.version + 1
	for k, r := range newData {
		newShas[k] = r.sha
		sha, exists := s.shas[k]
		if exists && sha != r.sha {
			if s.version != nextVersion {
				s.version = nextVersion
			}
			s.process(resource.Updated, k, r)
		} else if !exists {
			if s.version != nextVersion {
				s.version = nextVersion
			}
			s.process(resource.Added, k, r)
		}
	}
	for k := range s.shas {
		if _, exists := newShas[k]; !exists {
			s.process(resource.Deleted, k, nil)
		}
	}
	s.shas = newShas
}

// Stop implements runtime.Source
func (s *source) Stop() {
	s.worker.Stop()
}

func (s *source) process(eventKind resource.EventKind, key fileResourceKey, r *fileResource) {
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
						FullName:   key.fullName,
					},
					Version: version,
				},
				Item:     r.entry.Resource,
				Metadata: r.entry.Metadata,
			},
		}
	case resource.Deleted:
		spec := kubeMeta.Types.Get(key.kind)
		event = resource.Event{
			Kind: eventKind,
			Entry: resource.Entry{
				ID: resource.VersionedKey{
					Key: resource.Key{
						Collection: spec.Target.Collection,
						FullName:   key.fullName,
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
	return s.worker.Start(nil, func(ctx context.Context) {
		s.handler = handler
		s.initialCheck()
		c := make(chan appsignals.Signal)
		appsignals.Watch(c)

		for {
			select {
			case <-ctx.Done():
				return
			case trigger := <-c:
				if trigger.Signal == syscall.SIGUSR1 {
					log.Scope.Infof("Triggering reload in response to: %v", trigger.Source)
					s.reload()
				}
			}
		}
	})
}

// New returns a File System implementation of runtime.Source.
func New(root string, schema *schema.Instance, config *converter.Config) (runtime.Source, error) {
	fs := &source{
		config:  config,
		root:    root,
		kinds:   map[string]bool{},
		shas:    map[fileResourceKey][sha1.Size]byte{},
		worker:  util.NewWorker("fs source", log.Scope),
		version: 0,
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

func (r *fileResource) newKey() fileResourceKey {
	return fileResourceKey{
		kind:     r.spec.Kind,
		fullName: r.entry.Key,
	}
}

type fileResourceKey struct {
	fullName resource.FullName
	kind     string
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
