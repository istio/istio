/*
 Copyright Istio Authors

 Licensed under the Apache License, Version 2.0 (the "License");
 you may not use this file except in compliance with the License.
 You may obtain a copy of the License at

     http://www.apache.org/licenses/LICENSE-2.0

 Unless required by applicable law or agreed to in writing, software
 distributed under the License is distributed on an "AS IS" BASIS,
 WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 See the License for the specific language governing permissions and
 limitations under the License.
*/

package file
import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"

	"github.com/hashicorp/go-multierror"
	yamlv3 "gopkg.in/yaml.v3" // nolint: depguard // needed for line numbers
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	kubeJson "k8s.io/apimachinery/pkg/runtime/serializer/json"
	"k8s.io/apimachinery/pkg/util/yaml"

	kubeyaml2 "istio.io/istio/pilot/pkg/config/file/util/kubeyaml"
	"istio.io/istio/pilot/pkg/config/memory"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/config"
	legacykube "istio.io/istio/pkg/config/analysis/legacy/source/kube"
	"istio.io/istio/pkg/config/resource"
	"istio.io/istio/pkg/config/schema/collection"
	sresource "istio.io/istio/pkg/config/schema/resource"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/log"
	"istio.io/istio/pkg/slices"
)

var (
	inMemoryKubeNameDiscriminator int64
	scope                         = log.RegisterScope("file", "File client messages")
)

// KubeSource is an in-memory source implementation that can handle K8s style resources.
type KubeSource struct {
	mu sync.Mutex

	name      string
	schemas   *collection.Schemas
	inner     model.ConfigStore
	defaultNs resource.Namespace

	shas   map[kubeResourceKey]resourceSha
	byFile map[string]map[kubeResourceKey]config.GroupVersionKind

	// If meshConfig.DiscoverySelectors are specified, the namespacesFilter tracks the namespaces this controller watches.
	namespacesFilter func(obj interface{}) bool

	// Cached decoder components to avoid repeated allocations in hot paths
	runtimeScheme *runtime.Scheme
	deserializer  runtime.Decoder

	// Pool reusable readers to reduce allocations for large file parsing
	readerPool *sync.Pool
}

func (s *KubeSource) Schemas() collection.Schemas {
	return *s.schemas
}

func (s *KubeSource) Get(typ config.GroupVersionKind, name, namespace string) *config.Config {
	return s.inner.Get(typ, name, namespace)
}

func (s *KubeSource) List(typ config.GroupVersionKind, namespace string) []config.Config {
	configs := s.inner.List(typ, namespace)
	if s.namespacesFilter != nil {
		return slices.Filter(configs, func(c config.Config) bool {
			return s.namespacesFilter(c)
		})
	}
	return configs
}

func (s *KubeSource) Create(config config.Config) (revision string, err error) {
	return s.inner.Create(config)
}

func (s *KubeSource) Update(config config.Config) (newRevision string, err error) {
	return s.inner.Update(config)
}

func (s *KubeSource) UpdateStatus(config config.Config) (newRevision string, err error) {
	return s.inner.UpdateStatus(config)
}

func (s *KubeSource) Patch(orig config.Config, patchFn config.PatchFunc) (string, error) {
	return s.inner.Patch(orig, patchFn)
}

func (s *KubeSource) Delete(typ config.GroupVersionKind, name, namespace string, resourceVersion *string) error {
	return s.inner.Delete(typ, name, namespace, resourceVersion)
}

func (s *KubeSource) RegisterEventHandler(kind config.GroupVersionKind, handler model.EventHandler) {
	panic("implement me")
}

func (s *KubeSource) Run(stop <-chan struct{}) {
}

func (s *KubeSource) HasSynced() bool {
	return true
}

type resourceSha [sha256.Size]byte

type kubeResource struct {
	// resource *resource.Instance
	config *config.Config
	schema sresource.Schema
	sha    resourceSha
}

func (r *kubeResource) newKey() kubeResourceKey {
	return kubeResourceKey{
		kind:     r.schema.Kind(),
		fullName: r.fullName(),
	}
}

