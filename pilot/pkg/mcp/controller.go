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

	"istio.io/istio/pilot/pkg/model"
)

var errUnsupported = errors.New("this operation is not supported by mcp controller")

//go:generate counterfeiter -o fakes/logger.go --fake-name Logger . logger
type logger interface {
	Warnf(template string, args ...interface{})
	Infof(template string, args ...interface{})
}

type Controller struct {
	configStore model.ConfigStore
	eventCh     chan func(model.Config, model.Event)
	descriptors map[string]model.ProtoSchema
	logger      logger
}

func NewController(store model.ConfigStore, logger logger) *Controller {
	descriptors := make(map[string]model.ProtoSchema, len(model.IstioConfigTypes))
	for _, config := range model.IstioConfigTypes {
		descriptors[config.Type] = config
	}

	return &Controller{
		configStore: store,
		eventCh:     make(chan func(model.Config, model.Event)),
		descriptors: descriptors,
		logger:      logger,
	}
}

func (c *Controller) Run(stop <-chan struct{}) {
	for {
		select {
		case <-stop:
			if _, ok := <-c.eventCh; ok {
				close(c.eventCh)
			}
			return
		case event, ok := <-c.eventCh:
			if ok {
				// TODO: do we need to call the events with any other args?
				event(model.Config{}, model.EventUpdate)
			}
		}
	}
}

func (c *Controller) RegisterEventHandler(typ string, handler func(model.Config, model.Event)) {
	c.eventCh <- handler
}

func (c *Controller) ConfigDescriptor() model.ConfigDescriptor {
	return model.IstioConfigTypes
}

func (c *Controller) Get(typ, name, namespace string) (config *model.Config, exists bool) {
	if err := c.Exist(typ); err != nil {
		c.logger.Infof("Get: %s", err)
		return nil, false
	}
	return c.configStore.Get(typ, name, namespace)
}

func (c *Controller) List(typ, namespace string) ([]model.Config, error) {
	if err := c.Exist(typ); err != nil {
		return nil, err
	}
	return c.configStore.List(typ, namespace)
}

func (c *Controller) Update(config model.Config) (newRevision string, err error) {
	if err := c.Exist(config.ConfigMeta.Type); err != nil {
		c.logger.Infof("Update: %s", err)
		return "", err
	}
	return c.configStore.Update(config)
}

//TODO: assuming always synced??
func (c *Controller) HasSynced() bool {
	return true
}

func (c *Controller) Exist(typ string) error {
	if _, ok := c.descriptors[typ]; !ok {
		return fmt.Errorf("type not supported: %s", typ)
	}
	return nil
}

//	noop
func (c *Controller) Create(config model.Config) (revision string, err error) {
	return "", errUnsupported
}

func (c *Controller) Delete(typ, name, namespace string) error {
	return errUnsupported
}
