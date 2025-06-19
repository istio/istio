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

package config

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
	"time"

	gogojsonpb "github.com/gogo/protobuf/jsonpb" // nolint: depguard
	gogoproto "github.com/gogo/protobuf/proto"   // nolint: depguard
	gogotypes "github.com/gogo/protobuf/types"   // nolint: depguard
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/structpb"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	kubetypes "k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/yaml"

	"istio.io/api/label"
	"istio.io/istio/pilot/pkg/util/protoconv"
	"istio.io/istio/pkg/cluster"
	"istio.io/istio/pkg/maps"
	"istio.io/istio/pkg/ptr"
	"istio.io/istio/pkg/slices"
	"istio.io/istio/pkg/util/gogoprotomarshal"
	"istio.io/istio/pkg/util/protomarshal"
	"istio.io/istio/pkg/util/sets"
)

// Meta is metadata attached to each configuration unit.
// The revision is optional, and if provided, identifies the
// last update operation on the object.
type Meta struct {
	// GroupVersionKind is a short configuration name that matches the content message type
	// (e.g. "route-rule")
	GroupVersionKind GroupVersionKind `json:"type,omitempty"`

	// UID
	UID string `json:"uid,omitempty"`

	// Name is a unique immutable identifier in a namespace
	Name string `json:"name,omitempty"`

	// Namespace defines the space for names (optional for some types),
	// applications may choose to use namespaces for a variety of purposes
	// (security domains, fault domains, organizational domains)
	Namespace string `json:"namespace,omitempty"`

	// Domain defines the suffix of the fully qualified name past the namespace.
	// Domain is not a part of the unique key unlike name and namespace.
	Domain string `json:"domain,omitempty"`

	// Map of string keys and values that can be used to organize and categorize
	// (scope and select) objects.
	Labels map[string]string `json:"labels,omitempty"`

	// Annotations is an unstructured key value map stored with a resource that may be
	// set by external tools to store and retrieve arbitrary metadata. They are not
	// queryable and should be preserved when modifying objects.
	Annotations map[string]string `json:"annotations,omitempty"`

	// ResourceVersion is an opaque identifier for tracking updates to the config registry.
	// The implementation may use a change index or a commit log for the revision.
	// The config client should not make any assumptions about revisions and rely only on
	// exact equality to implement optimistic concurrency of read-write operations.
	//
	// The lifetime of an object of a particular revision depends on the underlying data store.
	// The data store may compactify old revisions in the interest of storage optimization.
	//
	// An empty revision carries a special meaning that the associated object has
	// not been stored and assigned a revision.
	ResourceVersion string `json:"resourceVersion,omitempty"`

	// CreationTimestamp records the creation time
	CreationTimestamp time.Time `json:"creationTimestamp,omitempty"`

	// OwnerReferences allows specifying in-namespace owning objects.
	OwnerReferences []metav1.OwnerReference `json:"ownerReferences,omitempty"`

	// A sequence number representing a specific generation of the desired state. Populated by the system. Read-only.
	Generation int64 `json:"generation,omitempty"`
}

// Config is a configuration unit consisting of the type of configuration, the
// key identifier that is unique per type, and the content represented as a
// protobuf message.
type Config struct {
	Meta

	// Spec holds the configuration object as a gogo protobuf message
	Spec Spec

	// Status holds long-running status.
	Status Status

	// Extra holds additional, non-spec information for internal processing.
	Extra map[string]any
}

type ObjectWithCluster[T any] struct {
	ClusterID cluster.ID
	Object    *T
}

// We can't refer to krt directly without causing an import cycle, but this function
// implements an interface that allows the krt helper to know how to get the object key
func (o ObjectWithCluster[T]) GetObjectKeyable() any {
	if o.Object == nil {
		return nil
	}
	return *o.Object
}

func LabelsInRevision(lbls map[string]string, rev string) bool {
	configEnv, f := lbls[label.IoIstioRev.Name]
	if !f {
		// This is a global object, and always included
		return true
	}
	// If the revision is empty, this means we don't specify a revision, and
	// we should always include it
	if rev == "" {
		return true
	}
	// Otherwise, only return true if revisions equal
	return configEnv == rev
}