func (r *kubeResource) fullName() resource.FullName {
	return resource.NewFullName(resource.Namespace(r.config.Namespace),
		resource.LocalName(r.config.Name))
}

type kubeResourceKey struct {
	fullName resource.FullName
	kind     string
}

var _ model.ConfigStore = &KubeSource{}

// NewKubeSource returns a new in-memory Source that works with Kubernetes resources.
func NewKubeSource(schemas collection.Schemas) *KubeSource {
	name := fmt.Sprintf("kube-inmemory-%d", inMemoryKubeNameDiscriminator)
	inMemoryKubeNameDiscriminator++

	runtimeScheme := runtime.NewScheme()
	codecs := serializer.NewCodecFactory(runtimeScheme)
	deserializer := codecs.UniversalDeserializer()

	return &KubeSource{
		name:          name,
		schemas:       &schemas,
		inner:         memory.MakeSkipValidation(schemas),
		shas:          make(map[kubeResourceKey]resourceSha),
		byFile:        make(map[string]map[kubeResourceKey]config.GroupVersionKind),
		runtimeScheme: runtimeScheme,
		deserializer:  deserializer,
		readerPool:    &sync.Pool{New: func() any { return bufio.NewReaderSize(nil, 512*1024) }},
	}
}

// SetDefaultNamespace enables injecting a default namespace for resources where none is already specified
func (s *KubeSource) SetDefaultNamespace(defaultNs resource.Namespace) {
	s.defaultNs = defaultNs
}

// SetNamespacesFilter enables filtering the namespaces this controller watches.
func (s *KubeSource) SetNamespacesFilter(namespacesFilter func(obj interface{}) bool) {
	s.namespacesFilter = namespacesFilter
}

// Clear the contents of this source
func (s *KubeSource) Clear() {
	s.shas = make(map[kubeResourceKey]resourceSha)
	s.byFile = make(map[string]map[kubeResourceKey]config.GroupVersionKind)
	s.inner = memory.MakeSkipValidation(*s.schemas)
}

// ContentNames returns the names known to this source.
func (s *KubeSource) ContentNames() map[string]struct{} {
	result := make(map[string]struct{})
	s.mu.Lock()
	defer s.mu.Unlock()
	for n := range s.byFile {
		result[n] = struct{}{}
	}

	return result
}

// ApplyContentReader applies the given YAML content from an io.Reader to this source.
// Content is tracked with the given name; repeated calls with the same name overwrite/remove prior content.
// Returns an error if any were encountered, but that still may represent a partial success.
func (s *KubeSource) ApplyContentReader(name string, r io.Reader) error {
	// Parse without holding the write lock to minimize contention
	// Use a pooled larger buffer to reduce read syscalls and allocations for large inputs
	br := s.readerPool.Get().(*bufio.Reader)
	br.Reset(r)
	resources, parseErrs := s.parseContentReader(s.schemas, name, br)
	s.readerPool.Put(br)
	return s.applyContent(name, resources, parseErrs)
}

// ApplyContent applies the given yamltext to this source. The content is tracked with the given name. If ApplyContent
// gets called multiple times with the same name, the contents applied by the previous incarnation will be overwritten
// or removed, depending on the new content.
// Returns an error if any were encountered, but that still may represent a partial success
func (s *KubeSource) ApplyContent(name, yamlText string) error {
	// Parse without holding the write lock to minimize contention
	resources, parseErrs := s.parseContent(s.schemas, name, yamlText)
	return s.applyContent(name, resources, parseErrs)
}

