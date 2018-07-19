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
	"context"
	"errors"
	"testing"
	"time"

	copilotapi "code.cloudfoundry.org/copilot/api"
	"github.com/onsi/gomega"
	"google.golang.org/grpc"

	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/config/memory"
	"istio.io/istio/pilot/pkg/config/monitor"
	"istio.io/istio/pilot/pkg/config/monitor/fakes"
	"istio.io/istio/pilot/pkg/model"
)

func TestCloudFoundrySnapshot(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	routes := []*copilotapi.RouteWithBackends{
		{
			Hostname:        "some-external-route.example.com",
			Backends:        nil,
			CapiProcessGuid: "some-guid-z",
		},
		{
			Hostname:        "some-external-route.example.com",
			Path:            "/some/path",
			Backends:        nil,
			CapiProcessGuid: "some-guid-a",
		},
		{
			Hostname:        "other.example.com",
			Backends:        nil,
			CapiProcessGuid: "some-guid-x",
		},
	}

	routesResponses := []*copilotapi.RoutesResponse{{Routes: routes}}

	copilotSnapshot, err := bootstrap(routesResponses)
	g.Expect(err).ToNot(gomega.HaveOccurred())

	configs, _ := copilotSnapshot.ReadConfigFiles()
	g.Expect(configs).To(gomega.HaveLen(4))

	splitConfigs := split(configs)
	var virtualServices = splitConfigs.virtualServices
	var destinationRules = splitConfigs.destinationRules

	virtualService := virtualServices[1]
	g.Expect(virtualService.Hosts[0]).To(gomega.Equal("some-external-route.example.com"))
	g.Expect(virtualService.Gateways).To(gomega.ConsistOf([]string{"some-gateway", "some-other-gateway"}))

	g.Expect(virtualService.Http).To(gomega.HaveLen(2))
	g.Expect(virtualService.Http).To(gomega.Equal([]*networking.HTTPRoute{
		{
			Match: []*networking.HTTPMatchRequest{
				{
					Uri: &networking.StringMatch{
						MatchType: &networking.StringMatch_Prefix{
							Prefix: "/some/path",
						},
					},
				},
			},
			Route: []*networking.DestinationWeight{
				{
					Destination: &networking.Destination{
						Host: "some-external-route.example.com",
						Port: &networking.PortSelector{
							Port: &networking.PortSelector_Number{
								Number: 8080,
							},
						},
						Subset: "some-guid-a",
					},
				},
			},
		},
		{
			Route: []*networking.DestinationWeight{
				{
					Destination: &networking.Destination{
						Host: "some-external-route.example.com",
						Port: &networking.PortSelector{
							Port: &networking.PortSelector_Number{
								Number: 8080,
							},
						},
						Subset: "some-guid-z",
					},
				},
			},
		},
	}))

	g.Expect(destinationRules).To(gomega.HaveLen(2))

	rule := destinationRules[1]

	g.Expect(rule.Host).To(gomega.Equal("some-external-route.example.com"))
	g.Expect(rule.Subsets).To(gomega.HaveLen(2))
	g.Expect(rule.Subsets).To(gomega.ConsistOf([]*networking.Subset{
		{
			Name:   "some-guid-a",
			Labels: map[string]string{"cfapp": "some-guid-a"},
		},
		{
			Name:   "some-guid-z",
			Labels: map[string]string{"cfapp": "some-guid-z"},
		},
	}))
}

func TestCloudFoundrySnapshotConnectionError(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	mockCopilotClient := &fakes.CopilotClient{}
	mockCopilotClient.RoutesReturns(&copilotapi.RoutesResponse{}, errors.New("Copilot Connection Error"))

	configDescriptor := model.ConfigDescriptor{}
	store := memory.Make(configDescriptor)
	timeout := 4 * time.Millisecond
	copilotSnapshot := monitor.NewCopilotSnapshot(store, mockCopilotClient, timeout)

	virtualServices, err := copilotSnapshot.ReadConfigFiles()
	g.Expect(err).To(gomega.HaveOccurred())
	g.Expect(virtualServices).To(gomega.BeNil())
}

func TestCloudFoundrySnapshotTimeoutError(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	mockCopilotClient := &fakes.CopilotClient{}
	mockCopilotClient.RoutesStub = func(ctx context.Context, in *copilotapi.RoutesRequest, opts ...grpc.CallOption) (*copilotapi.RoutesResponse, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	}

	configDescriptor := model.ConfigDescriptor{}
	store := memory.Make(configDescriptor)
	timeout := 1 * time.Millisecond
	copilotSnapshot := monitor.NewCopilotSnapshot(store, mockCopilotClient, timeout)

	virtualServices, err := copilotSnapshot.ReadConfigFiles()
	g.Expect(err).To(gomega.BeAssignableToTypeOf(context.DeadlineExceeded))
	g.Expect(virtualServices).To(gomega.BeNil())
}

func bootstrap(routeResponses []*copilotapi.RoutesResponse) (*monitor.CopilotSnapshot, error) {
	mockCopilotClient := &fakes.CopilotClient{}

	for idx, response := range routeResponses {
		mockCopilotClient.RoutesReturnsOnCall(idx, response, nil)
	}

	configDescriptor := model.ConfigDescriptor{
		model.DestinationRule,
		model.Gateway,
	}
	store := memory.Make(configDescriptor)
	timeout := 20 * time.Millisecond
	copilotSnapshot := monitor.NewCopilotSnapshot(store, mockCopilotClient, timeout)

	gatewayConfigs := []model.Config{
		{
			ConfigMeta: model.ConfigMeta{
				Name: "some-gateway",
				Type: "gateway",
			},
			Spec: &networking.Gateway{
				Servers: []*networking.Server{
					{
						Port: &networking.Port{
							Number:   80,
							Name:     "http",
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
			Spec: &networking.Gateway{
				Servers: []*networking.Server{
					{
						Port: &networking.Port{
							Number:   80,
							Name:     "http",
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
		if err != nil {
			return nil, err
		}
	}

	return copilotSnapshot, nil
}

type splitConfigs struct {
	virtualServices  []*networking.VirtualService
	destinationRules []*networking.DestinationRule
}

func split(configs []*model.Config) splitConfigs {
	var sc splitConfigs
	for _, untypedConfig := range configs {
		if virtualService, ok := untypedConfig.Spec.(*networking.VirtualService); ok {
			sc.virtualServices = append(sc.virtualServices, virtualService)
		} else {
			sc.destinationRules = append(sc.destinationRules, untypedConfig.Spec.(*networking.DestinationRule))
		}
	}
	return sc
}
