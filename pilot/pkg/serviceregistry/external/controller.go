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
	"reflect"
	"sort"
	"time"

	"istio.io/istio/pilot/pkg/model"
)

type serviceHandler func(*model.Service, model.Event)
type instanceHandler func(*model.ServiceInstance, model.Event)

type controller struct {
	interval         time.Duration
	serviceHandlers  []serviceHandler
	instanceHandlers []instanceHandler
	config           model.IstioConfigStore
}

// NewController instantiates a new Eureka controller
func NewController(config model.IstioConfigStore, interval time.Duration) model.Controller {
	return &controller{
		interval:         interval,
		serviceHandlers:  make([]serviceHandler, 0),
		instanceHandlers: make([]instanceHandler, 0),
		config:           config,
	}
}

func (c *controller) AppendServiceHandler(f func(*model.Service, model.Event)) error {
	c.serviceHandlers = append(c.serviceHandlers, f)
	return nil
}

func (c *controller) AppendInstanceHandler(f func(*model.ServiceInstance, model.Event)) error {
	c.instanceHandlers = append(c.instanceHandlers, f)
	return nil
}

func (c *controller) Run(stop <-chan struct{}) {
	cachedConfigs := configs{}
	ticker := time.NewTicker(c.interval)
	for {
		select {
		case <-ticker.C:
			externalServiceConfigs := c.config.ExternalServices()

			newRecord := configs(externalServiceConfigs)
			sort.Sort(newRecord)

			if !reflect.DeepEqual(newRecord, cachedConfigs) {
				cachedConfigs = newRecord
				// TODO: feed with real events.
				// The handlers are being fed dummy events. This is sufficient with simplistic
				// handlers that invalidate the cache on any event but will not work with smarter
				// handlers.
				for _, h := range c.serviceHandlers {
					go h(&model.Service{}, model.EventAdd)
				}
				for _, h := range c.instanceHandlers {
					go h(&model.ServiceInstance{}, model.EventAdd)
				}
			}
		case <-stop:
			ticker.Stop()
			return
		}
	}
}

type configs []model.Config

// Len of the array
func (c configs) Len() int {
	return len(c)
}

// Swap i and j
func (c configs) Swap(i, j int) {
	c[i], c[j] = c[j], c[i]
}

// Less i and j
func (c configs) Less(i, j int) bool {
	return c[i].Name < c[j].Name
}