func LabelsInRevisionOrTags(lbls map[string]string, rev string, tags sets.Set[string]) bool {
	if LabelsInRevision(lbls, rev) {
		return true
	}
	configEnv := lbls[label.IoIstioRev.Name]
	// Otherwise, only return true if revisions equal
	return tags.Contains(configEnv)
}

func ObjectInRevision(o *Config, rev string) bool {
	return LabelsInRevision(o.Labels, rev)
}

// Spec defines the spec for the config. In order to use below helper methods,
// this must be one of:
// * golang/protobuf Message
// * gogo/protobuf Message
// * Able to marshal/unmarshal using json
type Spec any

func ToProto(s Spec) (*anypb.Any, error) {
	// golang protobuf. Use protoreflect.ProtoMessage to distinguish from gogo
	// golang/protobuf 1.4+ will have this interface. Older golang/protobuf are gogo compatible
	// but also not used by Istio at all.
	if pb, ok := s.(protoreflect.ProtoMessage); ok {
		return protoconv.MessageToAnyWithError(pb)
	}

	// gogo protobuf
	if pb, ok := s.(gogoproto.Message); ok {
		gogoany, err := gogotypes.MarshalAny(pb)
		if err != nil {
			return nil, err
		}
		return &anypb.Any{
			TypeUrl: gogoany.TypeUrl,
			Value:   gogoany.Value,
		}, nil
	}

	js, err := json.Marshal(s)
	if err != nil {
		return nil, err
	}
	pbs := &structpb.Struct{}
	if err := protomarshal.Unmarshal(js, pbs); err != nil {
		return nil, err
	}
	return protoconv.MessageToAnyWithError(pbs)
}

func ToMap(s Spec) (map[string]any, error) {
	js, err := ToJSON(s)
	if err != nil {
		return nil, err
	}

	// Unmarshal from json bytes to go map
	var data map[string]any
	err = json.Unmarshal(js, &data)
	if err != nil {
		return nil, err
	}

	return data, nil
}

func ToRaw(s Spec) (json.RawMessage, error) {
	js, err := ToJSON(s)
	if err != nil {
		return nil, err
	}

	// Unmarshal from json bytes to go map
	return js, nil
}

func ToJSON(s Spec) ([]byte, error) {
	return toJSON(s, false)
}

func ToPrettyJSON(s Spec) ([]byte, error) {
	return toJSON(s, true)
}

func toJSON(s Spec, pretty bool) ([]byte, error) {
	indent := ""
	if pretty {
		indent = "    "
	}

	// golang protobuf. Use protoreflect.ProtoMessage to distinguish from gogo
	// golang/protobuf 1.4+ will have this interface. Older golang/protobuf are gogo compatible
	// but also not used by Istio at all.
	if _, ok := s.(protoreflect.ProtoMessage); ok {
		if pb, ok := s.(proto.Message); ok {
			b, err := protomarshal.MarshalIndent(pb, indent)
			return b, err
		}
	}

	b := &bytes.Buffer{}
	// gogo protobuf
	if pb, ok := s.(gogoproto.Message); ok {
		err := (&gogojsonpb.Marshaler{Indent: indent}).Marshal(b, pb)
		return b.Bytes(), err
	}
	if pretty {
		return json.MarshalIndent(s, "", indent)
	}
	return json.Marshal(s)
}

type deepCopier interface {
	DeepCopyInterface() any
}

func ApplyYAML(s Spec, yml string) error {
	js, err := yaml.YAMLToJSON([]byte(yml))
	if err != nil {
		return err
	}
	return ApplyJSON(s, string(js))
}

func ApplyJSONStrict(s Spec, js string) error {
	// golang protobuf. Use protoreflect.ProtoMessage to distinguish from gogo
	// golang/protobuf 1.4+ will have this interface. Older golang/protobuf are gogo compatible
	// but also not used by Istio at all.
	if _, ok := s.(protoreflect.ProtoMessage); ok {
		if pb, ok := s.(proto.Message); ok {
			err := protomarshal.ApplyJSONStrict(js, pb)
			return err
		}
	}

	// gogo protobuf
	if pb, ok := s.(gogoproto.Message); ok {
		err := gogoprotomarshal.ApplyJSONStrict(js, pb)
		return err
	}

	d := json.NewDecoder(bytes.NewReader([]byte(js)))
	d.DisallowUnknownFields()
	return d.Decode(&s)
}

