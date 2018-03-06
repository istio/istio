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
	"strings"

	copilotapi "code.cloudfoundry.org/copilot/api"

	v2routing "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/log"
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
}

// NewCopilotSnapshot returns a CopilotSnapshot used for integration with Cloud Foundry.
// The store is required to discover any existing gateways: the generated config will reference those gateways
// The client is used to discover Cloud Foundry Routes from Copilot
// The snapshot will not return routes for any hostnames which end with strings on the hostnameBlacklist
func NewCopilotSnapshot(store model.ConfigStore, client CopilotClient, hostnameBlacklist []string) *CopilotSnapshot {
	return &CopilotSnapshot{
		store:             store,
		client:            client,
		hostnameBlacklist: hostnameBlacklist,
	}
}

// ReadConfigFiles returns a complete set of VirtualServices for all Cloud Foundry routes known to Copilot.
// It may be used for the getSnapshotFunc when constructing a NewMonitor
func (c *CopilotSnapshot) ReadConfigFiles() ([]*model.Config, error) {
	resp, err := c.client.Routes(context.Background(), new(copilotapi.RoutesRequest))
	if err != nil {
		log.Warnf("Error connecting to copilot: %v", err)
		return nil, err
	}

	gateways, err := c.store.List(model.Gateway.Type, model.NamespaceAll)
	if err != nil {
		log.Warnf("Error connecting to data store: %v", err)
		return nil, err
	}

	var gatewayNames []string
	for _, gwConfig := range gateways {
		gatewayNames = append(gatewayNames, gwConfig.Name)
	}

	var virtualServices []*model.Config
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

		config := &model.Config{
			ConfigMeta: model.ConfigMeta{
				Type:    model.VirtualService.Type,
				Version: model.VirtualService.Version,
				Name:    fmt.Sprintf("route-for-%s", hostname),
			},
			Spec: &v2routing.VirtualService{
				Gateways: gatewayNames,
				Hosts:    []string{hostname},
				Http: []*v2routing.HTTPRoute{
					{
						Route: []*v2routing.DestinationWeight{
							{
								Destination: &v2routing.Destination{
									Name: hostname,
								},
							},
						},
					},
				},
			},
		}

		virtualServices = append(virtualServices, config)
	}
	return virtualServices, nil
}
