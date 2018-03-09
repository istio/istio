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

package external

import (
	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/model"
)

type serviceHandler func(*model.Service, model.Event)
type instanceHandler func(*model.ServiceInstance, model.Event)

type controller struct {
	serviceHandlers  []serviceHandler
	instanceHandlers []instanceHandler
	configStore      model.ConfigStoreCache
}

// NewController instantiates a new ExternalServices controller
func NewController(configStore model.ConfigStoreCache) model.Controller {
	c := &controller{
		serviceHandlers:  make([]serviceHandler, 0),
		instanceHandlers: make([]instanceHandler, 0),
		configStore:      configStore,
	}

	configStore.RegisterEventHandler(model.ExternalService.Type, func(config model.Config, event model.Event) {
		externalSvc := config.Spec.(*networking.ExternalService)

		services := convertService(externalSvc)
		for _, handler := range c.serviceHandlers {
			for _, service := range services {
				go handler(service, event)
			}
		}

		instances := convertInstances(externalSvc)
		for _, handler := range c.instanceHandlers {
			for _, instance := range instances {
				go handler(instance, event)
			}
		}
	})

	return c
}

func (c *controller) AppendServiceHandler(f func(*model.Service, model.Event)) error {
	c.serviceHandlers = append(c.serviceHandlers, f)
	return nil
}

func (c *controller) AppendInstanceHandler(f func(*model.ServiceInstance, model.Event)) error {
	c.instanceHandlers = append(c.instanceHandlers, f)
	return nil
}

func (c *controller) Run(stop <-chan struct{}) {}
