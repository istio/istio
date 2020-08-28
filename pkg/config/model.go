package config

import (
	"fmt"
	"time"

	"github.com/gogo/protobuf/proto"
)

// ConfigMeta is metadata attached to each configuration unit.
// The revision is optional, and if provided, identifies the
// last update operation on the object.
type ConfigMeta struct {
	// GroupVersionKind is a short configuration name that matches the content message type
	// (e.g. "route-rule")
	GroupVersionKind GroupVersionKind `json:"type,omitempty"`

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
}

// Config is a configuration unit consisting of the type of configuration, the
// key identifier that is unique per type, and the content represented as a
// protobuf message.
type Config struct {
	ConfigMeta

	// Spec holds the configuration object as a gogo protobuf message
	Spec proto.Message
}

// Key function for the configuration objects
func Key(typ, name, namespace string) string {
	return fmt.Sprintf("%s/%s/%s", typ, namespace, name)
}

// Key is the unique identifier for a configuration object
// TODO: this is *not* unique - needs the version and group
func (meta *ConfigMeta) Key() string {
	return Key(meta.GroupVersionKind.Kind, meta.Name, meta.Namespace)
}

func (c Config) DeepCopy() Config {
	var clone Config
	clone.ConfigMeta = c.ConfigMeta
	if c.Labels != nil {
		clone.Labels = make(map[string]string)
		for k, v := range c.Labels {
			clone.Labels[k] = v
		}
	}
	if c.Annotations != nil {
		clone.Annotations = make(map[string]string)
		for k, v := range c.Annotations {
			clone.Annotations[k] = v
		}
	}
	clone.Spec = proto.Clone(c.Spec)
	return clone
}

var _ fmt.Stringer = GroupVersionKind{}

type GroupVersionKind struct {
	Group   string `json:"group"`
	Version string `json:"version"`
	Kind    string `json:"kind"`
}

func (g GroupVersionKind) String() string {
	if g.Group == "" {
		return "core/" + g.Version + "/" + g.Kind
	}
	return g.Group + "/" + g.Version + "/" + g.Kind
}
