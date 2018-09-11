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

// Controller is a temporary storage for the changes received
// via MCP server
type Controller struct {
	configStoreMu sync.RWMutex
	// keys [type][namespace][name]
	configStore              map[string]map[string]map[string]model.Config
	descriptorsByMessageName map[string]model.ProtoSchema
}

// NewController provides a new CoreDataModel controller
func NewController() CoreDataModel {
	descriptorsByMessageName := make(map[string]model.ProtoSchema, len(model.IstioConfigTypes))
	for _, descriptor := range model.IstioConfigTypes {
		descriptorsByMessageName[descriptor.MessageName] = descriptor
	}

	// Remove this when https://github.com/istio/istio/issues/7947 is done
	configStore := make(map[string]map[string]map[string]model.Config)
	for _, typ := range model.IstioConfigTypes.Types() {
		configStore[typ] = make(map[string]map[string]model.Config)
	}
	return &Controller{
		configStore:              configStore,
		descriptorsByMessageName: descriptorsByMessageName,
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
		if ok {
			for _, byNamespace := range byType {
				for _, config := range byNamespace {
					out = append(out, config)
				}
			}
			return out, nil
		}
	}

	if byNamespace, ok := byType[namespace]; ok {
		for _, config := range byNamespace {
			out = append(out, config)
		}
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
		name, nameSpace := extractNameNamespace(obj.Metadata.Name)

		createTime := time.Now()
		if obj.Metadata.CreateTime != nil {
			var err error
			if createTime, err = types.TimestampFromProto(obj.Metadata.CreateTime); err != nil {
				return fmt.Errorf("failed to parse %v create_time: %v", obj.Metadata.Name, err)
			}
		}

		conf := model.Config{
			ConfigMeta: model.ConfigMeta{
				Type:              descriptor.Type,
				Group:             descriptor.Group,
				Version:           descriptor.Version,
				Name:              name,
				Namespace:         nameSpace,
				ResourceVersion:   obj.Metadata.Version,
				CreationTimestamp: createTime,
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

	c.configStoreMu.Lock()
	c.configStore[descriptor.Type] = innerStore
	c.configStoreMu.Unlock()

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
	log.Warnf("registerEventHandler %s", errUnsupported)
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
	return segments[0], ""
}

func extractMessagename(typeURL string) string {
	if slash := strings.LastIndex(typeURL, "/"); slash >= 0 {
		return typeURL[slash+1:]
	}
	return typeURL
}
