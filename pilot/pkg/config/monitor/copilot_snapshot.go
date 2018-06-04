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

package monitor

import (
	"context"
	"fmt"
	"sort"
	"time"

	copilotapi "code.cloudfoundry.org/copilot/api"

	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/model"
)

// CopilotClient defines a local interface for interacting with Cloud Foundry Copilot
//go:generate counterfeiter -o fakes/copilot_client.go --fake-name CopilotClient . CopilotClient
type CopilotClient interface {
	copilotapi.IstioCopilotClient
}

// CopilotSnapshot provides a snapshot of configuration from the Cloud Foundry Copilot component.
type CopilotSnapshot struct {
	store   model.ConfigStore
	client  CopilotClient
	timeout time.Duration
}

// NewCopilotSnapshot returns a CopilotSnapshot used for integration with Cloud Foundry.
// The store is required to discover any existing gateways: the generated config will reference those gateways
// The client is used to discover Cloud Foundry Routes from Copilot
func NewCopilotSnapshot(store model.ConfigStore, client CopilotClient, timeout time.Duration) *CopilotSnapshot {
	return &CopilotSnapshot{
		store:   store,
		client:  client,
		timeout: timeout,
	}
}

// ReadConfigFiles returns a complete set of VirtualServices for all Cloud Foundry routes known to Copilot.
// It may be used for the getSnapshotFunc when constructing a NewMonitor
func (c *CopilotSnapshot) ReadConfigFiles() ([]*model.Config, error) {
	ctx, cancel := context.WithTimeout(context.Background(), c.timeout)
	defer cancel()

	resp, err := c.client.Routes(ctx, new(copilotapi.RoutesRequest))
	if err != nil {
		return nil, err
	}

	gateways, err := c.store.List(model.Gateway.Type, model.NamespaceAll)
	if err != nil {
		return nil, err
	}

	virtualServices := make(map[string]*model.Config)
	destinationRules := make(map[string]*model.Config)

	var gatewayNames []string
	for _, gwConfig := range gateways {
		gatewayNames = append(gatewayNames, gwConfig.Name)
	}

	for _, route := range resp.GetRoutes() {
		var dr *networking.DestinationRule
		if config, ok := destinationRules[route.GetHostname()]; ok {
			dr = config.Spec.(*networking.DestinationRule)
		} else {
			dr = createDestinationRule(route)
		}
		dr.Subsets = append(dr.Subsets, createSubset(route.GetCapiProcessGuid()))

		var vs *networking.VirtualService
		if config, ok := virtualServices[route.GetHostname()]; ok {
			vs = config.Spec.(*networking.VirtualService)
		} else {
			vs = createVirtualService(gatewayNames, route)
		}

		r := createRoute(route)
		r.Route[0].Destination.Subset = route.GetCapiProcessGuid()

		// TODO: Extract this sorting logic into a custom sorter
		// The route matches need to be first before the root routes;
		// they will otherwise not be enumerated past the root route
		if route.GetPath() != "" {
			r.Match = createMatchRequest(route)
			vs.Http = append([]*networking.HTTPRoute{r}, vs.Http...)
		} else {
			vs.Http = append(vs.Http, r)
		}

		virtualServices[route.GetHostname()] = &model.Config{
			ConfigMeta: model.ConfigMeta{
				Type:    model.VirtualService.Type,
				Version: model.VirtualService.Version,
				Name:    fmt.Sprintf("virtual-service-for-%s", route.GetHostname()),
			},
			Spec: vs,
		}
		destinationRules[route.GetHostname()] = &model.Config{
			ConfigMeta: model.ConfigMeta{
				Type:    model.DestinationRule.Type,
				Version: model.DestinationRule.Version,
				Name:    fmt.Sprintf("dest-rule-for-%s", route.GetHostname()),
			},
			Spec: dr,
		}
	}

	var configs []*model.Config
	for _, service := range virtualServices {
		configs = append(configs, service)
	}

	for _, rule := range destinationRules {
		configs = append(configs, rule)
	}

	sort.Slice(configs, func(i, j int) bool { return configs[i].Key() < configs[j].Key() })

	return configs, nil
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
