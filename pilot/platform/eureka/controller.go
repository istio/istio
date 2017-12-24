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

	"github.com/golang/glog"

	"istio.io/istio/pilot/model"
)

type controller struct {
	client           Client
	ticker           time.Ticker
    controllerPath 	string
    handler 		*model.ControllerViewHandler	
}

// NewController instantiates a new Eureka controller
func NewController(client Client, ticker time.Ticker) *controller {
    return &controller {
        client: client,
        ticker: ticker,
    }
}

// Implements Mesh View Controller interface 
func (c *controller) Handle(path string, handler *model.ControllerViewHandler) error {
    if c.handler != nil {
        err := fmt.Errorf("Eureka registry already setup to handle Mesh View at controller path '%s'",
            c.controllerPath)
        glog.Error(err.Error())
        return err
    }
    c.controllerPath = path
    c.handler = handler
    return nil
}    

// Implements Mesh View Controller interface 
func (c *controller) Run(stop <-chan struct{}) {
    glog.Infof("Starting Eureka registry controller for controller path '%s'", c.controllerPath)
    for {
        // Block until tick
        <- c.ticker.C
        c.doReconcile()        
        select {
        case <- stop:
            return  
        }        
    }
    glog.Infof("Stopping Eureka registry controller for controller path '%s'", c.controllerPath)
}

func (c *controller) getControllerView() (*model.ControllerView, error) {
	apps, err := c.client.Applications()
	if err != nil {
    	controllerErr := fmt.Errorf("Eureka registry controller '%s' failed to list Eureka instances with: %s", 
    	    c.controllerPath, err)
    	glog.Warning(controllerErr)
		return nil, controllerErr
	}
    controllerView := model.ControllerView {
        Path: 				c.controllerPath,
        Services:			make([]*model.Service, 0),
        ServiceInstances: 	make([]*model.ServiceInstance, 0),
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


func (c *controller) doReconcile() {
	controllerView, err := c.getControllerView()
	if err != nil {
	    return
	}
    (*c.handler).Reconcile(controllerView)
}
