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

package eureka

import (
	"reflect"
	"time"

	"github.com/golang/glog"

	"istio.io/istio/pilot/model"
)

type serviceHandler func(*model.Service, model.Event)
type instanceHandler func(*model.ServiceInstance, model.Event)

type controller struct {
	interval         time.Duration
	serviceHandlers  []serviceHandler
	instanceHandlers []instanceHandler
	client           Client
}

// NewController instantiates a new Eureka controller
func NewController(client Client, interval time.Duration) model.Controller {
	return &controller{
		interval:         interval,
		serviceHandlers:  make([]serviceHandler, 0),
		instanceHandlers: make([]instanceHandler, 0),
		client:           client,
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
	cachedApps := make([]*application, 0)
	ticker := time.NewTicker(c.interval)
	for {
		select {
		case <-ticker.C:
			apps, err := c.client.Applications()
			if err != nil {
				glog.Warningf("periodic Eureka poll failed: %v", err)
				continue
			}
			sortApplications(apps)

			if !reflect.DeepEqual(apps, cachedApps) {
				cachedApps = apps
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
