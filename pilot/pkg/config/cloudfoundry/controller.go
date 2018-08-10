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
	"sync"
	"time"

	copilotapi "code.cloudfoundry.org/copilot/api"

	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/model"
)

var errUnsupported = errors.New("this operation is not supported by the cloudfoundry copilot controller")

// CopilotClient defines a local interface for interacting with Cloud Foundry Copilot
//go:generate $GOPATH/src/istio.io/istio/bin/counterfeiter.sh -o fakes/copilot_client.go --fake-name CopilotClient . CopilotClient
type CopilotClient interface {
	copilotapi.IstioCopilotClient
}

type storage struct {
	sync.RWMutex
	virtualServices  map[string]*model.Config
	destinationRules map[string]*model.Config
}

//go:generate $GOPATH/src/istio.io/istio/bin/counterfeiter.sh -o fakes/logger.go --fake-name Logger . logger
type logger interface {
	Warnf(template string, args ...interface{})
	Infof(template string, args ...interface{})
}

// Controller holds the storage of both virtual-services and
// destination-rules
type Controller struct {
	client        CopilotClient
	store         model.ConfigStore
	logger        logger
	timeout       time.Duration
	checkInterval time.Duration
	eventHandlers []func(model.Config, model.Event)
	storage       storage
}

// NewController provides a new CF controller
func NewController(client CopilotClient, store model.ConfigStore, logger logger, timeout, checkInterval time.Duration) *Controller {
	return &Controller{
		client:        client,
		store:         store,
		logger:        logger,
		timeout:       timeout,
		checkInterval: checkInterval,
		storage: storage{
			virtualServices:  make(map[string]*model.Config),
			destinationRules: make(map[string]*model.Config),
		},
	}
}

// RegisterEventHandler will save event handler off to controller struct
// we call handler on a time interval instead within Run
func (c *Controller) RegisterEventHandler(typ string, f func(model.Config, model.Event)) {
	c.eventHandlers = append(c.eventHandlers, f)
}

// HasSynced demonstrates whether we have route data or not - meaning
// we've successfuclly queried the copilot for data
func (c *Controller) HasSynced() bool {
	return len(c.storage.virtualServices) > 0 && len(c.storage.destinationRules) > 0
}

// Run kicks off the async copilot fetch plus running
// the cache clearing event handler that causes the rest of
// pilot to repopulate the rules to push data to envoy
func (c *Controller) Run(stop <-chan struct{}) {
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

// ConfigDescriptor supported by the CF Controller are only virtual-services
// and destination-rules
func (c *Controller) ConfigDescriptor() model.ConfigDescriptor {
	return model.ConfigDescriptor{
		model.VirtualService,
		model.DestinationRule,
	}
}

// Get has been optimized for the current cloudfoundry case of
// no namespace and only two specific types. We  have also
// left these in maps for faster lookup
func (c *Controller) Get(typ, name, namespace string) (*model.Config, bool) {
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
		c.logger.Infof("get type not supported: %s", typ)
	}

	return config, found
}

// List also ignores namespace to return all the rules of a particular type
// we've paid some attention here to avoid locking too long and also
// avoid allocation performance hits
func (c *Controller) List(typ, namespace string) ([]model.Config, error) {
	var configs []model.Config
	var index int

	c.storage.RLock()
	switch typ {
	case model.VirtualService.Type:
		configs = make([]model.Config, len(c.storage.virtualServices))
		for _, service := range c.storage.virtualServices {
			configs[index] = *service
			index++
		}

	case model.DestinationRule.Type:
		configs = make([]model.Config, len(c.storage.destinationRules))
		for _, rule := range c.storage.destinationRules {
			configs[index] = *rule
			index++
		}
	default:
		c.logger.Infof("list type not supported: %s", typ)
	}
	c.storage.RUnlock()

	return configs, nil
}

// Create is not implemented, this is handled by rebuildRules
func (c *Controller) Create(config model.Config) (string, error) {
	return "", errUnsupported
}

// Update is not implemented, copilot is the source of truth
func (c *Controller) Update(config model.Config) (string, error) {
	return "", errUnsupported
}

// Delete is not implemented, again copilot is the source of truth
func (c *Controller) Delete(typ, name, namespace string) error {
	return errUnsupported
}

func (c *Controller) rebuildRules() {
	ctx, cancel := context.WithTimeout(context.Background(), c.timeout)
	defer cancel()

	resp, err := c.client.Routes(ctx, new(copilotapi.RoutesRequest))
	if err != nil {
		c.logger.Warnf("failed to fetch routes from copilot: %s", err)
	}

	gateways, err := c.store.List(model.Gateway.Type, model.NamespaceAll)
	if err != nil {
		c.logger.Warnf("failed to list gateways: %s", err)
	}

	var gatewayNames []string
	for _, gwConfig := range gateways {
		gatewayNames = append(gatewayNames, gwConfig.Name)
	}

	virtualServices := make(map[string]*model.Config, len(resp.GetRoutes()))
	destinationRules := make(map[string]*model.Config, len(resp.GetRoutes()))

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

		r := createHTTPRoute(route)
		r.Route[0].Destination.Subset = route.GetCapiProcessGuid()

		if route.GetPath() != "" {
			r.Match = createHTTPMatchRequest(route)
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

func (c *Controller) runHandlers() {
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

func createHTTPRoute(route *copilotapi.RouteWithBackends) *networking.HTTPRoute {
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

func createHTTPMatchRequest(route *copilotapi.RouteWithBackends) []*networking.HTTPMatchRequest {
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
