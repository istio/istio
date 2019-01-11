// Copyright 2018 Istio Authors
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

package coredatamodel

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/gogo/protobuf/types"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/log"
	"istio.io/istio/pkg/mcp/sink"
)

var errUnsupported = errors.New("this operation is not supported by mcp controller")

// CoreDataModel is a combined interface for ConfigStoreCache
// MCP Updater and ServiceDiscovery
type CoreDataModel interface {
	model.ConfigStoreCache
	sink.Updater
}

// Options stores the configurable attributes of a Control
type Options struct {
	DomainSuffix string
}

// Controller is a temporary storage for the changes received
// via MCP server
type Controller struct {
	configStoreMu sync.RWMutex
	// keys [type][namespace][name]
	configStore              map[string]map[string]map[string]*model.Config
	descriptorsByMessageName map[string]model.ProtoSchema
	options                  Options
	eventHandlers            map[string][]func(model.Config, model.Event)

	syncedMu sync.Mutex
	synced   map[string]bool
}

// NewController provides a new CoreDataModel controller
func NewController(options Options) CoreDataModel {
	descriptorsByMessageName := make(map[string]model.ProtoSchema, len(model.IstioConfigTypes))
	synced := make(map[string]bool)
	for _, descriptor := range model.IstioConfigTypes {
		// don't register duplicate descriptors for the same message name, e.g. auth policy
		if _, ok := descriptorsByMessageName[descriptor.MessageName]; !ok {
			descriptorsByMessageName[descriptor.MessageName] = descriptor
			synced[descriptor.MessageName] = false
		}
	}

	return &Controller{
		configStore:              make(map[string]map[string]map[string]*model.Config),
		options:                  options,
		descriptorsByMessageName: descriptorsByMessageName,
		eventHandlers:            make(map[string][]func(model.Config, model.Event)),
		synced:                   synced,
	}
}

// ConfigDescriptor returns all the ConfigDescriptors that this
// controller is responsible for
func (c *Controller) ConfigDescriptor() model.ConfigDescriptor {
	return model.IstioConfigTypes
}

// List returns all the config that is stored by type and namespace
// if namespace is empty string it returns config for all the namespaces
func (c *Controller) List(typ, namespace string) (out []model.Config, err error) {
	_, ok := c.ConfigDescriptor().GetByType(typ)
	if !ok {
		return nil, fmt.Errorf("list unknown type %s", typ)
	}
	c.configStoreMu.Lock()
	byType, ok := c.configStore[typ]
	c.configStoreMu.Unlock()
	if !ok {
		return nil, nil
	}

	if namespace == "" {
		// ByType does not need locking since
		// we replace the entire sub-map
		for _, byNamespace := range byType {
			for _, config := range byNamespace {
				out = append(out, *config)
			}
		}
		return out, nil
	}

	for _, config := range byType[namespace] {
		out = append(out, *config)
	}
	return out, nil
}

