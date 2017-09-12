// Copyright 2017 Istio Authors
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
	"context"
	"errors"
	"fmt"
	"net/url"
	"sync"

	"github.com/gogo/protobuf/proto"
	"github.com/golang/glog"
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

// BackendEvent is an event used between Store2Backend and Store2.
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
	Metadata ResourceMeta
	Spec     map[string]interface{}
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

// Event represents an event. Used by Store2.Watch.
type Event struct {
	Key
	Type ChangeType

	// Value refers the new value in the updated event. nil if the event type is delete.
	Value *Resource
}

// Validator defines the interface to validate a new change.
type Validator interface {
	Validate(t ChangeType, key Key, spec proto.Message) bool
}

// Store2Backend defines the typeless storage backend for mixer.
// TODO: rename to StoreBackend.
type Store2Backend interface {
	Init(ctx context.Context, kinds []string) error

	// Watch creates a channel to receive the events.
	Watch(ctx context.Context) (<-chan BackendEvent, error)

	// Get returns a resource's spec to the key.
	Get(key Key) (*BackEndResource, error)

	// List returns the whole mapping from key to resource specs in the store.
	List() map[Key]*BackEndResource
}

// Store2 defines the access to the storage for mixer.
// TODO: rename to Store.
type Store2 interface {
	Init(ctx context.Context, kinds map[string]proto.Message) error

	// Watch creates a channel to receive the events. A store can conduct a single
	// watch channel at the same time. Multiple calls lead to an error.
	Watch(ctx context.Context) (<-chan Event, error)

	// Get returns a resource's spec to the key.
	Get(key Key, spec proto.Message) error

	// List returns the whole mapping from key to resource specs in the store.
	List() map[Key]*Resource
}

// store2 is the implementation of Store2 interface.
type store2 struct {
	kinds   map[string]proto.Message
	backend Store2Backend

	mu    sync.Mutex
	queue *eventQueue
}

// Init initializes the connection with the storage backend. This uses "kinds"
// for the mapping from the kind's name and its structure in protobuf.
// The connection will be closed after ctx is done.
func (s *store2) Init(ctx context.Context, kinds map[string]proto.Message) error {
	kindNames := make([]string, 0, len(kinds))
	for k := range kinds {
		kindNames = append(kindNames, k)
	}
	if err := s.backend.Init(ctx, kindNames); err != nil {
		return err
	}
	s.kinds = kinds
	return nil
}

// Watch creates a channel to receive the events.
func (s *store2) Watch(ctx context.Context) (<-chan Event, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.queue != nil {
		return nil, ErrWatchAlreadyExists
	}
	ch, err := s.backend.Watch(ctx)
	if err != nil {
		return nil, err
	}
	q := newQueue(ctx, ch, s.kinds)
	s.queue = q
	go func() {
		<-ctx.Done()
		s.mu.Lock()
		s.queue = nil
		s.mu.Unlock()
	}()
	return q.chout, nil
}

const (
	ruleKind      = "rule"
	selectorField = "selector"
	matchField    = "match"
)

// warnDeprecationAndFix warns users about deprecated fields.
// It maps the field into new name.
func warnDeprecationAndFix(key Key, spec map[string]interface{}) map[string]interface{} {
	if key.Kind != ruleKind {
		return spec
	}
	sel := spec[selectorField]
	if sel == nil {
		return spec
	}
	glog.Warningf("Deprecated field 'selector' used in %s. Use 'match' instead.", key)
	spec[matchField] = sel
	delete(spec, selectorField)
	return spec
}

// Get returns a resource's spec to the key.
func (s *store2) Get(key Key, spec proto.Message) error {
	obj, err := s.backend.Get(key)
	if err != nil {
		return err
	}

	return convert(warnDeprecationAndFix(key, obj.Spec), spec)
}

// List returns the whole mapping from key to resource specs in the store.
func (s *store2) List() map[Key]*Resource {
	data := s.backend.List()
	result := make(map[Key]*Resource, len(data))
	for k, d := range data {
		pbSpec, err := cloneMessage(k.Kind, s.kinds)
		if err != nil {
			glog.Errorf("Failed to clone %s spec: %v", k, err)
			continue
		}
		if err = convert(warnDeprecationAndFix(k, d.Spec), pbSpec); err != nil {
			glog.Errorf("Failed to convert %s spec: %v", k, err)
			continue
		}
		result[k] = &Resource{
			Metadata: d.Metadata,
			Spec:     pbSpec,
		}
	}
	return result
}

// Store2Builder is the type of function to build a Store2Backend.
type Store2Builder func(u *url.URL) (Store2Backend, error)

// RegisterFunc2 is the type to register a builder for URL scheme.
type RegisterFunc2 func(map[string]Store2Builder)

// Registry2 keeps the relationship between the URL scheme and
// the Store2Backend implementation.
type Registry2 struct {
	builders map[string]Store2Builder
}

// NewRegistry2 creates a new Registry instance for the inventory.
func NewRegistry2(inventory ...RegisterFunc2) *Registry2 {
	b := map[string]Store2Builder{}
	for _, rf := range inventory {
		rf(b)
	}
	return &Registry2{builders: b}
}

// NewStore2 creates a new Store2 instance with the specified backend.
func (r *Registry2) NewStore2(configURL string) (Store2, error) {
	u, err := url.Parse(configURL)

	if err != nil {
		return nil, fmt.Errorf("invalid config URL %s %v", configURL, err)
	}

	s2 := &store2{}
	if u.Scheme == FSUrl {
		s2.backend = NewFsStore2(u.Path)
		return s2, nil
	}
	if builder, ok := r.builders[u.Scheme]; ok {
		s2.backend, err = builder(u)
		if err != nil {
			return nil, fmt.Errorf("unable to get config store: %v", err)
		}
		return s2, nil
	}
	return nil, fmt.Errorf("unknown config URL scheme %s", u.Scheme)
}
