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

package model

import (
	"sort"
	"strings"

	udpa "github.com/cncf/xds/go/udpa/type/v1"
	"k8s.io/apimachinery/pkg/types"

	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/host"
	"istio.io/istio/pkg/config/schema/collection"
	"istio.io/istio/pkg/config/schema/kind"
	"istio.io/istio/pkg/util/hash"
	netutil "istio.io/istio/pkg/util/net"
	"istio.io/istio/pkg/util/sets"
)

// Statically link protobuf descriptors from UDPA
var _ = udpa.TypedStruct{}

type ConfigHash uint64

// ConfigKey describe a specific config item.
// In most cases, the name is the config's name. However, for ServiceEntry it is service's FQDN.
type ConfigKey struct {
	Kind      kind.Kind
	Name      string
	Namespace string
}

func (key ConfigKey) HashCode() ConfigHash {
	h := hash.New()
	h.Write([]byte{byte(key.Kind)})
	// Add separator / to avoid collision.
	h.Write([]byte("/"))
	h.Write([]byte(key.Namespace))
	h.Write([]byte("/"))
	h.Write([]byte(key.Name))
	return ConfigHash(h.Sum64())
}

func (key ConfigKey) String() string {
	return key.Kind.String() + "/" + key.Namespace + "/" + key.Name
}

// ConfigsOfKind extracts configs of the specified kind.
func ConfigsOfKind(configs sets.Set[ConfigKey], kind kind.Kind) sets.Set[ConfigKey] {
	ret := make(sets.Set[ConfigKey])

	for conf := range configs {
		if conf.Kind == kind {
			ret.Insert(conf)
		}
	}

	return ret
}

// HasConfigsOfKind returns true if configs has changes of type kind
func HasConfigsOfKind(configs sets.Set[ConfigKey], kind kind.Kind) bool {
	for c := range configs {
		if c.Kind == kind {
			return true
		}
	}
	return false
}

// ConfigNamesOfKind extracts config names of the specified kind.
func ConfigNamesOfKind(configs sets.Set[ConfigKey], kind kind.Kind) sets.String {
	ret := sets.New[string]()

	for conf := range configs {
		if conf.Kind == kind {
			ret.Insert(conf.Name)
		}
	}

	return ret
}

// ConfigNamespacedNameOfKind extracts config names of the specified kind.
func ConfigNamespacedNameOfKind(configs map[ConfigKey]struct{}, kind kind.Kind) sets.Set[types.NamespacedName] {
	ret := sets.New[types.NamespacedName]()

	for conf := range configs {
		if conf.Kind == kind {
			ret.Insert(types.NamespacedName{
				Namespace: conf.Namespace,
				Name:      conf.Name,
			})
		}
	}

	return ret
}

// ConfigNameOfKind extracts config names of the specified kind.
func ConfigNameOfKind(configs map[ConfigKey]struct{}, kind kind.Kind) sets.String {
	ret := sets.New[string]()

	for conf := range configs {
		if conf.Kind == kind {
			ret.Insert(conf.Name)
		}
	}

	return ret
}

