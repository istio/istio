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

package cloudfoundry

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	copilotapi "code.cloudfoundry.org/copilot/api"
	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/model"
)

var unsupportedErr = errors.New("this operation is not supported by the cloudfoundry copilot controller")

// CopilotClient defines a local interface for interacting with Cloud Foundry Copilot
//go:generate counterfeiter -o fakes/copilot_client.go --fake-name CopilotClient . CopilotClient
type CopilotClient interface {
	copilotapi.IstioCopilotClient
}

type storage struct {
	sync.RWMutex
	virtualServices  map[string]*model.Config
	destinationRules map[string]*model.Config
}

type controller struct {
	client        CopilotClient
	store         model.ConfigStore
	timeout       time.Duration
	checkInterval time.Duration
	eventHandlers []func(model.Config, model.Event)
	storage       storage
}

func NewController(client CopilotClient, store model.ConfigStore, timeout, checkInterval time.Duration) *controller {
	return &controller{
		client:        client,
		store:         store,
		timeout:       timeout,
		checkInterval: checkInterval,
		storage: storage{
			virtualServices:  make(map[string]*model.Config),
			destinationRules: make(map[string]*model.Config),
		},
	}
}

func (c *controller) RegisterEventHandler(typ string, f func(model.Config, model.Event)) {
	c.eventHandlers = append(c.eventHandlers, f)
}

func (c *controller) HasSynced() bool {
	return len(c.storage.virtualServices) > 0 && len(c.storage.destinationRules) > 0
}

func (c *controller) Run(stop <-chan struct{}) {
	tick := time.NewTicker(c.checkInterval)

	go func() {
		for {
			select {
			case <-stop:
				tick.Stop()
				return
			case <-tick.C:
				c.rebuildRules()
				c.runHandlers()
			}
		}
	}()
}

func (c *controller) ConfigDescriptor() model.ConfigDescriptor {
	return model.ConfigDescriptor{
		model.VirtualService,
		model.DestinationRule,
	}
}

func (c *controller) Get(typ, name, namespace string) (*model.Config, bool) {
	c.storage.RLock()
	defer c.storage.RUnlock()

	var (
		config *model.Config
		found  bool
	)

	switch typ {
	case model.VirtualService.Type:
		if vs, ok := c.storage.virtualServices[name]; ok {
			config = vs
			found = true
		}
	case model.DestinationRule.Type:
		if dr, ok := c.storage.destinationRules[name]; ok {
			config = dr
			found = true
		}
	default:
	}

	return config, found
}

func (c *controller) List(typ, namespace string) ([]model.Config, error) {
	var configs []model.Config

	c.storage.RLock()
	switch typ {
	case model.VirtualService.Type:
		for _, service := range c.storage.virtualServices {
			configs = append(configs, *service)
		}
	case model.DestinationRule.Type:
		for _, rule := range c.storage.destinationRules {
			configs = append(configs, *rule)
		}
	default:
	}
	c.storage.RUnlock()

	sort.Slice(configs, func(i, j int) bool { return configs[i].Key() < configs[j].Key() })

	return configs, nil
}

func (c *controller) Create(config model.Config) (string, error) {
	return "", unsupportedErr
}

func (c *controller) Update(config model.Config) (string, error) {
	return "", unsupportedErr
}

func (c *controller) Delete(typ, name, namespace string) error {
	return unsupportedErr
}

func (c *controller) rebuildRules() {
	ctx, cancel := context.WithTimeout(context.Background(), c.timeout)
	defer cancel()

	resp, err := c.client.Routes(ctx, new(copilotapi.RoutesRequest))
	if err != nil {
		panic(err)
	}

	gateways, err := c.store.List(model.Gateway.Type, model.NamespaceAll)
	if err != nil {
		panic(err)
	}

	var gatewayNames []string
	for _, gwConfig := range gateways {
		gatewayNames = append(gatewayNames, gwConfig.Name)
	}

	virtualServices := make(map[string]*model.Config)
	destinationRules := make(map[string]*model.Config)

	for _, route := range resp.GetRoutes() {
		destinationRuleName := fmt.Sprintf("dest-rule-for-%s", route.GetHostname())
		virtualServiceName := fmt.Sprintf("virtual-service-for-%s", route.GetHostname())

		var dr *networking.DestinationRule

		if config, ok := destinationRules[destinationRuleName]; ok {
			dr = config.Spec.(*networking.DestinationRule)
		} else {
			dr = createDestinationRule(route)
		}
		dr.Subsets = append(dr.Subsets, createSubset(route.GetCapiProcessGuid()))

		var vs *networking.VirtualService
		if config, ok := virtualServices[virtualServiceName]; ok {
			vs = config.Spec.(*networking.VirtualService)
		} else {
			vs = createVirtualService(gatewayNames, route)
		}

		r := createRoute(route)
		r.Route[0].Destination.Subset = route.GetCapiProcessGuid()

		if route.GetPath() != "" {
			r.Match = createMatchRequest(route)
			vs.Http = append([]*networking.HTTPRoute{r}, vs.Http...)
		} else {
			vs.Http = append(vs.Http, r)
		}

		virtualServices[virtualServiceName] = &model.Config{
			ConfigMeta: model.ConfigMeta{
				Type:    model.VirtualService.Type,
				Version: model.VirtualService.Version,
				Name:    virtualServiceName,
			},
			Spec: vs,
		}
		destinationRules[destinationRuleName] = &model.Config{
			ConfigMeta: model.ConfigMeta{
				Type:    model.DestinationRule.Type,
				Version: model.DestinationRule.Version,
				Name:    destinationRuleName,
			},
			Spec: dr,
		}
	}

	c.storage.Lock()
	c.storage.virtualServices = virtualServices
	c.storage.destinationRules = destinationRules
	c.storage.Unlock()
}

func (c *controller) runHandlers() {
	for _, h := range c.eventHandlers {
		h(model.Config{}, model.EventUpdate)
	}
}

func createVirtualService(gatewayNames []string, route *copilotapi.RouteWithBackends) *networking.VirtualService {
	return &networking.VirtualService{
		Gateways: gatewayNames,
		Hosts:    []string{route.GetHostname()},
	}
}

func createRoute(route *copilotapi.RouteWithBackends) *networking.HTTPRoute {
	return &networking.HTTPRoute{
		Route: []*networking.DestinationWeight{
			{
				Destination: &networking.Destination{
					Host: route.GetHostname(),
					Port: &networking.PortSelector{
						Port: &networking.PortSelector_Number{
							Number: 8080,
						},
					},
				},
			},
		},
	}
}

func createMatchRequest(route *copilotapi.RouteWithBackends) []*networking.HTTPMatchRequest {
	return []*networking.HTTPMatchRequest{
		{
			Uri: &networking.StringMatch{
				MatchType: &networking.StringMatch_Prefix{
					Prefix: route.GetPath(),
				},
			},
		},
	}
}

func createSubset(capiProcessGUID string) *networking.Subset {
	return &networking.Subset{
		Name:   capiProcessGUID,
		Labels: map[string]string{"cfapp": capiProcessGUID},
	}
}

func createDestinationRule(route *copilotapi.RouteWithBackends) *networking.DestinationRule {
	return &networking.DestinationRule{
		Host: route.GetHostname(),
	}
}
