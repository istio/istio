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
	yamlv3 "gopkg.in/yaml.v3"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	kubeJson "k8s.io/apimachinery/pkg/runtime/serializer/json"
	"k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/client-go/tools/cache"

	kubeyaml2 "istio.io/istio/pilot/pkg/config/file/util/kubeyaml"
	"istio.io/istio/pilot/pkg/config/memory"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/config"
	kube2 "istio.io/istio/pkg/config/legacy/source/kube"
	"istio.io/istio/pkg/config/resource"
	"istio.io/istio/pkg/config/schema/collection"
	schemaresource "istio.io/istio/pkg/config/schema/resource"
	"istio.io/istio/pkg/kube"
	"istio.io/pkg/log"
)

var (
	inMemoryKubeNameDiscriminator int64
	scope                         = log.RegisterScope("file", "File client messages", 0)
)

// KubeSource is an in-memory source implementation that can handle K8s style resources.
type KubeSource struct {
	mu sync.Mutex

	name      string
	schemas   *collection.Schemas
	inner     model.ConfigStore
	defaultNs resource.Namespace

	versionCtr int64
	shas       map[kubeResourceKey]resourceSha
	byFile     map[string]map[kubeResourceKey]config.GroupVersionKind
}

func (s *KubeSource) Schemas() collection.Schemas {
	return *s.schemas
}

func (s *KubeSource) Get(typ config.GroupVersionKind, name, namespace string) *config.Config {
	return s.inner.Get(typ, name, namespace)
}