func (s *KubeSource) applyContent(name string, resources []kubeResource, parseErrs error) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	oldKeys := s.byFile[name]
	newKeys := make(map[kubeResourceKey]config.GroupVersionKind, len(resources))

	for _, r := range resources {
		key := r.newKey()

		oldSha, found := s.shas[key]
		if !found || oldSha != r.sha {
			scope.Debugf("KubeSource.ApplyContent: Set: %v/%v", r.schema.GroupVersionKind(), r.fullName())
			// apply is idempotent, but configstore is not, thus the odd logic here
			_, err := s.inner.Update(*r.config)
			if err != nil {
				_, err = s.inner.Create(*r.config)
				if err != nil {
					return fmt.Errorf("cannot store config %s/%s %s from reader: %s",
						r.schema.Version(), r.schema.Kind(), r.fullName(), err)
				}
			}
			s.shas[key] = r.sha
		}
		newKeys[key] = r.schema.GroupVersionKind()
		if oldKeys != nil {
			scope.Debugf("KubeSource.ApplyContent: Delete: %v/%v", r.schema.GroupVersionKind(), key)
			delete(oldKeys, key)
		}
	}

	for k, col := range oldKeys {
		empty := ""
		err := s.inner.Delete(col, k.fullName.Name.String(), k.fullName.Namespace.String(), &empty)
		if err != nil {
			scope.Errorf("encountered unexpected error removing resource from filestore: %s", err)
		}
	}
	s.byFile[name] = newKeys

	if parseErrs != nil {
		return fmt.Errorf("errors parsing content %q: %v", name, parseErrs)
	}
	return nil
}

func (s *KubeSource) RemoveContent(name string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	keys := s.byFile[name]
	if keys != nil {
		for key, col := range keys {
			empty := ""
			err := s.inner.Delete(col, key.fullName.Name.String(), key.fullName.Namespace.String(), &empty)
			if err != nil {
				scope.Errorf("encountered unexpected error removing resource from filestore: %s", err)
			}
			delete(s.shas, key)
		}

		delete(s.byFile, name)
	}
}

func (s *KubeSource) parseContent(r *collection.Schemas, name, yamlText string) ([]kubeResource, error) {
	const readerBufSize = 512 * 1024
	return s.parseContentReader(r, name, bufio.NewReaderSize(strings.NewReader(yamlText), readerBufSize))
}

func (s *KubeSource) parseContentReader(r *collection.Schemas, name string, reader *bufio.Reader) ([]kubeResource, error) {
	var (
		resourcesMu sync.Mutex
		resources   []kubeResource
		errsMu      sync.Mutex
		errs        error
	)

	decoder := kubeyaml2.NewYAMLReader(reader)
	type job struct {
		line  int
		chunk []byte
		idx   int
	}
	jobs := make(chan job, 64)
	const workers = 4
	var wg sync.WaitGroup
	wg.Add(workers)
	for w := 0; w < workers; w++ {
		go func() {
			defer wg.Done()
			for j := range jobs {
				chunkResources, err := s.parseChunk(r, name, j.line, j.chunk)
				if err != nil {
					var uerr *unknownSchemaError
					if errors.As(err, &uerr) {
						scope.Debugf("skipping unknown yaml chunk %s: %s", name, uerr.Error())
						continue
					}
					e := fmt.Errorf("error processing %s[%d]: %v", name, j.idx, err)
					scope.Warnf("%v - skipping", e)
					scope.Debugf("Failed to parse yaml chunk")
					errsMu.Lock()
					errs = multierror.Append(errs, e)
					errsMu.Unlock()
					continue
				}
				resourcesMu.Lock()
				resources = append(resources, chunkResources...)
				resourcesMu.Unlock()
			}
		}()
	}

	chunkCount := -1
	for {
		chunkCount++
		doc, lineNum, err := decoder.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			e := fmt.Errorf("error reading documents in %s[%d]: %v", name, chunkCount, err)
			scope.Warnf("%v - skipping", e)
			scope.Debug("Failed to parse yamlText chunk")
			errsMu.Lock()
			errs = multierror.Append(errs, e)
			errsMu.Unlock()
			break
		}
		chunk := bytes.TrimSpace(doc)
		if len(chunk) == 0 {
			continue
		}
		jobs <- job{line: lineNum, chunk: chunk, idx: chunkCount}
	}
	close(jobs)
	wg.Wait()
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

