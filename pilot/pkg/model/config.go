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
	"fmt"
	"hash/crc32"
	"sort"
	"strings"
	"time"

	"istio.io/pkg/ledger"

	udpa "github.com/cncf/udpa/go/udpa/type/v1"
	"github.com/gogo/protobuf/proto"

	mccpb "istio.io/api/mixer/v1/config/client"
	networking "istio.io/api/networking/v1alpha3"

	"istio.io/istio/pkg/config/host"
	"istio.io/istio/pkg/config/labels"
	"istio.io/istio/pkg/config/schema/collection"
	"istio.io/istio/pkg/config/schema/gvk"
	"istio.io/istio/pkg/config/schema/resource"
)

var (
	// Statically link protobuf descriptors from UDPA
	_ = udpa.TypedStruct{}
)

// ConfigKey describe a specific config item.
// In most cases, the name is the config's name. However, for ServiceEntry it is service's FQDN.
type ConfigKey struct {
	Kind      resource.GroupVersionKind
	Name      string
	Namespace string
}

func (key ConfigKey) HashCode() uint32 {
	var result uint32
	result = 31*result + crc32.ChecksumIEEE([]byte(key.Kind.Kind))
	result = 31*result + crc32.ChecksumIEEE([]byte(key.Kind.Version))
	result = 31*result + crc32.ChecksumIEEE([]byte(key.Kind.Group))
	result = 31*result + crc32.ChecksumIEEE([]byte(key.Namespace))
	result = 31*result + crc32.ChecksumIEEE([]byte(key.Name))
	return result
}

// ConfigsOfKind extracts configs of the specified kind.
func ConfigsOfKind(configs map[ConfigKey]struct{}, kind resource.GroupVersionKind) map[ConfigKey]struct{} {
	ret := make(map[ConfigKey]struct{})

	for conf := range configs {
		if conf.Kind == kind {
			ret[conf] = struct{}{}
		}
	}

	return ret
}

// ConfigNamesOfKind extracts config names of the specified kind.
func ConfigNamesOfKind(configs map[ConfigKey]struct{}, kind resource.GroupVersionKind) map[string]struct{} {
	ret := make(map[string]struct{})

	for conf := range configs {
		if conf.Kind == kind {
			ret[conf.Name] = struct{}{}
		}
	}

	return ret
}

