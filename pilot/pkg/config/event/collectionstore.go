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

package event

import (
	"fmt"
	"sync"
	"sync/atomic"

	"istio.io/pkg/log"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/config/event"
	"istio.io/istio/pkg/config/schema/collection"
)

type eventHandler func(schema collection.Schema, eventKind event.Kind, prev, conf *model.Config)

// collectionStore handles config storage for a single collection.
type collectionStore struct {
	mutex        sync.RWMutex
	store        map[string]map[string]*model.Config
	synced       int32
	domainSuffix string
	handler      eventHandler
	schema       collection.Schema
}

func newCollectionStore(schema collection.Schema, domainSuffix string, handler eventHandler) *collectionStore {
	return &collectionStore{
		store:        make(map[string]map[string]*model.Config),
		domainSuffix: domainSuffix,
		handler:      handler,
		schema:       schema,
	}
}

// List returns all the config that is stored by namespace
// if namespace is empty string it returns config for all the namespaces
func (s *collectionStore) List(namespace string) (out []model.Config, err error) {
	if namespace == "" {
		return s.listAll(), nil
	}

	return s.listNamespace(namespace), nil
}

func (s *collectionStore) listAll() (out []model.Config) {
	s.mutex.RLock()
	for _, byNamespace := range s.store {
		for _, config := range byNamespace {
			out = append(out, *config)
		}
	}
	s.mutex.RUnlock()

	return out
}

func (s *collectionStore) listNamespace(namespace string) (out []model.Config) {
	s.mutex.RLock()
	for _, config := range s.store[namespace] {
		out = append(out, *config)
	}
	s.mutex.RUnlock()
	return out
}

func (s *collectionStore) Handle(e event.Event) {
	switch e.Kind {
	case event.Reset:
		s.onReset()
	case event.FullSync:
		s.onFullSync()
	case event.Added, event.Updated:
		s.onAddOrUpdate(e)
	case event.Deleted:
		s.onDeleted(e)
	default:
		log.Warnf("unsupported event kind %v", e.Kind)
	}
}

func (s *collectionStore) HasSynced() bool {
	return atomic.LoadInt32(&s.synced) != 0
}

func (s *collectionStore) onReset() {
	atomic.StoreInt32(&s.synced, 0)

	s.mutex.Lock()
	toDelete := s.store
	s.store = make(map[string]map[string]*model.Config)
	s.mutex.Unlock()

	for _, nameMap := range toDelete {
		for _, conf := range nameMap {
			s.handler(s.schema, event.Deleted, nil, conf)
		}
	}
}

func (s *collectionStore) onFullSync() {
	atomic.StoreInt32(&s.synced, 1)
}

func (s *collectionStore) onAddOrUpdate(e event.Event) {
	conf, err := s.convertEvent(e)
	if err != nil {
		log.Warna(err)
		return
	}

	s.mutex.Lock()

	// Get or create the storage for the namespace.
	ns := s.store[conf.Namespace]
	if ns == nil {
		ns = make(map[string]*model.Config)
		s.store[conf.Namespace] = ns
	}

	// Save the previous value if there was one.
	prev := ns[conf.Name]

	eventKind := event.Added
	if prev != nil {
		if prev.ResourceVersion == conf.ResourceVersion {
			log.Debugf("%s: received %s event current version: %s", conf.GroupVersionKind(), e.Kind, conf.ResourceVersion)
			s.mutex.Unlock()
			return
		}

		// Force the emitted event to be Updated, in the event that we received a redundant Added event.
		eventKind = event.Updated
	}

	// Store the new value.
	ns[conf.Name] = conf

	s.mutex.Unlock()

	s.handler(s.schema, eventKind, prev, conf)
}

func (s *collectionStore) onDeleted(e event.Event) {
	// Remove the config from the map.
	fullName := e.Resource.Metadata.FullName

	s.mutex.Lock()

	// Lookup the storage for the namespace.
	nameStore, ok := s.store[fullName.Namespace.String()]
	var conf *model.Config
	if ok {
		// Retrieve the config value if there was one.
		conf = nameStore[fullName.Name.String()]

		// Delete the entry in the map.
		delete(nameStore, fullName.Name.String())
	}

	// If there are no remaining entries for the namespace, delete the namespace.
	if len(nameStore) == 0 {
		delete(nameStore, fullName.Namespace.String())
	}

	s.mutex.Unlock()

	if conf == nil {
		log.Infof("deleted resource not found: %s", fullName.String())
		return
	}

	s.handler(s.schema, e.Kind, nil, conf)
}

func (s *collectionStore) convertEvent(e event.Event) (*model.Config, error) {
	r := e.Resource
	schema := r.Metadata.Schema
	fullName := r.Metadata.FullName
	conf := &model.Config{
		ConfigMeta: model.ConfigMeta{
			Type:              schema.Kind(),
			Group:             schema.Group(),
			Version:           schema.Version(),
			Name:              fullName.Name.String(),
			Namespace:         fullName.Namespace.String(),
			ResourceVersion:   r.Metadata.Version.String(),
			CreationTimestamp: r.Metadata.CreateTime,
			Labels:            r.Metadata.Labels,
			Annotations:       r.Metadata.Annotations,
			Domain:            s.domainSuffix,
		},
		Spec: e.Resource.Message,
	}

	if err := schema.ValidateProto(conf.Name, conf.Namespace, conf.Spec); err != nil {
		return nil, fmt.Errorf("discarding incoming MCP resource: validation failed (%s/%s): %v",
			conf.Namespace, conf.Name, err)
	}
	return conf, nil
}

func convertEventKind(k event.Kind) model.Event {
	switch k {
	case event.Added:
		return model.EventAdd
	case event.Updated:
		return model.EventUpdate
	case event.Deleted:
		return model.EventDelete
	default:
		panic("unsupported event kind %v")
	}
}
