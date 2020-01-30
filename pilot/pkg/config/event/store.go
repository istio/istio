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
	"errors"
	"fmt"

	"istio.io/pkg/ledger"
	"istio.io/pkg/log"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/config/event"
	"istio.io/istio/pkg/config/schema/collection"
	"istio.io/istio/pkg/config/schema/collections"
	resourceSchema "istio.io/istio/pkg/config/schema/resource"
)

var (
	errUnsupported        = errors.New("this operation is not supported by event-based config store")
	supportedSchemas      = collections.Pilot
	supportedSchemasArray = supportedSchemas.All()
	serviceEntryKind      = collections.IstioNetworkingV1Alpha3Serviceentries.Resource().GroupVersionKind()
)

const (
	ledgerLogf = "error tracking pilot config versions for mcp distribution: %v"
)

// Options stores the configurable attributes of a Control
type Options struct {
	ClusterID    string
	DomainSuffix string
	XDSUpdater   model.XDSUpdater
	ConfigLedger ledger.Ledger
}

// store is a model.ConfigStoreCache populated by events.
type store struct {
	serviceEntryHandlers []func(model.Config, model.Config, model.Event)
	ledger               ledger.Ledger
	byGVK                map[resourceSchema.GroupVersionKind]*collectionStore
	byCollection         map[collection.Name]*collectionStore
	xdsUpdater           model.XDSUpdater
}

// NewConfigStoreAndHandler creates a new model.ConfigStoreCache and an event.Handler used to populate it.
func NewConfigStoreAndHandler(options *Options) (model.ConfigStoreCache, event.Handler) {
	s := &store{
		serviceEntryHandlers: make([]func(model.Config, model.Config, model.Event), 0),
		ledger:               options.ConfigLedger,
		byGVK:                make(map[resourceSchema.GroupVersionKind]*collectionStore, len(supportedSchemasArray)),
		byCollection:         make(map[collection.Name]*collectionStore, len(supportedSchemasArray)),
		xdsUpdater:           options.XDSUpdater,
	}

	// Create the collection stores.
	for _, schema := range supportedSchemasArray {
		cstore := newCollectionStore(schema, options.DomainSuffix, s.onEvent)
		s.byGVK[schema.Resource().GroupVersionKind()] = cstore
		s.byCollection[schema.Name()] = cstore
	}

	return s, s
}

func (s *store) onEvent(schema collection.Schema, eventKind event.Kind, prev, conf *model.Config) {
	// Update the ledger.
	ledgerKey := model.Key(schema.Resource().Kind(), conf.Namespace, conf.Name)
	switch eventKind {
	case event.Added, event.Updated:
		_, err := s.ledger.Put(ledgerKey, conf.ResourceVersion)
		if err != nil {
			log.Warnf(ledgerLogf, err)
		}
	case event.Deleted:
		err := s.ledger.Delete(ledgerKey)
		if err != nil {
			log.Warnf(ledgerLogf, err)

		}
	}

	gvk := schema.Resource().GroupVersionKind()
	if gvk == serviceEntryKind {
		s.dispatchServiceEntry(prev, conf, convertEventKind(eventKind))
	} else {
		s.xdsUpdater.ConfigUpdate(&model.PushRequest{
			Full:               true,
			ConfigTypesUpdated: map[resourceSchema.GroupVersionKind]struct{}{gvk: {}},
			Reason:             []model.TriggerReason{model.ConfigUpdate},
		})
	}
}

func (s *store) Schemas() collection.Schemas {
	return supportedSchemas
}

// List returns all the config that is stored by type and namespace
// if namespace is empty string it returns config for all the namespaces
func (s *store) List(gvk resourceSchema.GroupVersionKind, namespace string) (out []model.Config, err error) {
	cache, ok := s.byGVK[gvk]
	if !ok {
		return nil, fmt.Errorf("list unknown type %s", gvk)
	}

	return cache.List(namespace)
}

func (s *store) Handle(e event.Event) {
	cache, ok := s.byCollection[e.Source.Name()]
	if !ok {
		log.Warnf("received event for unsupported collection: %s", e.Source.Name())
		return
	}

	cache.Handle(e)
}

// HasSynced returns true if the first batch of items has been popped
func (s *store) HasSynced() bool {
	for _, cache := range s.byGVK {
		if !cache.HasSynced() {
			return false
		}
	}

	log.Infof("Configuration synced")
	return true
}

// RegisterEventHandler registers a handler using the type as a key
func (s *store) RegisterEventHandler(kind resourceSchema.GroupVersionKind, handler func(model.Config, model.Config, model.Event)) {
	if kind == serviceEntryKind {
		s.serviceEntryHandlers = append(s.serviceEntryHandlers, handler)
	}
}

func (s *store) Version() string {
	return s.ledger.RootHash()
}

func (s *store) GetResourceAtVersion(version string, key string) (resourceVersion string, err error) {
	return s.ledger.GetPreviousValue(version, key)
}

// Run is not implemented
func (s *store) Run(<-chan struct{}) {
	log.Warnf("Run: %s", errUnsupported)
}

// Get is not implemented
func (s *store) Get(_ resourceSchema.GroupVersionKind, _, _ string) *model.Config {
	log.Warnf("get %s", errUnsupported)
	return nil
}

// Update is not implemented
func (s *store) Update(_ model.Config) (newRevision string, err error) {
	log.Warnf("update %s", errUnsupported)
	return "", errUnsupported
}

// Create is not implemented
func (s *store) Create(_ model.Config) (revision string, err error) {
	log.Warnf("create %s", errUnsupported)
	return "", errUnsupported
}

// Delete is not implemented
func (s *store) Delete(_ resourceSchema.GroupVersionKind, _, _ string) error {
	return errUnsupported
}

func (s *store) dispatchServiceEntry(prevCfg *model.Config, cfg *model.Config, e model.Event) {
	var prev model.Config
	if prevCfg != nil {
		prev = *prevCfg
	}
	if len(s.serviceEntryHandlers) > 0 {
		if log.DebugEnabled() {
			log.Debugf("MCP event dispatch: key=%v event=%v", cfg.Key(), e.String())
		}
		for _, handler := range s.serviceEntryHandlers {
			handler(prev, *cfg, e)
		}
	}
}

func (s *store) serviceEntryEvents(currentStore, prevStore map[string]map[string]*model.Config) {
	// add/update
	for namespace, byName := range currentStore {
		for name, cfg := range byName {
			if prevByNamespace, ok := prevStore[namespace]; ok {
				if prevConfig, ok := prevByNamespace[name]; ok {
					if cfg.ResourceVersion != prevConfig.ResourceVersion {
						s.dispatchServiceEntry(prevConfig, cfg, model.EventUpdate)
					}
				} else {
					s.dispatchServiceEntry(nil, cfg, model.EventAdd)
				}
			} else {
				s.dispatchServiceEntry(nil, cfg, model.EventAdd)
			}
		}
	}

	// delete
	for namespace, prevByName := range prevStore {
		for name, prevConfig := range prevByName {
			if _, ok := currentStore[namespace][name]; !ok {
				s.dispatchServiceEntry(nil, prevConfig, model.EventDelete)
			}
		}
	}
}
