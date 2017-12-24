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

package cloudfoundry

import (
    "fmt"
	"time"

	copilotapi "code.cloudfoundry.org/copilot/api"
	"github.com/golang/glog"
	"golang.org/x/net/context"

	"istio.io/istio/pilot/model"
)

// AppPort is the container-side port on which Cloud Foundry Diego applications listen
const AppPort = 8080

// Controller communicates with Cloud Foundry and monitors for changes
type Controller struct {
	client           copilotapi.IstioCopilotClient
	ticker           time.Ticker
    controllerPath 	string
    handler 		*model.ControllerViewHandler	
}

func NewController(client copilotapi.IstioCopilotClient, ticker time.Ticker) *Controller {
    return &Controller {
        client: client,
        ticker: ticker,
    }
}

// Implements Mesh View Controller interface 
func (c *Controller) Handle(path string, handler *model.ControllerViewHandler) error {
    if c.handler != nil {
        err := fmt.Errorf("Cloud Foundry registry already setup to handle Mesh View at controller path '%s'",
            c.controllerPath)
        glog.Error(err.Error())
        return err
    }
    c.controllerPath = path
    c.handler = handler
    return nil
}    

// Implements Mesh View Controller interface 
func (c *Controller) Run(stop <-chan struct{}) {
    glog.Infof("Starting Cloud Foundry registry controller for controller path '%s'", c.controllerPath)
    for {
        // Block until tick
        <- c.ticker.C
        c.doReconcile()        
        select {
        case <- stop:
            return  
        }        
    }
    glog.Infof("Stopping Cloud Foundry registry controller for controller path '%s'", c.controllerPath)
}

func newService(hostname string) *model.Service {
	return &model.Service{
		Hostname: hostname,
		Ports: []*model.Port{
			{
				Port:     AppPort,
				Protocol: model.ProtocolTCP,
			},
		},
	}
}

func (c *Controller) getControllerView() (*model.ControllerView, error) {
	resp, err := c.client.Routes(context.Background(), new(copilotapi.RoutesRequest))
	if err != nil {
    	controllerErr := fmt.Errorf("Cloud Foundry registry controller '%s' failed periodic copilot routes poll with: %s", 
    	    c.controllerPath, err)
    	glog.Warning(controllerErr)
		return nil, controllerErr
	}
    controllerView := model.ControllerView {
        Path: 				c.controllerPath,
        Services:			make([]*model.Service, 0, len(resp.GetBackends())),
        ServiceInstances: 	make([]*model.ServiceInstance, 0, len(resp.GetBackends())),
    }
	for hostname := range resp.Backends {
	    service := newService(hostname)
		controllerView.Services = append(controllerView.Services, service)
    	backendSet, ok := resp.Backends[hostname]
    	if !ok {
    		continue
    	}
    	for _, backend := range backendSet.GetBackends() {
    		controllerView.ServiceInstances = append(controllerView.ServiceInstances, &model.ServiceInstance{
    			Endpoint: model.NetworkEndpoint{
    				Address:     backend.Address,
    				Port:        int(backend.Port),
    				ServicePort: service.Ports[0],
    			},
    			Service: service,
    		})
    	}
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