// ConfigMeta is metadata attached to each configuration unit.
// The revision is optional, and if provided, identifies the
// last update operation on the object.
type ConfigMeta struct {
	// GroupVersionKind is a short configuration name that matches the content message type
	// (e.g. "route-rule")
	GroupVersionKind resource.GroupVersionKind `json:"type,omitempty"`

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
	// The type schema defines the bidrectional mapping between configuration
	// types and the protobuf encoding schema.
	Schemas() collection.Schemas

	// Get retrieves a configuration element by a type and a key
	Get(typ resource.GroupVersionKind, name, namespace string) *Config

	// List returns objects by type and namespace.
	// Use "" for the namespace to list across namespaces.
	List(typ resource.GroupVersionKind, namespace string) ([]Config, error)

	// Create adds a new configuration object to the store. If an object with the
	// same name and namespace for the type already exists, the operation fails
	// with no side effects.
	Create(config Config) (revision string, err error)

	// Update modifies an existing configuration object in the store.  Update
	// requires that the object has been created.  Resource version prevents
	// overriding a value that has been changed between prior _Get_ and _Put_
	// operation to achieve optimistic concurrency. This method returns a new
	// revision if the operation succeeds.
	Update(config Config) (newRevision string, err error)

	// Delete removes an object from the store by key
	Delete(typ resource.GroupVersionKind, name, namespace string) error

	Version() string

	GetResourceAtVersion(version string, key string) (resourceVersion string, err error)

	GetLedger() ledger.Ledger

	SetLedger(ledger.Ledger) error
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

// ConfigStoreCache is a local fully-replicated cache of the config store.  The
// cache actively synchronizes its local state with the remote store and
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
type ConfigStoreCache interface {
	ConfigStore

	// RegisterEventHandler adds a handler to receive config update events for a
	// configuration type
	RegisterEventHandler(kind resource.GroupVersionKind, handler func(Config, Config, Event))

	// Run until a signal is received
	Run(stop <-chan struct{})

	// HasSynced returns true after initial cache synchronization is complete
	HasSynced() bool
}

// IstioConfigStore is a specialized interface to access config store using
// Istio configuration types
// nolint
type IstioConfigStore interface {
	ConfigStore

	// ServiceEntries lists all service entries
	ServiceEntries() []Config

	// Gateways lists all gateways bound to the specified workload labels
	Gateways(workloadLabels labels.Collection) []Config

	// QuotaSpecByDestination selects Mixerclient quota specifications
	// associated with destination service instances.
	QuotaSpecByDestination(hostname host.Name) []Config

	// AuthorizationPolicies selects AuthorizationPolicies in the specified namespace.
	AuthorizationPolicies(namespace string) []Config
}

const (
	// NamespaceAll is a designated symbol for listing across all namespaces
	NamespaceAll = ""
)

/*
  This conversion of CRD (== yaml files with k8s metadata) is extremely inefficient.
  The yaml is parsed (kubeyaml), converted to YAML again (FromJSONMap),
  converted to JSON (YAMLToJSON) and finally UnmarshallString in proto is called.

  The result is not cached in the model.

  In 0.7, this was the biggest factor in scalability. Moving forward we will likely
  deprecate model, and do the conversion (hopefully more efficient) only once, when
  an object is first read.
*/

// ResolveHostname produces a FQDN based on either the service or
// a concat of the namespace + domain
// Deprecated. Do not use
func ResolveHostname(meta ConfigMeta, svc *mccpb.IstioService) host.Name {
	out := svc.Name
	// if FQDN is specified, do not append domain or namespace to hostname
	// Service field has precedence over Name
	if svc.Service != "" {
		out = svc.Service
	} else {
		if svc.Namespace != "" {
			out = out + "." + svc.Namespace
		} else if meta.Namespace != "" {
			out = out + "." + meta.Namespace
		}

		if svc.Domain != "" {
			out = out + "." + svc.Domain
		} else if meta.Domain != "" {
			out = out + ".svc." + meta.Domain
		}
	}

	return host.Name(out)
}

// ResolveShortnameToFQDN uses metadata information to resolve a reference
// to shortname of the service to FQDN
func ResolveShortnameToFQDN(hostname string, meta ConfigMeta) host.Name {
	out := hostname
	// Treat the wildcard hostname as fully qualified. Any other variant of a wildcard hostname will contain a `.` too,
	// and skip the next if, so we only need to check for the literal wildcard itself.
	if hostname == "*" {
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
func resolveGatewayName(gwname string, meta ConfigMeta) string {
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

// MostSpecificHostMatch compares the elements of the stack to the needle, and returns the longest stack element
// matching the needle, or false if no element in the stack matches the needle.
func MostSpecificHostMatch(needle host.Name, stack []host.Name) (host.Name, bool) {
	matches := []host.Name{}
	for _, h := range stack {
		if needle == h {
			// exact match, return immediately
			return needle, true
		}
		if needle.SubsetOf(h) {
			matches = append(matches, h)
		}
	}
	if len(matches) > 0 {
		// TODO: return closest match out of all non-exact matching hosts
		return matches[0], true
	}
	return "", false
}

// istioConfigStore provides a simple adapter for Istio configuration types
// from the generic config registry
type istioConfigStore struct {
	ConfigStore
}

// MakeIstioStore creates a wrapper around a store.
// In pilot it is initialized with a ConfigStoreCache, tests only use
// a regular ConfigStore.
func MakeIstioStore(store ConfigStore) IstioConfigStore {
	return &istioConfigStore{store}
}

func (store *istioConfigStore) ServiceEntries() []Config {
	serviceEntries, err := store.List(gvk.ServiceEntry, NamespaceAll)
	if err != nil {
		return nil
	}

	// To ensure the ip allocation logic deterministically
	// allocates the same IP to a service entry.
	sortConfigByCreationTime(serviceEntries)
	return serviceEntries
}

// sortConfigByCreationTime sorts the list of config objects in ascending order by their creation time (if available).
func sortConfigByCreationTime(configs []Config) {
	sort.SliceStable(configs, func(i, j int) bool {
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
}

func (store *istioConfigStore) Gateways(workloadLabels labels.Collection) []Config {
	configs, err := store.List(gvk.Gateway, NamespaceAll)
	if err != nil {
		return nil
	}

	sortConfigByCreationTime(configs)
	out := make([]Config, 0)
	for _, cfg := range configs {
		gateway := cfg.Spec.(*networking.Gateway)
		if gateway.GetSelector() == nil {
			// no selector. Applies to all workloads asking for the gateway
			out = append(out, cfg)
		} else {
			gatewaySelector := labels.Instance(gateway.GetSelector())
			if workloadLabels.IsSupersetOf(gatewaySelector) {
				out = append(out, cfg)
			}
		}
	}
	return out
}

// matchWildcardService matches destinationHost to a wildcarded svc.
// checked values for svc
//     '*'  matches everything
//     '*.ns.*'  matches anything in the same namespace
//		strings of any other form are not matched.
func matchWildcardService(destinationHost, svc string) bool {
	if len(svc) == 0 || !strings.Contains(svc, "*") {
		return false
	}

	if svc == "*" {
		return true
	}

	// check for namespace match with svc like '*.ns.*'
	// extract match substring by dropping '*'
	if strings.HasPrefix(svc, "*") && strings.HasSuffix(svc, "*") {
		return strings.Contains(destinationHost, svc[1:len(svc)-1])
	}

	log.Warnf("Wildcard pattern '%s' is not allowed. Only '*' or '*.<ns>.*' is allowed.", svc)

	return false
}

// MatchesDestHost returns true if the service instance matches the given IstioService
// ex: binding host(details.istio-system.svc.cluster.local) ?= instance(reviews.default.svc.cluster.local)
func MatchesDestHost(destinationHost string, meta ConfigMeta, svc *mccpb.IstioService) bool {
	if matchWildcardService(destinationHost, svc.Service) {
		return true
	}

	// try exact matches
	hostname := string(ResolveHostname(meta, svc))
	if destinationHost == hostname {
		return true
	}
	shortName := hostname[0:strings.Index(hostname, ".")]
	if strings.HasPrefix(destinationHost, shortName) {
		log.Warnf("Quota excluded. service: %s matches binding shortname: %s, but does not match fqdn: %s",
			destinationHost, shortName, hostname)
	}

	return false
}

func recordSpecRef(refs map[string]bool, bindingNamespace string, quotas []*mccpb.QuotaSpecBinding_QuotaSpecReference) {
	for _, spec := range quotas {
		namespace := spec.Namespace
		if namespace == "" {
			namespace = bindingNamespace
		}
		refs[key(spec.Name, namespace)] = true
	}
}

// key creates a key from a reference's name and namespace.
func key(name, namespace string) string {
	return name + "/" + namespace
}

// findQuotaSpecRefs returns a set of quotaSpec reference names
func findQuotaSpecRefs(hostname host.Name, bindings []Config) map[string]bool {
	// Build the set of quota spec references bound to the service instance.
	refs := make(map[string]bool)
	for _, binding := range bindings {
		b := binding.Spec.(*mccpb.QuotaSpecBinding)
		for _, service := range b.Services {
			if MatchesDestHost(string(hostname), binding.ConfigMeta, service) {
				recordSpecRef(refs, binding.Namespace, b.QuotaSpecs)
				// found a binding that matches the instance.
				break
			}
		}
	}

	return refs
}

// filterQuotaSpecsByDestination provides QuotaSpecByDestination filtering logic as a
// function that can be called on cached binding + spec sets
func filterQuotaSpecsByDestination(hostname host.Name, bindings []Config, specs []Config) []Config {
	// Build the set of quota spec references bound to the service instance.
	refs := findQuotaSpecRefs(hostname, bindings)
	log.Debugf("QuotaSpecByDestination refs:%v", refs)

	// Append any spec that is in the set of references.
	// Remove matching specs from refs so refs only contains dangling references.
	var out []Config
	for _, spec := range specs {
		refkey := key(spec.ConfigMeta.Name, spec.ConfigMeta.Namespace)
		if refs[refkey] {
			out = append(out, spec)
			delete(refs, refkey)
		}
	}

	if len(refs) > 0 {
		log.Warnf("Some matched QuotaSpecs were not found: %v", refs)
	}
	return out
}

// QuotaSpecByDestination selects Mixerclient quota specifications
// associated with destination service instances.
func (store *istioConfigStore) QuotaSpecByDestination(hostname host.Name) []Config {
	log.Debugf("QuotaSpecByDestination(%v)", hostname)
	bindings, err := store.List(gvk.QuotaSpecBinding, NamespaceAll)
	if err != nil {
		log.Warnf("Unable to fetch QuotaSpecBindings: %v", err)
		return nil
	}

	log.Debugf("QuotaSpecByDestination bindings[%d] %v", len(bindings), bindings)
	specs, err := store.List(gvk.QuotaSpec, NamespaceAll)
	if err != nil {
		log.Warnf("Unable to fetch QuotaSpecs: %v", err)
		return nil
	}

	log.Debugf("QuotaSpecByDestination specs[%d] %v", len(specs), specs)

	return filterQuotaSpecsByDestination(hostname, bindings, specs)
}

func (store *istioConfigStore) AuthorizationPolicies(namespace string) []Config {
	authorizationPolicies, err := store.List(gvk.AuthorizationPolicy, namespace)
	if err != nil {
		log.Errorf("failed to get AuthorizationPolicy in namespace %s: %v", namespace, err)
		return nil
	}

	return authorizationPolicies
}

// SortQuotaSpec sorts a slice in a stable manner.
func SortQuotaSpec(specs []Config) {
	sort.Slice(specs, func(i, j int) bool {
		// protect against incompatible types
		irule, _ := specs[i].Spec.(*mccpb.QuotaSpec)
		jrule, _ := specs[j].Spec.(*mccpb.QuotaSpec)
		return irule == nil || jrule == nil || (specs[i].Key() < specs[j].Key())
	})
}

func (c Config) DeepCopy() Config {
	var clone Config
	clone.ConfigMeta = c.ConfigMeta
	clone.Spec = proto.Clone(c.Spec)
	return clone
}
