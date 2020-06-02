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

package store

import (
	"errors"
	"fmt"
	"net/url"
	"sync"
	"time"

	"github.com/gogo/protobuf/proto"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"istio.io/istio/pkg/mcp/creds"
	"istio.io/pkg/log"
	"istio.io/pkg/probe"
)

// ChangeType denotes the type of a change
type ChangeType int

const (
	// Update - change was an update or a create to a key.
	Update ChangeType = iota
	// Delete - key was removed.
	Delete
)

// ErrNotFound is the error to be returned when the given key does not exist in the storage.
var ErrNotFound = errors.New("not found")

// ErrWatchAlreadyExists is the error to report that the watching channel already exists.
var ErrWatchAlreadyExists = errors.New("watch already exists")

// Key represents the key to identify a resource in the store.
type Key struct {
	Kind      string
	Namespace string
	Name      string
}

// BackendEvent is an event used between Backend and Store.
type BackendEvent struct {
	Key
	Type  ChangeType
	Value *BackEndResource
}

// ResourceMeta is the standard metadata associated with a resource.
type ResourceMeta struct {
	Name        string
	Namespace   string
	Labels      map[string]string
	Annotations map[string]string
	Revision    string
}

// BackEndResource represents a resources with a raw spec
type BackEndResource struct {
	Kind     string
	Metadata ResourceMeta
	Spec     map[string]interface{}
}

// Key returns the key of the resource in the store.
func (ber *BackEndResource) Key() Key {
	return Key{
		Kind:      ber.Kind,
		Name:      ber.Metadata.Name,
		Namespace: ber.Metadata.Namespace,
	}
}

// Resource represents a resources with converted spec.
type Resource struct {
	Metadata ResourceMeta
	Spec     proto.Message
}

// String is the Istio compatible string representation of the resource.
// Name.Kind.Namespace
// At the point of use Namespace can be omitted, and it is assumed to be the namespace
// of the document.
func (k Key) String() string {
	return k.Name + "." + k.Kind + "." + k.Namespace
}

// Event represents an event. Used by Store.Watch.
type Event struct {
	Key
	Type ChangeType

	// Value refers the new value in the updated event. nil if the event type is delete.
	Value *Resource
}

// BackendValidator defines the interface to validate unstructured event.
type BackendValidator interface {
	Validate(ev *BackendEvent) error
	SupportsKind(string) bool
}

// Validator defines the interface to validate a new change.
type Validator interface {
	Validate(ev *Event) error
}

// Backend defines the typeless storage backend for mixer.
type Backend interface {
	Init(kinds []string) error

	Stop()

	// WaitForSynced blocks and awaits for the caches to be fully populated until timeout.
	WaitForSynced(time.Duration) error

	// Watch creates a channel to receive the events.
	Watch() (<-chan BackendEvent, error)

	// Get returns a resource's spec to the key.
	Get(key Key) (*BackEndResource, error)

	// List returns the whole mapping from key to resource specs in the store.
	List() map[Key]*BackEndResource
}

// Store defines the access to the storage for mixer.
type Store interface {
	Init(kinds map[string]proto.Message) error

	Stop()

	// WaitForSynced blocks and awaits for the caches to be fully populated until timeout.
	WaitForSynced(time.Duration) error

	// Watch creates a channel to receive the events. A store can conduct a single
	// watch channel at the same time. Multiple calls lead to an error.
	Watch() (<-chan Event, error)

	// Get returns the resource to the key.
	Get(key Key) (*Resource, error)

	// List returns the whole mapping from key to resource specs in the store.
	List() map[Key]*Resource

	probe.SupportsProbe
}

// store is the implementation of Store interface.
type store struct {
	kinds   map[string]proto.Message
	backend Backend

	mu    sync.Mutex
	queue *eventQueue
}

func (s *store) RegisterProbe(c probe.Controller, name string) {
	if e, ok := s.backend.(probe.SupportsProbe); ok {
		e.RegisterProbe(c, name)
	}
}

