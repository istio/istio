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

package consul

import (
    "fmt"
	"time"

	"github.com/golang/glog"
	"github.com/hashicorp/consul/api"

	"istio.io/istio/pilot/model"
)

// Controller communicates with Consul and monitors for changes
type Controller struct {
	client     		*api.Client
	dataCenter 		string
	ticker 			time.Ticker
    controllerPath 	string
    handler 		*model.ControllerViewHandler	
}

// NewController creates a new Consul controller
func NewController(addr, datacenter string, ticker time.Ticker) (*Controller, error) {
	conf := api.DefaultConfig()
	conf.Address = addr
	client, err := api.NewClient(conf)
	if err != nil {
	    return nil, err
	}
	return &Controller{
		client:     client,
		dataCenter: datacenter,
		ticker:   	ticker,
	}, nil
}

// Implements Mesh View Controller interface 
func (c *Controller) Handle(path string, handler *model.ControllerViewHandler) error {
    if c.handler != nil {
        err := fmt.Errorf("Consul registry for datacenter '%s' already setup to handle Mesh View at controller path '%s'",
            c.dataCenter, c.controllerPath)
        glog.Error(err.Error())
        return err
    }
    c.controllerPath = path
    c.handler = handler
    return nil
}    

// Implements Mesh View Controller interface 
func (c *Controller) Run(stop <-chan struct{}) {
    glog.Infof("Starting Consul registry controller for controller path '%s'", c.controllerPath)
    for {
        // Block until tick
        <- c.ticker.C
        c.doReconcile()        
        select {
        case <- stop:
            return  
        }        
    }
    glog.Infof("Stopping Consul registry controller for controller path '%s'", c.controllerPath)
}

func (c *Controller) getServices() (map[string][]string, error) {
	data, _, err := c.client.Catalog().Services(nil)
	if err != nil {
		glog.Warningf("Could not retrieve services from consul: %v", err)
		return nil, err
	}

	return data, nil
}

func (c *Controller) getCatalogService(name string, q *api.QueryOptions) ([]*api.CatalogService, error) {
	endpoints, _, err := c.client.Catalog().Service(name, "", q)
	if err != nil {
		return nil, err
	}

	return endpoints, nil
}

func (c *Controller) doReconcile() {
	data, err := c.getServices()
	if err != nil {
	    glog.Warning("Consul registry controller '%d' for datacenter '%s' unable to fetch services. Mesh view likely to become stale: '%v'", 
	        c.controllerPath, c.dataCenter, err.Error())
	    return
	}
	consulView := model.ControllerView {
	    Path: c.controllerPath,
    	Services: []*model.Service{},
    	ServiceInstances: []*model.ServiceInstance{},
	}
	for name := range data {
		endpoints, err := c.getCatalogService(name, nil)
		if err != nil {
    	    glog.Warning("Consul registry for datacenter '%s' removing service with name '%s' from Mesh view. Error fetching service details: '%v'", 
    	        c.dataCenter, name, err.Error())
    	    continue
		}
		consulView.Services = append(consulView.Services, convertService(endpoints))
    	for _, endpoint := range endpoints {
    		consulView.ServiceInstances = append(consulView.ServiceInstances, convertInstance(endpoint))
    	}
	}
	(*c.handler).Reconcile(&consulView)
}

