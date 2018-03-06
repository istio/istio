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

package monitor_test

import (
	"errors"
	"testing"

	"github.com/onsi/gomega"

	copilotapi "code.cloudfoundry.org/copilot/api"

	v2routing "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/config/memory"
	"istio.io/istio/pilot/pkg/config/monitor"
	"istio.io/istio/pilot/pkg/config/monitor/fakes"
	"istio.io/istio/pilot/pkg/model"
)

func TestCloudFoundrySnapshot(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	mockCopilotClient := &fakes.CopilotClient{}

	mockCopilotClient.RoutesReturns(&copilotapi.RoutesResponse{
		Backends: map[string]*copilotapi.BackendSet{
			"some-external-route.example.com":       nil,
			"some-other-external-route.example.com": nil,
			"some-internal-route.apps.internal":     nil,
		},
	}, nil)

	configDescriptor := model.ConfigDescriptor{
		model.VirtualService,
		model.Gateway,
	}
	store := memory.Make(configDescriptor)
	copilotSnapshot := monitor.NewCopilotSnapshot(store, mockCopilotClient, []string{".internal"})

	gatewayConfigs := []model.Config{
		{
			ConfigMeta: model.ConfigMeta{
				Name: "some-gateway",
				Type: "gateway",
			},
			Spec: &v2routing.Gateway{
				Servers: []*v2routing.Server{
					{
						Port: &v2routing.Port{
							Number:   80,
							Protocol: "HTTP",
						},
						Hosts: []string{"*.example.com"},
					},
				},
			},
		},
		{
			ConfigMeta: model.ConfigMeta{
				Name: "some-other-gateway",
				Type: "gateway",
			},
			Spec: &v2routing.Gateway{
				Servers: []*v2routing.Server{
					{
						Port: &v2routing.Port{
							Number:   80,
							Protocol: "HTTP",
						},
						Hosts: []string{"*"},
					},
				},
			},
		},
	}

	for _, gatewayConfig := range gatewayConfigs {
		_, err := store.Create(gatewayConfig)
		g.Expect(err).NotTo(gomega.HaveOccurred())
	}

	virtualServices, _ := copilotSnapshot.ReadConfigFiles()
	g.Expect(virtualServices).To(gomega.HaveLen(2))

	for _, untypedConfig := range virtualServices {
		c := untypedConfig.Spec.(*v2routing.VirtualService)

		g.Expect(c.Hosts).To(gomega.HaveLen(1))
		matchHostname := c.Hosts[0]

		g.Expect(c.Gateways).To(gomega.ConsistOf([]string{"some-gateway", "some-other-gateway"}))

		g.Expect(c.Http).To(gomega.Equal([]*v2routing.HTTPRoute{
			{
				Route: []*v2routing.DestinationWeight{
					{
						Destination: &v2routing.Destination{
							Name: matchHostname,
						},
					},
				},
			},
		}))
	}
}

func TestCloudFoundrySnapshotConnectionError(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	mockCopilotClient := &fakes.CopilotClient{}
	mockCopilotClient.RoutesReturns(&copilotapi.RoutesResponse{}, errors.New("Copilot Connection Error"))

	configDescriptor := model.ConfigDescriptor{}
	store := memory.Make(configDescriptor)
	copilotSnapshot := monitor.NewCopilotSnapshot(store, mockCopilotClient, []string{".internal"})

	virtualServices, err := copilotSnapshot.ReadConfigFiles()
	g.Expect(err).To(gomega.HaveOccurred())
	g.Expect(virtualServices).To(gomega.BeNil())
}
