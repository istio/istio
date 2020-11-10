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

package inmemory

import (
	"bufio"
	"bytes"
	"crypto/sha1"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"

	"github.com/hashicorp/go-multierror"
	yamlv3 "gopkg.in/yaml.v3"
	kubeJson "k8s.io/apimachinery/pkg/runtime/serializer/json"
	"k8s.io/apimachinery/pkg/util/yaml"

	"istio.io/istio/galley/pkg/config/scope"
	"istio.io/istio/galley/pkg/config/source/inmemory"
	"istio.io/istio/galley/pkg/config/source/kube/rt"
	"istio.io/istio/galley/pkg/config/util/kubeyaml"
	"istio.io/istio/pkg/config/event"
	"istio.io/istio/pkg/config/resource"
	"istio.io/istio/pkg/config/schema/collection"
	schemaresource "istio.io/istio/pkg/config/schema/resource"
)

var inMemoryKubeNameDiscriminator int64

// KubeSource is an in-memory source implementation that can handle K8s style resources.
type KubeSource struct {
	mu sync.Mutex

	name      string
	schemas   *collection.Schemas
	source    *inmemory.Source
	defaultNs resource.Namespace

	versionCtr int64
	shas       map[kubeResourceKey]resourceSha
	byFile     map[string]map[kubeResourceKey]collection.Name
}

type resourceSha [sha1.Size]byte

type kubeResource struct {
	resource *resource.Instance
	schema   collection.Schema
	sha      resourceSha
}

func (r *kubeResource) newKey() kubeResourceKey {
	return kubeResourceKey{
		kind:     r.schema.Resource().Kind(),
		fullName: r.resource.Metadata.FullName,
	}
}

type kubeResourceKey struct {
	fullName resource.FullName
	kind     string
}

var _ event.Source = &KubeSource{}

// NewKubeSource returns a new in-memory Source that works with Kubernetes resources.
func NewKubeSource(schemas collection.Schemas) *KubeSource {
	name := fmt.Sprintf("kube-inmemory-%d", inMemoryKubeNameDiscriminator)
	inMemoryKubeNameDiscriminator++

	s := inmemory.New(schemas)

	return &KubeSource{
		name:    name,
		schemas: &schemas,
		source:  s,
		shas:    make(map[kubeResourceKey]resourceSha),
		byFile:  make(map[string]map[kubeResourceKey]collection.Name),
	}
}

