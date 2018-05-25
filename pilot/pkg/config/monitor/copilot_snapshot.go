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
	"crypto/md5"
	"fmt"
	"io"
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
	store            model.ConfigStore
	client           CopilotClient
	timeout          time.Duration
	virtualServices  map[string]*model.Config
	destinationRules map[string]*model.Config
}

// NewCopilotSnapshot returns a CopilotSnapshot used for integration with Cloud Foundry.
// The store is required to discover any existing gateways: the generated config will reference those gateways
// The client is used to discover Cloud Foundry Routes from Copilot
// The snapshot will not return routes for any hostnames which end with strings on the hostnameBlacklist
func NewCopilotSnapshot(store model.ConfigStore, client CopilotClient, timeout time.Duration) *CopilotSnapshot {
	return &CopilotSnapshot{
		store:            store,
		client:           client,
		timeout:          timeout,
		virtualServices:  make(map[string]*model.Config),
		destinationRules: make(map[string]*model.Config),
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

	var gatewayNames []string
	for _, gwConfig := range gateways {
		gatewayNames = append(gatewayNames, gwConfig.Name)
	}

	for _, route := range resp.GetRoutes() {
		cfHash := hashRoute(route)

		if _, ok := c.virtualServices[cfHash]; ok {
			continue
		}

		config := &model.Config{
			ConfigMeta: model.ConfigMeta{
				Type:    model.VirtualService.Type,
				Version: model.VirtualService.Version,
				Name:    fmt.Sprintf("route-for-%s", route.GetHostname()),
			},
			Spec: &networking.VirtualService{
				Gateways: gatewayNames,
				Hosts:    []string{route.GetHostname()},
				Http: []*networking.HTTPRoute{
					{
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
					},
				},
			},
		}

		if route.GetPath() != "" {
			vs := config.Spec.(*networking.VirtualService)

			vs.Http[0].Match = []*networking.HTTPMatchRequest{
				{
					Uri: &networking.StringMatch{
						MatchType: &networking.StringMatch_Prefix{
							Prefix: route.GetPath(),
						},
					},
				},
			}
			vs.Http[0].Route[0].Destination.Subset = cfHash

			rule := &model.Config{
				ConfigMeta: model.ConfigMeta{
					Type:    model.DestinationRule.Type,
					Version: model.DestinationRule.Version,
					Name:    fmt.Sprintf("rule-for-%s", route.GetHostname()),
				},
				Spec: &networking.DestinationRule{
					Host: route.GetHostname(),
					Subsets: []*networking.Subset{
						{
							Name:   cfHash,
							Labels: map[string]string{"cf-service-instance": cfHash},
						},
					},
				},
			}

			c.destinationRules[cfHash] = rule
		}

		c.virtualServices[cfHash] = config
	}

	var cachedHashedRoutes sort.StringSlice
	for hashedRoute := range c.virtualServices {
		cachedHashedRoutes = append(cachedHashedRoutes, hashedRoute)
	}
	cachedHashedRoutes.Sort()

	var ruleHashes sort.StringSlice
	for hash := range c.destinationRules {
		ruleHashes = append(ruleHashes, hash)
	}
	ruleHashes.Sort()

	var configs []*model.Config
	vs := collectConfigs(c.virtualServices, cachedHashedRoutes, resp)
	dr := collectConfigs(c.destinationRules, ruleHashes, resp)

	configs = append(configs, vs...)
	configs = append(configs, dr...)

	return configs, nil
}

func collectConfigs(cachedConfigs map[string]*model.Config, cachedHashedRoutes sort.StringSlice, resp *copilotapi.RoutesResponse) []*model.Config {
	var configs []*model.Config

	backends := make(map[string]bool)

	for _, route := range resp.GetRoutes() {
		h := hashRoute(route)
		backends[h] = true
	}

	for _, hashedRoute := range cachedHashedRoutes {
		if _, ok := backends[hashedRoute]; !ok {
			delete(cachedConfigs, hashedRoute)
			continue
		}

		configs = append(configs, cachedConfigs[hashedRoute])
	}

	return configs
}

func hashRoute(route *copilotapi.RouteWithBackends) string {
	h := md5.New()
	io.WriteString(h, fmt.Sprintf("%s%s", route.GetHostname(), route.GetPath()))

	return fmt.Sprintf("%x", h.Sum(nil))
}