func ApplyJSON(s Spec, js string) error {
	// golang protobuf. Use protoreflect.ProtoMessage to distinguish from gogo
	// golang/protobuf 1.4+ will have this interface. Older golang/protobuf are gogo compatible
	// but also not used by Istio at all.
	if _, ok := s.(protoreflect.ProtoMessage); ok {
		if pb, ok := s.(proto.Message); ok {
			err := protomarshal.ApplyJSON(js, pb)
			return err
		}
	}

	// gogo protobuf
	if pb, ok := s.(gogoproto.Message); ok {
		err := gogoprotomarshal.ApplyJSON(js, pb)
		return err
	}

	return json.Unmarshal([]byte(js), &s)
}

func DeepCopy(s any) any {
	if s == nil {
		return nil
	}
	// If deep copy is defined, use that
	if dc, ok := s.(deepCopier); ok {
		return dc.DeepCopyInterface()
	}

	// golang protobuf. Use protoreflect.ProtoMessage to distinguish from gogo
	// golang/protobuf 1.4+ will have this interface. Older golang/protobuf are gogo compatible
	// but also not used by Istio at all.
	if _, ok := s.(protoreflect.ProtoMessage); ok {
		if pb, ok := s.(proto.Message); ok {
			return protomarshal.Clone(pb)
		}
	}

	// gogo protobuf
	if pb, ok := s.(gogoproto.Message); ok {
		return gogoproto.Clone(pb)
	}

	// If we don't have a deep copy method, we will have to do some reflection magic. Its not ideal,
	// but all Istio types have an efficient deep copy.
	js, err := json.Marshal(s)
	if err != nil {
		return nil
	}

	data := reflect.New(reflect.TypeOf(s)).Interface()
	if err := json.Unmarshal(js, data); err != nil {
		return nil
	}
	data = reflect.ValueOf(data).Elem().Interface()
	return data
}

func (c *Config) Equals(other *Config) bool {
	am, bm := c.Meta, other.Meta
	if am.GroupVersionKind != bm.GroupVersionKind {
		return false
	}
	if am.UID != bm.UID {
		return false
	}
	if am.Name != bm.Name {
		return false
	}
	if am.Namespace != bm.Namespace {
		return false
	}
	if am.Domain != bm.Domain {
		return false
	}
	if !maps.Equal(am.Labels, bm.Labels) {
		return false
	}
	if !maps.Equal(am.Annotations, bm.Annotations) {
		return false
	}
	if am.ResourceVersion != bm.ResourceVersion {
		return false
	}
	if am.CreationTimestamp != bm.CreationTimestamp {
		return false
	}
	if !slices.EqualFunc(am.OwnerReferences, bm.OwnerReferences, func(a metav1.OwnerReference, b metav1.OwnerReference) bool {
		if a.APIVersion != b.APIVersion {
			return false
		}
		if a.Kind != b.Kind {
			return false
		}
		if a.Name != b.Name {
			return false
		}
		if a.UID != b.UID {
			return false
		}
		if !ptr.Equal(a.Controller, b.Controller) {
			return false
		}
		if !ptr.Equal(a.BlockOwnerDeletion, b.BlockOwnerDeletion) {
			return false
		}
		return true
	}) {
		return false
	}
	if am.Generation != bm.Generation {
		return false
	}

	if !equals(c.Spec, other.Spec) {
		return false
	}
	if !equals(c.Status, other.Status) {
		return false
	}
	if !maps.Equal(c.Extra, other.Extra) {
		return false
	}
	return true
}

func equals(a any, b any) bool {
	if _, ok := a.(protoreflect.ProtoMessage); ok {
		if pb, ok := a.(proto.Message); ok {
			return proto.Equal(pb, b.(proto.Message))
		}
	}
	// We do NOT do gogo here. The reason is Kubernetes has hacked up almost-gogo types that do not allow Equals() calls

	return reflect.DeepEqual(a, b)
}