func (s *store) Stop() {
	s.mu.Lock()
	if s.queue != nil {
		close(s.queue.closec)
	}
	s.queue = nil
	s.mu.Unlock()
	s.backend.Stop()
}

// Init initializes the connection with the storage backend. This uses "kinds"
// for the mapping from the kind's name and its structure in protobuf.
func (s *store) Init(kinds map[string]proto.Message) error {
	kindNames := make([]string, 0, len(kinds))
	for k := range kinds {
		kindNames = append(kindNames, k)
	}
	if err := s.backend.Init(kindNames); err != nil {
		return err
	}
	s.kinds = kinds
	return nil
}

// WaitForSynced awaits for the backend to sync.
func (s *store) WaitForSynced(timeout time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.backend.WaitForSynced(timeout)
}

// Watch creates a channel to receive the events.
func (s *store) Watch() (<-chan Event, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.queue != nil {
		return nil, ErrWatchAlreadyExists
	}
	ch, err := s.backend.Watch()
	if err != nil {
		return nil, err
	}
	q := newQueue(ch, s.kinds)
	s.queue = q
	return q.chout, nil
}

// Get returns a resource's spec to the key.
func (s *store) Get(key Key) (*Resource, error) {
	obj, err := s.backend.Get(key)
	if err != nil {
		return nil, err
	}

	pbSpec, err := cloneMessage(key.Kind, s.kinds)
	if err != nil {
		return nil, err
	}
	if err = convert(key, obj.Spec, pbSpec); err != nil {
		return nil, err
	}
	return &Resource{
		Metadata: obj.Metadata,
		Spec:     pbSpec,
	}, nil
}

// List returns the whole mapping from key to resource specs in the store.
func (s *store) List() map[Key]*Resource {
	data := s.backend.List()
	result := make(map[Key]*Resource, len(data))
	for k, d := range data {
		pbSpec, err := cloneMessage(k.Kind, s.kinds)
		if err != nil {
			log.Errorf("Failed to clone %s spec: %v", k, err)
			continue
		}
		if err = convert(k, d.Spec, pbSpec); err != nil {
			log.Errorf("Failed to convert %s spec: %v", k, err)
			continue
		}
		result[k] = &Resource{
			Metadata: d.Metadata,
			Spec:     pbSpec,
		}
	}
	return result
}

// WithBackend creates a new Store with a certain backend. This should be used
// only by tests.
func WithBackend(b Backend) Store {
	return &store{backend: b}
}

// Builder is the type of function to build a Backend.
type Builder func(u *url.URL, gv *schema.GroupVersion, credOptions *creds.Options, ck []string) (Backend, error)

// RegisterFunc is the type to register a builder for URL scheme.
type RegisterFunc func(map[string]Builder)

// Registry keeps the relationship between the URL scheme and
// the Backend implementation.
type Registry struct {
	builders map[string]Builder
}

// NewRegistry creates a new Registry instance for the inventory.
func NewRegistry(inventory ...RegisterFunc) *Registry {
	b := map[string]Builder{}
	for _, rf := range inventory {
		rf(b)
	}
	return &Registry{builders: b}
}

// URL types supported by the config store
const (
	// example fs:///tmp/testdata/configroot
	FSUrl = "fs"
)

// NewStore creates a new Store instance with the specified backend.
func (r *Registry) NewStore(
	configURL string,
	groupVersion *schema.GroupVersion,
	credOptions *creds.Options,
	criticalKinds []string) (Store, error) {
	u, err := url.Parse(configURL)

	if err != nil {
		return nil, fmt.Errorf("invalid config URL %s %v", configURL, err)
	}

	var b Backend
	switch u.Scheme {
	case FSUrl:
		b = newFsStore(u.Path)
	default:
		if builder, ok := r.builders[u.Scheme]; ok {
			b, err = builder(u, groupVersion, credOptions, criticalKinds)
			if err != nil {
				return nil, err
			}
		}
	}
	if b != nil {
		return &store{backend: b}, nil
	}
	return nil, fmt.Errorf("unknown config URL %s %v", configURL, u)
}
