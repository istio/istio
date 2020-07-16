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
			scope.Source.Debuga("KubeSource.ApplyContent: Set: ", r.schema.Name(), r.resource.Metadata.FullName)
			s.source.Get(r.schema.Name()).Set(r.resource)
			s.shas[key] = r.sha
		}
		newKeys[key] = r.schema.Name()
		if oldKeys != nil {
			scope.Source.Debuga("KubeSource.ApplyContent: Delete: ", r.schema.Name(), key)
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
		doc, lineDoc, lineNum, err := decoder.Read()
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
		lineChunk := bytes.TrimSpace(lineDoc)
		r, err := s.parseChunk(r, name, lineNum, chunk, lineChunk)
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

func (s *KubeSource) parseChunk(r *collection.Schemas, name string, lineNum int, yamlChunk []byte, yamlChunkWithLineNumber []byte) (kubeResource, error) {
	// Convert to JSON
	jsonChunk, err := yaml.ToJSON(yamlChunk)
	if err != nil {
		return kubeResource{}, fmt.Errorf("failed converting YAML to JSON: %v", err)
	}

	// Convert YAML with line numbers as values to a JSON object
	jsonChunkWithLineNumber, err := yaml.ToJSON(yamlChunkWithLineNumber)
	if err != nil {
		jsonChunkWithLineNumber = nil
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
	var fieldMap map[string]int
	if jsonChunkWithLineNumber != nil {
		fieldMap = BuildFieldMap(jsonChunk, jsonChunkWithLineNumber)
	}

	pos := rt.Position{Filename: name, Line: lineNum}
	return kubeResource{
		schema:   schema,
		sha:      sha1.Sum(yamlChunk),
		resource: rt.ToResource(objMeta, schema, item, &pos, fieldMap),
	}, nil
}

// BuildFieldMap builds every field along its JSONPath as key and its line number as value
func BuildFieldMap(yamlJson []byte, yamlJsonWithLineNumber []byte) map[string]int {

	// fieldMap contains paths of each fields of the resource as the key, and its corresponding line number as the value
	fieldMap := make(map[string]int)

	// Unmarshal two JSON objects to obtain keys to build the map
	yamlData := kubeyaml.JsonUnmarshal(yamlJson)
	yamlDataWithLineNumber := kubeyaml.JsonUnmarshal(yamlJsonWithLineNumber)

	DfsBuildMap(yamlData, yamlDataWithLineNumber, fieldMap, "")

	return fieldMap
}

// DfsBuildMap builds the field map with a dfs method to each key in the JSON object
func DfsBuildMap(yamlObj interface{}, yamlObjWithLineNumber interface{}, fieldMap map[string]int, curPath string) {
	var yamlData interface{}
	var lineData interface{}
	yamlData = yamlObj
	lineData = yamlObjWithLineNumber

	// Check interface type, and go to the next step
	switch yamlData.(type) {
	case float64:
		key := curPath
		fieldMap["{" + key + "}"] = int(lineData.(float64))

	case string:
		key := curPath
		fieldMap["{" + key + "}"] = int(lineData.(float64))

	case []interface {}:
		var lineArray []interface{}
		yamlArray := yamlData.([]interface{})

		// Handle edge case for values like "[a,b,c,d,e]" in the command field
		// lineData only has two types: float64 and []interface {}
		if FindFloatInterface(lineData) == "float64" {
			res := FillLineNumberToStruct(yamlArray, lineData.(float64))
			if res == nil {
				res = make([]interface{}, 0)
			}
			lineArray = res.([]interface{})
		}else {
			lineArray = lineData.([]interface{})
		}

		// Append corresponding index to the field in the array
		for i := range yamlArray {
			DfsBuildMap(yamlArray[i], lineArray[i], fieldMap, curPath + "[" + fmt.Sprintf("%d", i) + "]")
		}

	case map[string]interface {}:
		yamlMap := yamlData.(map[string]interface{})

		// Handle edge case for values that has maps in only one line
		var lineMap map[string]interface{}
		if FindFloatInterface(lineData) == "float64" {

			res := FillLineNumberToStruct(yamlMap, lineData.(float64))
			if res == nil {
				res = make(map[string]interface{})
			}
			lineMap = res.(map[string]interface{})
		}else {
			lineMap = lineData.(map[string]interface{})
		}

		// perform map builder to the map's child
		for k := range yamlMap {
			DfsBuildMap(yamlMap[k], lineMap[k], fieldMap, curPath + "." + k)
		}

	default:
		return
	}
}

// FindFloatInterface returns "float64" if the interface is float64 type else returns ""
func FindFloatInterface(p interface{}) string {
	res := ""
	switch p.(type) {
	case float64:
		res = "float64"
	}
	return res
}

// FillLineNumberToStruct returns an object that has the same structure with the input but the values are filled by the input line
func FillLineNumberToStruct(keyObject interface{}, lineNumber float64) interface{} {
	var fieldMap interface{}
	switch keyObject.(type) {
	case int:
		fieldMap = lineNumber

	case float64:
		fieldMap = lineNumber

	case string:
		fieldMap = lineNumber

	case []interface {}:
		a := make([]interface{}, 0)
		for _, v := range keyObject.([]interface{}) {
			a = append(a, FillLineNumberToStruct(v, lineNumber))
		}
		return a

	case map[string]interface {}:
		m := make(map[string]interface{})
		for k, v := range keyObject.(map[string]interface{}) {
			m[k] = FillLineNumberToStruct(v, lineNumber)
		}
		return m
	}
	return fieldMap
}
