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

package inmemory

import (
	"bytes"
	"crypto/sha1"
	"fmt"
	"sync"

	"github.com/ghodss/yaml"
	kubeJson "k8s.io/apimachinery/pkg/runtime/serializer/json"

	"istio.io/istio/galley/pkg/config/collection"
	"istio.io/istio/galley/pkg/config/event"
	"istio.io/istio/galley/pkg/config/resource"
	"istio.io/istio/galley/pkg/config/schema"
	"istio.io/istio/galley/pkg/config/scope"
	"istio.io/istio/galley/pkg/config/source/inmemory"
	"istio.io/istio/galley/pkg/config/source/kube/rt"
	"istio.io/istio/galley/pkg/config/util/kubeyaml"
)

var inMemoryKubeNameDiscriminator int64

// KubeSource is an in-memory source implementation that can handle K8s style resources.
type KubeSource struct {
	mu sync.Mutex

	name      string
	resources schema.KubeResources
	source    *inmemory.Source

	versionCtr int64
	shas       map[kubeResourceKey]resourceSha
	byFile     map[string]map[kubeResourceKey]collection.Name
}

type resourceSha [sha1.Size]byte

type kubeResource struct {
	entry *resource.Entry
	spec  schema.KubeResource
	sha   resourceSha
}

func (r *kubeResource) newKey() kubeResourceKey {
	return kubeResourceKey{
		kind:     r.spec.Kind,
		fullName: r.entry.Metadata.Name,
	}
}

type kubeResourceKey struct {
	fullName resource.Name
	kind     string
}

var _ event.Source = &KubeSource{}

// NewKubeSource returns a new in-memory Source that works with Kubernetes resources.
func NewKubeSource(resources schema.KubeResources) *KubeSource {
	name := fmt.Sprintf("kube-inmemory-%d", inMemoryKubeNameDiscriminator)
	inMemoryKubeNameDiscriminator++

	s := inmemory.New(resources.Collections())

	return &KubeSource{
		name:      name,
		resources: resources,
		source:    s,
		shas:      make(map[kubeResourceKey]resourceSha),
		byFile:    make(map[string]map[kubeResourceKey]collection.Name),
	}
}

// Start implements processor.Source
func (s *KubeSource) Start() {
	s.source.Start()
}

// Stop implements processor.Source
func (s *KubeSource) Stop() {
	s.source.Stop()
}

// Clear the contents of this source
func (s *KubeSource) Clear() {
	s.versionCtr = 0
	s.shas = make(map[kubeResourceKey]resourceSha)
	s.byFile = make(map[string]map[kubeResourceKey]collection.Name)
	s.source.Clear()
}

// Dispatch implements processor.Source
func (s *KubeSource) Dispatch(h event.Handler) {
	s.source.Dispatch(h)
}

// Get returns the named collection.
func (s *KubeSource) Get(collection collection.Name) *inmemory.Collection {
	return s.source.Get(collection)
}

// ContentNames returns the names known to this source.
func (s *KubeSource) ContentNames() map[string]struct{} {
	s.mu.Lock()
	defer s.mu.Unlock()

	result := make(map[string]struct{})
	for n := range s.byFile {
		result[n] = struct{}{}
	}

	return result
}

// ApplyContent applies the given yamltext to this source. The content is tracked with the given name. If ApplyContent
// gets called multiple times with the same name, the contents applied by the previous incarnation will be overwritten
// or removed, depending on the new content.
func (s *KubeSource) ApplyContent(name, yamlText string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	resources := parseContent(s.resources, name, yamlText)

	oldKeys := s.byFile[name]
	newKeys := make(map[kubeResourceKey]collection.Name)

	for _, r := range resources {
		key := r.newKey()

		oldSha, found := s.shas[key]
		if !found || oldSha != r.sha {
			s.versionCtr++
			r.entry.Metadata.Version = resource.Version(fmt.Sprintf("v%d", s.versionCtr))
			scope.Source.Debuga("KubeSource.ApplyContent: Set: ", r.spec.Collection.Name, r.entry.Metadata.Name)
			s.source.Get(r.spec.Collection.Name).Set(r.entry)
			s.shas[key] = r.sha
		}
		newKeys[key] = r.spec.Collection.Name
		if oldKeys != nil {
			scope.Source.Debuga("KubeSource.ApplyContent: Delete: ", r.spec.Collection.Name, key)
			delete(oldKeys, key)
		}
	}

	for k, col := range oldKeys {
		s.source.Get(col).Remove(k.fullName)
	}
	s.byFile[name] = newKeys

	return nil
}

// RemoveContent removes the content for the given name
func (s *KubeSource) RemoveContent(name string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	keys := s.byFile[name]
	if keys != nil {
		for key, col := range keys {
			s.source.Get(col).Remove(key.fullName)
			delete(s.shas, key)
		}

		delete(s.byFile, name)
	}
}

func parseContent(r schema.KubeResources, name, yamlText string) []kubeResource {
	var resources []kubeResource
	for i, chunk := range kubeyaml.Split([]byte(yamlText)) {
		chunk = bytes.TrimSpace(chunk)

		r, err := parseChunk(r, chunk)
		if err != nil {
			scope.Source.Errorf("Error processing %s[%d]: %v", name, i, err)
			scope.Source.Debugf("Offending Yaml chunk: %v", string(chunk))
			continue
		}

		resources = append(resources, r)
	}
	return resources
}

func parseChunk(r schema.KubeResources, yamlChunk []byte) (kubeResource, error) {
	// Convert to JSON
	jsonChunk, err := yaml.YAMLToJSON(yamlChunk)
	if err != nil {
		return kubeResource{}, fmt.Errorf("failed converting YAML to JSON")
	}

	// Peek at the beginning of the JSON to
	groupVersionKind, err := kubeJson.DefaultMetaFactory.Interpret(jsonChunk)
	if err != nil {
		return kubeResource{}, fmt.Errorf("failed interpreting jsonChunk: %v", err)
	}

	resourceSpec, found := r.Find(groupVersionKind.Group, groupVersionKind.Kind)
	if !found {
		return kubeResource{}, fmt.Errorf("failed finding spec for group/kind: %s/%s", groupVersionKind.Group, groupVersionKind.Kind)
	}

	t := rt.DefaultProvider().GetAdapter(resourceSpec)
	obj, err := t.ParseJSON(jsonChunk)
	if err != nil {
		return kubeResource{}, fmt.Errorf("failed parsing JSON for built-in type: %v", err)
	}
	objMeta := t.ExtractObject(obj)

	item, err := t.ExtractResource(obj)
	if err != nil {
		return kubeResource{}, err
	}

	return kubeResource{
		spec:  resourceSpec,
		sha:   sha1.Sum(yamlChunk),
		entry: rt.ToResourceEntry(objMeta, &resourceSpec, item),
	}, nil
}