// Apply receives changes from MCP server and creates the
// corresponding config
func (c *Controller) Apply(change *sink.Change) error {
	messagename := extractMessagename(change.Collection)
	descriptor, ok := c.descriptorsByMessageName[messagename]
	if !ok {
		return fmt.Errorf("apply type not supported %s", messagename)
	}

	schema, valid := c.ConfigDescriptor().GetByType(descriptor.Type)
	if !valid {
		return fmt.Errorf("descriptor type not supported %s", messagename)
	}

	c.syncedMu.Lock()
	c.synced[messagename] = true
	c.syncedMu.Unlock()

	// TODO Pilot's internal configuration and endpoint store is still in
	// flux. The current code assumes the following:
	//
	// * add/update/delete events are generated for external service entries
	// * a single update event is sufficient for all user authored config.
	//
	// To that end, we only generate the full set of add/update/delete events
	// for service entries. For everything else, we generate a single fake event.
	var generateAllEvents bool
	if descriptor.Type == model.ServiceEntry.Type {
		generateAllEvents = true
	}

	// validate the entire change update before committing to the internal
	// store. Accumulate the validated changes in a temporary slice without
	// holding the store's lock. Once everything is validated, grab the lock
	// and update the store.

	addedAndUpdate := make([]*model.Config, 0, len(change.Objects))
	for _, obj := range change.Objects {
		namespace, name := extractNameNamespace(obj.Metadata.Name)

		createTime := time.Now()
		if obj.Metadata.CreateTime != nil {
			var err error
			if createTime, err = types.TimestampFromProto(obj.Metadata.CreateTime); err != nil {
				return fmt.Errorf("failed to parse %v create_time: %v", obj.Metadata.Name, err)
			}
		}

		// adjust the type name for mesh-scoped resources
		typ := descriptor.Type
		if namespace == "" && descriptor.Type == model.AuthenticationPolicy.Type {
			typ = model.AuthenticationMeshPolicy.Type
		}

		conf := &model.Config{
			ConfigMeta: model.ConfigMeta{
				Type:              typ,
				Group:             descriptor.Group,
				Version:           descriptor.Version,
				Name:              name,
				Namespace:         namespace,
				ResourceVersion:   obj.Metadata.Version,
				CreationTimestamp: createTime,
				Domain:            c.options.DomainSuffix,
			},
			Spec: obj.Body,
		}

		if err := schema.Validate(conf.Name, conf.Namespace, conf.Spec); err != nil {
			return err
		}

		addedAndUpdate = append(addedAndUpdate, conf)
	}

	// commit the validated set of changes to memory
	c.configStoreMu.Lock()

	// reset internal state for type
	if !change.Incremental {
		types := []string{descriptor.Type}
		if descriptor.Type == model.AuthenticationPolicy.Type {
			types = append(types, model.AuthenticationMeshPolicy.Type)
		}

		for _, typ := range types {
			for namespace, byNamespace := range c.configStore[typ] {
				for name := range byNamespace {
					delete(byNamespace, name)
				}
				delete(c.configStore[typ], namespace)
			}
		}
	}

	dispatch := func(model model.Config, event model.Event) {}
	if handlers, ok := c.eventHandlers[descriptor.Type]; ok {
		dispatch = func(model model.Config, event model.Event) {
			log.Debugf("MCP event dispatch: key=%v event=%v", model.Key(), event.String())
			for _, handler := range handlers {
				handler(model, event)
			}
		}
	}

	// update the internal store and optionally generate add/update/delete events
	for _, obj := range addedAndUpdate {
		byType, ok := c.configStore[obj.Type]
		if !ok {
			byType = make(map[string]map[string]*model.Config)
			c.configStore[obj.Type] = byType
		}

		byNamespace, ok := byType[obj.Namespace]
		if !ok {
			byNamespace = make(map[string]*model.Config)
			byType[obj.Namespace] = byNamespace
		}

		if generateAllEvents {
			if _, ok := byNamespace[obj.Name]; ok {
				dispatch(*obj, model.EventUpdate)
			} else {
				dispatch(*obj, model.EventAdd)
			}
		}

		byNamespace[obj.Name] = obj
	}
	for _, remove := range change.Removed {
		namespace, name := extractNameNamespace(remove)

		typ := descriptor.Type
		if namespace == "" && descriptor.Type == model.AuthenticationPolicy.Type {
			typ = model.AuthenticationMeshPolicy.Type
		}

		if obj, ok := c.configStore[typ][namespace][name]; ok {
			if generateAllEvents {
				dispatch(*obj, model.EventDelete)
			}
		} else {
			log.Warnf("Removing unknown resource type=%s name=%s", typ, remove)
		}

		delete(c.configStore[typ][namespace], name)
		if len(c.configStore[typ][namespace]) == 0 {
			delete(c.configStore[typ], namespace)
		}
	}
	c.configStoreMu.Unlock()

	// TODO replace with indirect call to DiscoveryServer::ConfigUpdate()
	if !generateAllEvents {
		// dummy event to force cache invalidation
		dispatch(model.Config{}, model.EventUpdate)
	}

	return nil
}

// HasSynced returns true if the first batch of items has been popped
func (c *Controller) HasSynced() bool {
	var notReady []string

	c.syncedMu.Lock()
	for messageName, synced := range c.synced {
		if !synced {
			notReady = append(notReady, messageName)
		}
	}
	c.syncedMu.Unlock()

	if len(notReady) > 0 {
		log.Infof("Configuration not synced: first push for %v not received", notReady)
		return false
	}
	return true
}

// Run is not implemented
func (c *Controller) Run(stop <-chan struct{}) {
	log.Warnf("Run: %s", errUnsupported)
}

// RegisterEventHandler is not implemented
func (c *Controller) RegisterEventHandler(typ string, handler func(model.Config, model.Event)) {
	c.eventHandlers[typ] = append(c.eventHandlers[typ], handler)
}

// Get is not implemented
func (c *Controller) Get(typ, name, namespace string) *model.Config {
	log.Warnf("get %s", errUnsupported)
	return nil
}

// Update is not implemented
func (c *Controller) Update(config model.Config) (newRevision string, err error) {
	log.Warnf("update %s", errUnsupported)
	return "", errUnsupported
}

// Create is not implemented
func (c *Controller) Create(config model.Config) (revision string, err error) {
	log.Warnf("create %s", errUnsupported)
	return "", errUnsupported
}

// Delete is not implemented
func (c *Controller) Delete(typ, name, namespace string) error {
	return errUnsupported
}

func extractNameNamespace(metadataName string) (string, string) {
	segments := strings.Split(metadataName, "/")
	if len(segments) == 2 {
		return segments[0], segments[1]
	}
	return "", segments[0]
}

func extractMessagename(typeURL string) string {
	if slash := strings.LastIndex(typeURL, "/"); slash >= 0 {
		return typeURL[slash+1:]
	}
	return typeURL
}