// ConfigStore describes a set of platform agnostic APIs that must be supported
// by the underlying platform to store and retrieve Istio configuration.
//
// Configuration key is defined to be a combination of the type, name, and
// namespace of the configuration object. The configuration key is guaranteed
// to be unique in the store.
//
// The storage interface presented here assumes that the underlying storage
// layer supports _Get_ (list), _Update_ (update), _Create_ (create) and
// _Delete_ semantics but does not guarantee any transactional semantics.
//
// _Update_, _Create_, and _Delete_ are mutator operations. These operations
// are asynchronous, and you might not see the effect immediately (e.g. _Get_
// might not return the object by key immediately after you mutate the store.)
// Intermittent errors might occur even though the operation succeeds, so you
// should always check if the object store has been modified even if the
// mutating operation returns an error.  Objects should be created with
// _Create_ operation and updated with _Update_ operation.
//
// Resource versions record the last mutation operation on each object. If a
// mutation is applied to a different revision of an object than what the
// underlying storage expects as defined by pure equality, the operation is
// blocked.  The client of this interface should not make assumptions about the
// structure or ordering of the revision identifier.
//
// Object references supplied and returned from this interface should be
// treated as read-only. Modifying them violates thread-safety.
type ConfigStore interface {
	// Schemas exposes the configuration type schema known by the config store.
	// The type schema defines the bidirectional mapping between configuration
	// types and the protobuf encoding schema.
	Schemas() collection.Schemas

	// Get retrieves a configuration element by a type and a key
	Get(typ config.GroupVersionKind, name, namespace string) *config.Config

	// List returns objects by type and namespace.
	// Use "" for the namespace to list across namespaces.
	List(typ config.GroupVersionKind, namespace string) []config.Config

	// Create adds a new configuration object to the store. If an object with the
	// same name and namespace for the type already exists, the operation fails
	// with no side effects.
	Create(config config.Config) (revision string, err error)

	// Update modifies an existing configuration object in the store.  Update
	// requires that the object has been created.  Resource version prevents
	// overriding a value that has been changed between prior _Get_ and _Put_
	// operation to achieve optimistic concurrency. This method returns a new
	// revision if the operation succeeds.
	Update(config config.Config) (newRevision string, err error)
	UpdateStatus(config config.Config) (newRevision string, err error)

	// Patch applies only the modifications made in the PatchFunc rather than doing a full replace. Useful to avoid
	// read-modify-write conflicts when there are many concurrent-writers to the same resource.
	Patch(orig config.Config, patchFn config.PatchFunc) (string, error)

	// Delete removes an object from the store by key
	// For k8s, resourceVersion must be fulfilled before a deletion is carried out.
	// If not possible, a 409 Conflict status will be returned.
	Delete(typ config.GroupVersionKind, name, namespace string, resourceVersion *string) error
}

type EventHandler = func(config.Config, config.Config, Event)

// ConfigStoreController is a local fully-replicated cache of the config store with additional handlers.  The
// controller actively synchronizes its local state with the remote store and
// provides a notification mechanism to receive update events. As such, the
// notification handlers must be registered prior to calling _Run_, and the
// cache requires initial synchronization grace period after calling  _Run_.
//
// Update notifications require the following consistency guarantee: the view
// in the cache must be AT LEAST as fresh as the moment notification arrives, but
// MAY BE more fresh (e.g. if _Delete_ cancels an _Add_ event).
//
// Handlers execute on the single worker queue in the order they are appended.
// Handlers receive the notification event and the associated object.  Note
// that all handlers must be registered before starting the cache controller.
type ConfigStoreController interface {
	ConfigStore

	// RegisterEventHandler adds a handler to receive config update events for a
	// configuration type
	RegisterEventHandler(kind config.GroupVersionKind, handler EventHandler)

	// Run until a signal is received.
	// Run *should* block, so callers should typically call `go controller.Run(stop)`
	Run(stop <-chan struct{})

	// HasSynced returns true after initial cache synchronization is complete
	HasSynced() bool
}

const (
	// NamespaceAll is a designated symbol for listing across all namespaces
	NamespaceAll = ""
)

// ResolveShortnameToFQDN uses metadata information to resolve a reference
// to shortname of the service to FQDN
func ResolveShortnameToFQDN(hostname string, meta config.Meta) host.Name {
	if len(hostname) == 0 {
		// only happens when the gateway-api BackendRef is invalid
		return ""
	}
	out := hostname
	// Treat the wildcard hostname as fully qualified. Any other variant of a wildcard hostname will contain a `.` too,
	// and skip the next if, so we only need to check for the literal wildcard itself.
	if hostname == "*" {
		return host.Name(out)
	}

	// if the hostname is a valid ipv4 or ipv6 address, do not append domain or namespace
	if netutil.IsValidIPAddress(hostname) {
		return host.Name(out)
	}

	// if FQDN is specified, do not append domain or namespace to hostname
	if !strings.Contains(hostname, ".") {
		if meta.Namespace != "" {
			out = out + "." + meta.Namespace
		}

		// FIXME this is a gross hack to hardcode a service's domain name in kubernetes
		// BUG this will break non kubernetes environments if they use shortnames in the
		// rules.
		if meta.Domain != "" {
			out = out + ".svc." + meta.Domain
		}
	}

	return host.Name(out)
}

