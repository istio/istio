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
	mcpclient "istio.io/istio/pkg/mcp/client"
)

var errUnsupported = errors.New("this operation is not supported by mcp controller")

// CoreDataModel is a combined interface for ConfigStoreCache
// MCP Updater and ServiceDiscovery
type CoreDataModel interface {
	model.ConfigStoreCache
	mcpclient.Updater
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
	configStore              map[string]map[string]map[string]model.Config
	descriptorsByMessageName map[string]model.ProtoSchema
	options                  Options
	eventHandlers            map[string][]func(model.Config, model.Event)
}

// NewController provides a new CoreDataModel controller
func NewController(options Options) CoreDataModel {
	descriptorsByMessageName := make(map[string]model.ProtoSchema, len(model.IstioConfigTypes))
	for _, descriptor := range model.IstioConfigTypes {
		// don't register duplicate descriptors for the same message name, e.g. auth policy
		if _, ok := descriptorsByMessageName[descriptor.MessageName]; !ok {
			descriptorsByMessageName[descriptor.MessageName] = descriptor
		}
	}

	// Remove this when https://github.com/istio/istio/issues/7947 is done
	configStore := make(map[string]map[string]map[string]model.Config)
	for _, typ := range model.IstioConfigTypes.Types() {
		configStore[typ] = make(map[string]map[string]model.Config)
	}
	return &Controller{
		configStore:              configStore,
		options:                  options,
		descriptorsByMessageName: descriptorsByMessageName,
		eventHandlers:            make(map[string][]func(model.Config, model.Event)),
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
				out = append(out, config)
			}
		}
		return out, nil
	}

	for _, config := range byType[namespace] {
		out = append(out, config)
	}
	return out, nil
}

// Apply receives changes from MCP server and creates the
// corresponding config
func (c *Controller) Apply(change *mcpclient.Change) error {
	messagename := extractMessagename(change.TypeURL)
	descriptor, ok := c.descriptorsByMessageName[messagename]
	if !ok {
		return fmt.Errorf("apply type not supported %s", messagename)
	}

	schema, valid := c.ConfigDescriptor().GetByType(descriptor.Type)
	if !valid {
		return fmt.Errorf("descriptor type not supported %s", messagename)
	}

	// innerStore is [namespace][name]
	innerStore := make(map[string]map[string]model.Config)
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

		conf := model.Config{
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
			Spec: obj.Resource,
		}

		if err := schema.Validate(conf.Name, conf.Namespace, conf.Spec); err != nil {
			return err
		}

		namedConfig, ok := innerStore[conf.Namespace]
		if ok {
			namedConfig[conf.Name] = conf
		} else {
			innerStore[conf.Namespace] = map[string]model.Config{
				conf.Name: conf,
			}
		}
	}

	// de-mux namespace and cluster-scoped authentication policy from the same
	// type_url stream.
	const clusterScopedNamespace = ""
	meshTypeInnerStore := make(map[string]map[string]model.Config)
	var meshType string
	if descriptor.Type == model.AuthenticationPolicy.Type {
		meshType = model.AuthenticationMeshPolicy.Type
		meshTypeInnerStore[clusterScopedNamespace] = innerStore[clusterScopedNamespace]
		delete(innerStore, clusterScopedNamespace)
	}

	var prevStore map[string]map[string]model.Config

	c.configStoreMu.Lock()
	prevStore = c.configStore[descriptor.Type]
	c.configStore[descriptor.Type] = innerStore
	if meshType != "" {
		c.configStore[meshType] = meshTypeInnerStore
	}
	c.configStoreMu.Unlock()

	dispatch := func(model model.Config, event model.Event) {}
	if handlers, ok := c.eventHandlers[descriptor.Type]; ok {
		dispatch = func(model model.Config, event model.Event) {
			for _, handler := range handlers {
				handler(model, event)
			}
		}
	}

	// add/update
	for namespace, byName := range innerStore {
		for name, config := range byName {
			if prevByNamespace, ok := prevStore[namespace]; ok {
				if prevConfig, ok := prevByNamespace[name]; ok {
					if config.ResourceVersion != prevConfig.ResourceVersion {
						dispatch(config, model.EventUpdate)
					}
				} else {
					dispatch(config, model.EventAdd)
				}
			} else {
				dispatch(config, model.EventAdd)
			}
		}
	}

	// remove
	for namespace, prevByName := range prevStore {
		for name, prevConfig := range prevByName {
			if byNamespace, ok := innerStore[namespace]; !ok {
				if _, ok := byNamespace[name]; !ok {
					dispatch(prevConfig, model.EventDelete)
				}
			} else {
				dispatch(prevConfig, model.EventDelete)
			}
		}
	}

	return nil
}

// HasSynced is not implemented, waiting for the following issue
// pending https://github.com/istio/istio/issues/7947
func (c *Controller) HasSynced() bool {
	// TODO:The configStore already populated with all the keys to avoid nil map issue
	if len(c.configStore) == 0 {
		return false
	}
	for _, descriptor := range c.ConfigDescriptor() {
		if _, ok := c.configStore[descriptor.Type]; !ok {
			return false
		}

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