func (s *KubeSource) List(typ config.GroupVersionKind, namespace string) ([]config.Config, error) {
	return s.inner.List(typ, namespace)
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

func (s *KubeSource) SetWatchErrorHandler(f func(r *cache.Reflector, err error)) error {
	panic("implement me")
}

func (s *KubeSource) HasSynced() bool {
	return true
}

type resourceSha [sha256.Size]byte

type kubeResource struct {
	// resource *resource.Instance
	config *config.Config
	schema collection.Schema
	sha    resourceSha
}

func (r *kubeResource) newKey() kubeResourceKey {
	return kubeResourceKey{
		kind:     r.schema.Resource().Kind(),
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

	return &KubeSource{
		name:    name,
		schemas: &schemas,
		inner:   memory.MakeSkipValidation(schemas),
		shas:    make(map[kubeResourceKey]resourceSha),
		byFile:  make(map[string]map[kubeResourceKey]config.GroupVersionKind),
	}
}

// SetDefaultNamespace enables injecting a default namespace for resources where none is already specified
func (s *KubeSource) SetDefaultNamespace(defaultNs resource.Namespace) {
	s.defaultNs = defaultNs
}

// Clear the contents of this source
func (s *KubeSource) Clear() {
	s.versionCtr = 0
	s.shas = make(map[kubeResourceKey]resourceSha)
	s.byFile = make(map[string]map[kubeResourceKey]config.GroupVersionKind)
	s.inner = memory.MakeSkipValidation(*s.schemas)
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
	newKeys := make(map[kubeResourceKey]config.GroupVersionKind)

	for _, r := range resources {
		key := r.newKey()

		oldSha, found := s.shas[key]
		if !found || oldSha != r.sha {
			s.versionCtr++
			r.config.ResourceVersion = fmt.Sprintf("v%d", s.versionCtr)
			scope.Debug("KubeSource.ApplyContent: Set: ", r.schema.Name(), r.fullName())
			// apply is idempotent, but configstore is not, thus the odd logic here
			_, err := s.inner.Update(*r.config)
			if err != nil {
				_, err = s.inner.Create(*r.config)
				if err != nil {
					return fmt.Errorf("cannot store config %s/%s %s from reader: %s",
						r.schema.Resource().Version(), r.schema.Resource().Kind(), r.fullName(), err)
				}
			}
			s.shas[key] = r.sha
		}
		newKeys[key] = r.schema.Resource().GroupVersionKind()
		if oldKeys != nil {
			scope.Debug("KubeSource.ApplyContent: Delete: ", r.schema.Name(), key)
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

// RemoveContent removes the content for the given name
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
	var resources []kubeResource
	var errs error

	reader := bufio.NewReader(strings.NewReader(yamlText))
	decoder := kubeyaml2.NewYAMLReader(reader)
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
			scope.Debugf("Failed to parse yamlText chunk: %v", yamlText)
			errs = multierror.Append(errs, e)
			break
		}

		chunk := bytes.TrimSpace(doc)
		r, err := s.parseChunk(r, name, lineNum, chunk)
		if err != nil {
			var uerr *unknownSchemaError
			if errors.As(err, &uerr) {
				// Note the error to the debug log but continue
				scope.Debugf("skipping unknown yaml chunk %s: %s", name, uerr.Error())
			} else {
				e := fmt.Errorf("error processing %s[%d]: %v", name, chunkCount, err)
				scope.Warnf("%v - skipping", e)
				scope.Debugf("Failed to parse yaml chunk: %v", string(chunk))
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

	// Cannot create new instance. This occurs because while newer types do not implement proto.Message,
	// this legacy code only supports proto.Messages.
	// Note: while NewInstance can be slightly modified to not return error here, the rest of the code
	// still requires a proto.Message so it won't work without completely refactoring galley/
	_, e := schema.Resource().NewInstance()
	cannotHandleProto := e != nil
	if cannotHandleProto {
		return kubeResource{}, &unknownSchemaError{
			group:   groupVersionKind.Group,
			version: groupVersionKind.Version,
			kind:    groupVersionKind.Kind,
		}
	}

	runtimeScheme := runtime.NewScheme()
	codecs := serializer.NewCodecFactory(runtimeScheme)
	deserializer := codecs.UniversalDeserializer()
	obj, err := kube.IstioScheme.New(schema.Resource().GroupVersionKind().Kubernetes())
	if err != nil {
		return kubeResource{}, fmt.Errorf("failed to initialize interface for built-in type: %v", err)
	}
	_, _, err = deserializer.Decode(jsonChunk, nil, obj)
	if err != nil {
		return kubeResource{}, fmt.Errorf("failed parsing JSON for built-in type: %v", err)
	}
	objMeta, ok := obj.(metav1.Object)
	if !ok {
		return kubeResource{}, errors.New("failed to assert type of object metadata")
	}

	// If namespace is blank and we have a default set, fill in the default
	// (This mirrors the behavior if you kubectl apply a resource without a namespace defined)
	// Don't do this for cluster scoped resources
	if !schema.Resource().IsClusterScoped() {
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

	pos := kube2.Position{Filename: name, Line: lineNum}
	c, err := ToConfig(objMeta, schema, &pos, fieldMap)
	if err != nil {
		return kubeResource{}, err
	}
	return kubeResource{
		schema: schema,
		sha:    sha256.Sum256(yamlChunk),
		config: c,
	}, nil
}

const (
	FieldMapKey  = "istiofilefieldmap"
	ReferenceKey = "istiosource"
)

// ToConfig converts the given object and proto to a config.Config
func ToConfig(object metav1.Object, schema collection.Schema, source resource.Reference, fieldMap map[string]int) (*config.Config, error) {
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

func TranslateObject(obj *unstructured.Unstructured, domainSuffix string, schema collection.Schema) *config.Config {
	mv2, err := schema.Resource().NewInstance()
	if err != nil {
		panic(err)
	}
	if spec, ok := obj.UnstructuredContent()["spec"]; ok {
		err = runtime.DefaultUnstructuredConverter.FromUnstructured(spec.(map[string]interface{}), mv2)
	} else {
		err = runtime.DefaultUnstructuredConverter.FromUnstructured(obj.UnstructuredContent(), mv2)
	}
	if err != nil {
		panic(err)
	}

	m := obj
	return &config.Config{
		Meta: config.Meta{
			GroupVersionKind: config.GroupVersionKind{
				Group:   m.GetObjectKind().GroupVersionKind().Group,
				Version: m.GetObjectKind().GroupVersionKind().Version,
				Kind:    m.GetObjectKind().GroupVersionKind().Kind,
			},
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