// resolveGatewayName uses metadata information to resolve a reference
// to shortname of the gateway to FQDN
func resolveGatewayName(gwname string, meta config.Meta) string {
	out := gwname

	// New way of binding to a gateway in remote namespace
	// is ns/name. Old way is either FQDN or short name
	if !strings.Contains(gwname, "/") {
		if !strings.Contains(gwname, ".") {
			// we have a short name. Resolve to a gateway in same namespace
			out = meta.Namespace + "/" + gwname
		} else {
			// parse namespace from FQDN. This is very hacky, but meant for backward compatibility only
			// This is a legacy FQDN format. Transform name.ns.svc.cluster.local -> ns/name
			i := strings.Index(gwname, ".")
			fqdn := strings.Index(gwname[i+1:], ".")
			if fqdn == -1 {
				out = gwname[i+1:] + "/" + gwname[:i]
			} else {
				out = gwname[i+1:i+1+fqdn] + "/" + gwname[:i]
			}
		}
	} else {
		// remove the . from ./gateway and substitute it with the namespace name
		i := strings.Index(gwname, "/")
		if gwname[:i] == "." {
			out = meta.Namespace + "/" + gwname[i+1:]
		}
	}
	return out
}

// MostSpecificHostMatch compares the maps of specific and wildcard hosts to the needle, and returns the longest element
// matching the needle and it's value, or false if no element in the maps matches the needle.
func MostSpecificHostMatch[V any](needle host.Name, specific map[host.Name]V, wildcard map[host.Name]V) (host.Name, V, bool) {
	if needle.IsWildCarded() {
		// exact match first
		if v, ok := wildcard[needle]; ok {
			return needle, v, true
		}

		return mostSpecificHostWildcardMatch(string(needle[1:]), wildcard)
	}

	// exact match first
	if v, ok := specific[needle]; ok {
		return needle, v, true
	}

	// check wildcard
	return mostSpecificHostWildcardMatch(string(needle), wildcard)
}

func mostSpecificHostWildcardMatch[V any](needle string, wildcard map[host.Name]V) (host.Name, V, bool) {
	found := false
	var matchHost host.Name
	var matchValue V

	for h, v := range wildcard {
		if strings.HasSuffix(needle, string(h[1:])) {
			if !found {
				matchHost = h
				matchValue = wildcard[h]
				found = true
			} else if host.MoreSpecific(h, matchHost) {
				matchHost = h
				matchValue = v
			}
		}
	}

	return matchHost, matchValue, found
}

// sortConfigByCreationTime sorts the list of config objects in ascending order by their creation time (if available).
func sortConfigByCreationTime(configs []config.Config) []config.Config {
	sort.Slice(configs, func(i, j int) bool {
		// If creation time is the same, then behavior is nondeterministic. In this case, we can
		// pick an arbitrary but consistent ordering based on name and namespace, which is unique.
		// CreationTimestamp is stored in seconds, so this is not uncommon.
		if configs[i].CreationTimestamp == configs[j].CreationTimestamp {
			in := configs[i].Name + "." + configs[i].Namespace
			jn := configs[j].Name + "." + configs[j].Namespace
			return in < jn
		}
		return configs[i].CreationTimestamp.Before(configs[j].CreationTimestamp)
	})
	return configs
}

// key creates a key from a reference's name and namespace.
func key(name, namespace string) string {
	return name + "/" + namespace
}