func (s *KubeSource) parseChunk(r *collection.Schemas, name string, lineNum int, yamlChunk []byte) ([]kubeResource, error) {
	resources := make([]kubeResource, 0)
	// Convert to JSON once
	jsonChunk, err := yaml.ToJSON(yamlChunk)
	if err != nil {
		return resources, fmt.Errorf("failed converting YAML to JSON: %v", err)
	}
	// ignore null json
	if len(jsonChunk) == 0 || bytes.Equal(jsonChunk, []byte("null")) {
		return resources, nil
	}
	// Interpret GVK from JSON
	groupVersionKind, err := kubeJson.DefaultMetaFactory.Interpret(jsonChunk)
	if err != nil {
		return resources, fmt.Errorf("failed interpreting jsonChunk: %v", err)
	}

	if groupVersionKind.Kind == "List" {
		resourceChunks, err := extractResourceChunksFromListYamlChunk(yamlChunk)
		if err != nil {
			return resources, fmt.Errorf("failed extracting resource chunks from list yaml chunk: %v", err)
		}
		for _, resourceChunk := range resourceChunks {
			lr, err := s.parseChunk(r, name, resourceChunk.lineNum+lineNum, resourceChunk.yamlChunk)
			if err != nil {
				return resources, fmt.Errorf("failed parsing resource chunk: %w", err)
			}
			resources = append(resources, lr...)
		}
		return resources, nil
	}

	if groupVersionKind.Empty() {
		return resources, fmt.Errorf("unable to parse resource with no group, version and kind")
	}

	schema, found := r.FindByGroupVersionAliasesKind(sresource.FromKubernetesGVK(groupVersionKind))

	if !found {
		return resources, &unknownSchemaError{
			group:   groupVersionKind.Group,
			version: groupVersionKind.Version,
			kind:    groupVersionKind.Kind,
		}
	}

	// Cannot create new instance. This occurs because while newer types do not implement proto.Message,
	// this legacy code only supports proto.Messages.
	// Note: while NewInstance can be slightly modified to not return error here, the rest of the code
	// still requires a proto.Message so it won't work without completely refactoring galley/
	_, e := schema.NewInstance()
	cannotHandleProto := e != nil
	if cannotHandleProto {
		return resources, &unknownSchemaError{
			group:   groupVersionKind.Group,
			version: groupVersionKind.Version,
			kind:    groupVersionKind.Kind,
		}
	}

	obj, err := kube.IstioScheme.New(schema.GroupVersionKind().Kubernetes())
	if err != nil {
		return resources, fmt.Errorf("failed to initialize interface for built-in type: %v", err)
	}
	_, _, err = s.deserializer.Decode(jsonChunk, nil, obj)
	if err != nil {
		return resources, fmt.Errorf("failed parsing JSON for built-in type: %v", err)
	}
	objMeta, ok := obj.(metav1.Object)
	if !ok {
		return resources, errors.New("failed to assert type of object metadata")
	}

	// If namespace is blank and we have a default set, fill in the default
	// (This mirrors the behavior if you kubectl apply a resource without a namespace defined)
	// Don't do this for cluster scoped resources
	if !schema.IsClusterScoped() {
		if objMeta.GetNamespace() == "" && s.defaultNs != "" {
			scope.Debugf("KubeSource.parseChunk: namespace not specified for %q, using %q", objMeta.GetName(), s.defaultNs)
			objMeta.SetNamespace(string(s.defaultNs))
		}
	} else {
		// Clear the namespace if there is any specified.
		objMeta.SetNamespace("")
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

	pos := legacykube.Position{Filename: name, Line: lineNum}
	c, err := ToConfig(objMeta, schema, &pos, fieldMap)
	if err != nil {
		return resources, err
	}
	return []kubeResource{
		{
			schema: schema,
			sha:    sha256.Sum256(yamlChunk),
			config: c,
		},
	}, nil
}

type resourceYamlChunk struct {
	lineNum   int
	yamlChunk []byte
}

func extractResourceChunksFromListYamlChunk(chunk []byte) ([]resourceYamlChunk, error) {
	chunks := make([]resourceYamlChunk, 0)
	yamlChunkNode := yamlv3.Node{}
	err := yamlv3.Unmarshal(chunk, &yamlChunkNode)
	if err != nil {
		return nil, fmt.Errorf("failed parsing yamlChunk: %v", err)
	}
	if len(yamlChunkNode.Content) == 0 {
		return nil, fmt.Errorf("failed parsing yamlChunk: no content")
	}
	yamlNode := yamlChunkNode.Content[0]
	var itemsInd int
	for ; itemsInd < len(yamlNode.Content); itemsInd++ {
		if yamlNode.Content[itemsInd].Kind == yamlv3.ScalarNode && yamlNode.Content[itemsInd].Value == "items" {
			itemsInd++
			break
		}
	}
	if itemsInd >= len(yamlNode.Content) || yamlNode.Content[itemsInd].Kind != yamlv3.SequenceNode {
		return nil, fmt.Errorf("failed parsing yamlChunk: malformed items field")
	}
	for _, n := range yamlNode.Content[itemsInd].Content {
		if n.Kind != yamlv3.MappingNode {
			return nil, fmt.Errorf("failed parsing yamlChunk: malformed items field")
		}
		resourceChunk, err := yamlv3.Marshal(n)
		if err != nil {
			return nil, fmt.Errorf("failed marshaling yamlChunk: %v", err)
		}
		chunks = append(chunks, resourceYamlChunk{
			lineNum:   n.Line,
			yamlChunk: resourceChunk,
		})
	}
	return chunks, nil
}

const (
	FieldMapKey  = "istiofilefieldmap"
	ReferenceKey = "istiosource"
)

// ToConfig converts the given object and proto to a config.Config
func ToConfig(object metav1.Object, schema sresource.Schema, source resource.Reference, fieldMap map[string]int) (*config.Config, error) {
	m, err := runtime.DefaultUnstructuredConverter.ToUnstructured(object)
	if err != nil {
		return nil, err
	}
	u := &unstructured.Unstructured{Object: m}
	if len(fieldMap) > 0 || source != nil {
		// TODO: populate
		annots := u.GetAnnotations()
		if annots == nil {
			annots = map[string]string{}
		}
		jsonfm, err := json.Marshal(fieldMap)
		if err != nil {
			return nil, err
		}
		annots[FieldMapKey] = string(jsonfm)
		jsonsource, err := json.Marshal(source)
		if err != nil {
			return nil, err
		}
		annots[ReferenceKey] = string(jsonsource)
		u.SetAnnotations(annots)
	}
	result := TranslateObject(u, "", schema)
	return result, nil
}

func TranslateObject(obj *unstructured.Unstructured, domainSuffix string, schema sresource.Schema) *config.Config {
	mv2, err := schema.NewInstance()
	if err != nil {
		panic(err)
	}
	if spec, ok := obj.UnstructuredContent()["spec"]; ok {
		err = runtime.DefaultUnstructuredConverter.FromUnstructured(spec.(map[string]any), mv2)
	} else {
		err = runtime.DefaultUnstructuredConverter.FromUnstructured(obj.UnstructuredContent(), mv2)
	}
	if err != nil {
		panic(err)
	}

	m := obj
	result := &config.Config{
		Meta: config.Meta{
			GroupVersionKind:  schema.GroupVersionKind(),
			UID:               string(m.GetUID()),
			Name:              m.GetName(),
			Namespace:         m.GetNamespace(),
			Labels:            m.GetLabels(),
			Annotations:       m.GetAnnotations(),
			ResourceVersion:   m.GetResourceVersion(),
			CreationTimestamp: m.GetCreationTimestamp().Time,
			OwnerReferences:   m.GetOwnerReferences(),
			Generation:        m.GetGeneration(),
			Domain:            domainSuffix,
		},
		Spec: mv2,
	}

	// attempt to handle status if we know the type
	statusStruct, err := schema.Status()
	if err == nil {
		if status, ok := obj.UnstructuredContent()["status"]; ok {
			if err := runtime.DefaultUnstructuredConverter.FromUnstructured(status.(map[string]any), statusStruct); err != nil {
				scope.Warnf("failed to parse status field: %v", err)
			} else {
				result.Status = statusStruct
			}
		}
	}

	return result
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