type Status any

// Key function for the configuration objects
func Key(grp, ver, typ, name, namespace string) string {
	return grp + "/" + ver + "/" + typ + "/" + namespace + "/" + name // Format: %s/%s/%s/%s/%s
}

// Key is the unique identifier for a configuration object
func (meta *Meta) Key() string {
	return Key(
		meta.GroupVersionKind.Group, meta.GroupVersionKind.Version, meta.GroupVersionKind.Kind,
		meta.Name, meta.Namespace)
}

func (meta *Meta) ToObjectMeta() metav1.ObjectMeta {
	return metav1.ObjectMeta{
		Name:              meta.Name,
		Namespace:         meta.Namespace,
		UID:               kubetypes.UID(meta.UID),
		ResourceVersion:   meta.ResourceVersion,
		Generation:        meta.Generation,
		CreationTimestamp: metav1.NewTime(meta.CreationTimestamp),
		Labels:            meta.Labels,
		Annotations:       meta.Annotations,
		OwnerReferences:   meta.OwnerReferences,
	}
}

func (meta Meta) DeepCopy() Meta {
	nm := meta
	nm.Labels = maps.Clone(meta.Labels)
	nm.Annotations = maps.Clone(meta.Annotations)
	return nm
}

func (c Config) DeepCopy() Config {
	var clone Config
	clone.Meta = c.Meta.DeepCopy()
	clone.Spec = DeepCopy(c.Spec)
	if c.Status != nil {
		clone.Status = DeepCopy(c.Status)
	}
	// Note that this is effectively a shallow clone, but this is fine as it is not manipulated.
	if c.Extra != nil {
		clone.Extra = maps.Clone(c.Extra)
	}
	return clone
}

func (c Config) GetName() string {
	return c.Name
}

func (c Config) GetNamespace() string {
	return c.Namespace
}

func (c Config) GetCreationTimestamp() time.Time {
	return c.CreationTimestamp
}

func (c Config) NamespacedName() kubetypes.NamespacedName {
	return kubetypes.NamespacedName{
		Namespace: c.Namespace,
		Name:      c.Name,
	}
}

var _ fmt.Stringer = GroupVersionKind{}

type GroupVersionKind struct {
	Group   string `json:"group"`
	Version string `json:"version"`
	Kind    string `json:"kind"`
}

func (g GroupVersionKind) String() string {
	return g.CanonicalGroup() + "/" + g.Version + "/" + g.Kind
}

// GroupVersion returns the group/version similar to what would be found in the apiVersion field of a Kubernetes resource.
func (g GroupVersionKind) GroupVersion() string {
	if g.Group == "" {
		return g.Version
	}
	return g.Group + "/" + g.Version
}

func FromKubernetesGVK(gvk schema.GroupVersionKind) GroupVersionKind {
	return GroupVersionKind{
		Group:   gvk.Group,
		Version: gvk.Version,
		Kind:    gvk.Kind,
	}
}

// Kubernetes returns the same GVK, using the Kubernetes object type
func (g GroupVersionKind) Kubernetes() schema.GroupVersionKind {
	return schema.GroupVersionKind{
		Group:   g.Group,
		Version: g.Version,
		Kind:    g.Kind,
	}
}

func CanonicalGroup(group string) string {
	if group != "" {
		return group
	}
	return "core"
}

// CanonicalGroup returns the group with defaulting applied. This means an empty group will
// be treated as "core", following Kubernetes API standards
func (g GroupVersionKind) CanonicalGroup() string {
	return CanonicalGroup(g.Group)
}

// PatchFunc provides the cached config as a base for modification. Only diff the between the cfg
// parameter and the returned Config will be applied.
type PatchFunc func(cfg Config) (Config, kubetypes.PatchType)

type Namer interface {
	GetName() string
	GetNamespace() string
}

func NamespacedName[T Namer](o T) kubetypes.NamespacedName {
	return kubetypes.NamespacedName{
		Namespace: o.GetNamespace(),
		Name:      o.GetName(),
	}
}
