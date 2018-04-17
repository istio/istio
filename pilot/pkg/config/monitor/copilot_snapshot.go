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
	"strings"
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
	store             model.ConfigStore
	client            CopilotClient
	hostnameBlacklist []string
	timeout           time.Duration
	virtualServices   map[string]*model.Config
}

// NewCopilotSnapshot returns a CopilotSnapshot used for integration with Cloud Foundry.
// The store is required to discover any existing gateways: the generated config will reference those gateways
// The client is used to discover Cloud Foundry Routes from Copilot
// The snapshot will not return routes for any hostnames which end with strings on the hostnameBlacklist
func NewCopilotSnapshot(store model.ConfigStore, client CopilotClient, hostnameBlacklist []string, timeout time.Duration) *CopilotSnapshot {
	return &CopilotSnapshot{
		store:             store,
		client:            client,
		hostnameBlacklist: hostnameBlacklist,
		timeout:           timeout,
		virtualServices:   make(map[string]*model.Config),
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

	filteredCopilotHostnames := c.removeBlacklistedHostnames(resp)

	for _, hostname := range filteredCopilotHostnames {
		if _, ok := c.virtualServices[hostname]; ok {
			continue
		}

		c.virtualServices[hostname] = &model.Config{
			ConfigMeta: model.ConfigMeta{
				Type:    model.VirtualService.Type,
				Version: model.VirtualService.Version,
				Name:    fmt.Sprintf("route-for-%s", hostname),
			},
			Spec: &networking.VirtualService{
				Gateways: gatewayNames,
				Hosts:    []string{hostname},
				Http: []*networking.HTTPRoute{
					{
						Route: []*networking.DestinationWeight{
							{
								Destination: &networking.Destination{
									Name: hostname,
								},
							},
						},
					},
				},
			},
		}
	}

	var cachedHostnames sort.StringSlice
	for hostname := range c.virtualServices {
		cachedHostnames = append(cachedHostnames, hostname)
	}

	cachedHostnames.Sort()

	return c.collectVirtualServices(cachedHostnames, resp), nil
}

func (c *CopilotSnapshot) removeBlacklistedHostnames(resp *copilotapi.RoutesResponse) []string {
	var filteredHostnames []string
	for hostname := range resp.Backends {
		var blacklisted bool

		for _, blacklistedItem := range c.hostnameBlacklist {
			if strings.HasSuffix(hostname, blacklistedItem) {
				blacklisted = true
				break
			}
		}

		if blacklisted {
			continue
		}

		filteredHostnames = append(filteredHostnames, hostname)
	}

	return filteredHostnames
}

func (c *CopilotSnapshot) collectVirtualServices(cachedHostnames sort.StringSlice, resp *copilotapi.RoutesResponse) []*model.Config {
	var virtualServices []*model.Config
	for _, hostname := range cachedHostnames {
		if _, ok := resp.Backends[hostname]; !ok {
			delete(c.virtualServices, hostname)
			continue
		}

		virtualServices = append(virtualServices, c.virtualServices[hostname])
	}

	return virtualServices
}
