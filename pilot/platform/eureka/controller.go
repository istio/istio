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
	"fmt"
	"time"

	"istio.io/istio/pilot/model"
	"istio.io/istio/pkg/log"
)

// Controller communicates with Eureka and monitors for changes
type Controller struct {
	client         Client
	ticker         time.Ticker
	controllerPath string
	handler        *model.ControllerViewHandler
}

// NewController instantiates a new Eureka controller
func NewController(client Client, ticker time.Ticker) *Controller {
	return &Controller{
		client: client,
		ticker: ticker,
	}
}

// Handle implements model.Controller interface
func (c *Controller) Handle(path string, handler *model.ControllerViewHandler) error {
	if c.handler != nil {
		err := fmt.Errorf("eureka registry already setup to handle mesh view at controller path '%s'",
			c.controllerPath)
		log.Error(err.Error())
		return err
	}
	c.controllerPath = path
	c.handler = handler
	return nil
}

// Run implements model.Controller interface
func (c *Controller) Run(stop <-chan struct{}) {
	log.Infof("Starting Eureka registry controller for controller path '%s'", c.controllerPath)
	for {
		// Block until tick
		<-c.ticker.C
		c.doReconcile()
		select {
		case <-stop:
			log.Infof("Stopping Eureka registry controller for controller path '%s'", c.controllerPath)
			return
		default:
		}
	}
}

func (c *Controller) getControllerView() (*model.ControllerView, error) {
	apps, err := c.client.Applications()
	if err != nil {
		controllerErr := fmt.Errorf("eureka registry controller '%s' failed to list eureka instances with: %s",
			c.controllerPath, err)
		log.Warn(controllerErr.Error())
		return nil, controllerErr
	}
	controllerView := model.ControllerView{
		Path:             c.controllerPath,
		Services:         make([]*model.Service, 0),
		ServiceInstances: make([]*model.ServiceInstance, 0),
	}
	hostSvcMap := convertServices(apps, nil)
	for _, svc := range hostSvcMap {
		controllerView.Services = append(controllerView.Services, svc)
	}
	for _, instance := range convertServiceInstances(hostSvcMap, apps) {
		controllerView.ServiceInstances = append(controllerView.ServiceInstances, instance)
	}
	return &controllerView, nil
}

func (c *Controller) doReconcile() {
	controllerView, err := c.getControllerView()
	if err != nil {
		return
	}
	(*c.handler).Reconcile(controllerView)
}