// SetDefaultNamespace enables injecting a default namespace for resources where none is already specified
func (s *KubeSource) SetDefaultNamespace(defaultNs resource.Namespace) {
	s.defaultNs = defaultNs
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
// Returns an error if any were encountered, but that still may represent a partial success
func (s *KubeSource) ApplyContent(name, yamlText string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// We hold off on dealing with parseErr until the end, since partial success is possible
	resources, parseErrs := s.parseContent(s.schemas, name, yamlText)

	oldKeys := s.byFile[name]
	newKeys := make(map[kubeResourceKey]collection.Name)

	for _, r := range resources {
		key := r.newKey()

		oldSha, found := s.shas[key]
		if !found || oldSha != r.sha {
			s.versionCtr++
			r.resource.Metadata.Version = resource.Version(fmt.Sprintf("v%d", s.versionCtr))
			scope.Source.Debug("KubeSource.ApplyContent: Set: ", r.schema.Name(), r.resource.Metadata.FullName)
			s.source.Get(r.schema.Name()).Set(r.resource)
			s.shas[key] = r.sha
		}
		newKeys[key] = r.schema.Name()
		if oldKeys != nil {
			scope.Source.Debug("KubeSource.ApplyContent: Delete: ", r.schema.Name(), key)
			delete(oldKeys, key)
		}
	}

	for k, col := range oldKeys {
		s.source.Get(col).Remove(k.fullName)
	}
	s.byFile[name] = newKeys

	if parseErrs != nil {
		return fmt.Errorf("errors parsing content %q: %v", name, parseErrs)
	}
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

func (s *KubeSource) parseContent(r *collection.Schemas, name, yamlText string) ([]kubeResource, error) {
	var resources []kubeResource
	var errs error

	reader := bufio.NewReader(strings.NewReader(yamlText))
	decoder := kubeyaml.NewYAMLReader(reader)
	chunkCount := -1

	for {
		chunkCount++
		doc, lineNum, err := decoder.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			e := fmt.Errorf("error reading documents in %s[%d]: %v", name, chunkCount, err)
			scope.Source.Warnf("%v - skipping", e)
			scope.Source.Debugf("Failed to parse yamlText chunk: %v", yamlText)
			errs = multierror.Append(errs, e)
			break
		}

		chunk := bytes.TrimSpace(doc)
		r, err := s.parseChunk(r, name, lineNum, chunk)
		if err != nil {
			var uerr *unknownSchemaError
			if errors.As(err, &uerr) {
				// Note the error to the debug log but continue
				scope.Source.Debugf("skipping unknown yaml chunk %s: %s", name, uerr.Error())
			} else {
				e := fmt.Errorf("error processing %s[%d]: %v", name, chunkCount, err)
				scope.Source.Warnf("%v - skipping", e)
				scope.Source.Debugf("Failed to parse yaml chunk: %v", string(chunk))
				errs = multierror.Append(errs, e)
			}
			continue
		}
		resources = append(resources, r)
	}

	return resources, errs
}

// unknownSchemaError represents a schema was not found for a group+version+kind.
type unknownSchemaError struct {
	group   string
	version string
	kind    string
}

func (e unknownSchemaError) Error() string {
	return fmt.Sprintf("failed finding schema for group/version/kind: %s/%s/%s", e.group, e.version, e.kind)
}

func (s *KubeSource) parseChunk(r *collection.Schemas, name string, lineNum int, yamlChunk []byte) (kubeResource, error) {
	// Convert to JSON
	jsonChunk, err := yaml.ToJSON(yamlChunk)
	if err != nil {
		return kubeResource{}, fmt.Errorf("failed converting YAML to JSON: %v", err)
	}

	// Peek at the beginning of the JSON to
	groupVersionKind, err := kubeJson.DefaultMetaFactory.Interpret(jsonChunk)
	if err != nil {
		return kubeResource{}, fmt.Errorf("failed interpreting jsonChunk: %v", err)
	}

	if groupVersionKind.Empty() {
		return kubeResource{}, fmt.Errorf("unable to parse resource with no group, version and kind")
	}

	schema, found := r.FindByGroupVersionKind(schemaresource.FromKubernetesGVK(groupVersionKind))
	if !found {
		return kubeResource{}, &unknownSchemaError{
			group:   groupVersionKind.Group,
			version: groupVersionKind.Version,
			kind:    groupVersionKind.Kind,
		}
	}

	t := rt.DefaultProvider().GetAdapter(schema.Resource())
	obj, err := t.ParseJSON(jsonChunk)
	if err != nil {
		return kubeResource{}, fmt.Errorf("failed parsing JSON for built-in type: %v", err)
	}
	objMeta := t.ExtractObject(obj)

	// If namespace is blank and we have a default set, fill in the default
	// (This mirrors the behavior if you kubectl apply a resource without a namespace defined)
	// Don't do this for cluster scoped resources
	if !schema.Resource().IsClusterScoped() {
		if objMeta.GetNamespace() == "" && s.defaultNs != "" {
			scope.Source.Debugf("KubeSource.parseChunk: namespace not specified for %q, using %q", objMeta.GetName(), s.defaultNs)
			objMeta.SetNamespace(string(s.defaultNs))
		}
	} else {
		// Clear the namespace if there is any specified.
		objMeta.SetNamespace("")
	}

	item, err := t.ExtractResource(obj)
	if err != nil {
		return kubeResource{}, err
	}

	// Build flat map for analyzers if the line JSON object exists, if the YAML text is ill-formed, this will be nil
	fieldMap := make(map[string]int)

	// yamlv3.Node contains information like line number of the node, which will be used with its name to construct the field map
	yamlChunkNode := yamlv3.Node{}
	err = yamlv3.Unmarshal(yamlChunk, &yamlChunkNode)
	if err == nil && len(yamlChunkNode.Content) == 1 {

		// Get the Node that contains all the YAML chunk information
		yamlNode := yamlChunkNode.Content[0]

		BuildFieldPathMap(yamlNode, lineNum, "", fieldMap)
	}

	pos := rt.Position{Filename: name, Line: lineNum}
	return kubeResource{
		schema:   schema,
		sha:      sha1.Sum(yamlChunk),
		resource: rt.ToResource(objMeta, schema, item, &pos, fieldMap),
	}, nil
}

// BuildFieldPathMap builds the flat map for each field of the YAML resource
func BuildFieldPathMap(yamlNode *yamlv3.Node, startLineNum int, curPath string, fieldPathMap map[string]int) {
	// If no content in the node, terminate the DFS search
	if len(yamlNode.Content) == 0 {
		return
	}

	nodeContent := yamlNode.Content
	// Iterate content by a step of 2, because in the content array the value is in the key's next index position
	for i := 0; i < len(nodeContent)-1; i += 2 {
		// Two condition, i + 1 positions have no content, which means they have the format like "key: value", then build the map
		// Or i + 1 has contents, which means "key:\n  value...", then perform one more DFS search
		keyNode := nodeContent[i]
		valueNode := nodeContent[i+1]
		pathKeyForMap := fmt.Sprintf("%s.%s", curPath, keyNode.Value)

		switch {
		case valueNode.Kind == yamlv3.ScalarNode:
			// Can build map because the value node has no content anymore
			// minus one because startLineNum starts at line 1, and yamlv3.Node.line also starts at line 1
			fieldPathMap[fmt.Sprintf("{%s}", pathKeyForMap)] = valueNode.Line + startLineNum - 1

		case valueNode.Kind == yamlv3.MappingNode:
			BuildFieldPathMap(valueNode, startLineNum, pathKeyForMap, fieldPathMap)

		case valueNode.Kind == yamlv3.SequenceNode:
			for j, node := range valueNode.Content {
				pathWithIndex := fmt.Sprintf("%s[%d]", pathKeyForMap, j)

				// Array with values or array with maps
				if node.Kind == yamlv3.ScalarNode {
					fieldPathMap[fmt.Sprintf("{%s}", pathWithIndex)] = node.Line + startLineNum - 1
				} else {
					BuildFieldPathMap(node, startLineNum, pathWithIndex, fieldPathMap)
				}
			}
		}
	}
}
