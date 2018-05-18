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

const (
	routeHashOne = "13c6cf41252f79cff042e4cf812f055c"
	routeHashTwo = "52ef1929b781773628c530351c90b608"
)

func TestCloudFoundrySnapshot(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	routes := []*copilotapi.RouteWithBackends{
		{
			Hostname: "some-external-route.example.com",
			Path:     "/some/path",
			Backends: nil,
		},
		{
			Hostname: "some-external-route.example.com",
			Path:     "/other/path",
			Backends: nil,
		},
	}

	routesResponses := []*copilotapi.RoutesResponse{{Routes: routes}}

	copilotSnapshot, err := bootstrap(routesResponses)
	g.Expect(err).ToNot(gomega.HaveOccurred())

	configs, _ := copilotSnapshot.ReadConfigFiles()
	g.Expect(configs).To(gomega.HaveLen(4))

	var virtualServices []*networking.VirtualService
	var destinationRules []*networking.DestinationRule
	for _, untypedConfig := range configs {
		if virtualService, ok := untypedConfig.Spec.(*networking.VirtualService); ok {
			virtualServices = append(virtualServices, virtualService)
		} else {
			destinationRules = append(destinationRules, untypedConfig.Spec.(*networking.DestinationRule))
		}
	}

	for _, virtualService := range virtualServices {
		g.Expect(virtualService.Hosts).To(gomega.HaveLen(1))
		matchHostname := virtualService.Hosts[0]

		g.Expect(virtualService.Gateways).To(gomega.ConsistOf([]string{"some-gateway", "some-other-gateway"}))

		g.Expect(virtualService.Http).To(gomega.HaveLen(1))
		g.Expect(virtualService.Http[0].Match).To(gomega.HaveLen(1))

		g.Expect(virtualService.Http[0].Match[0].Uri.GetPrefix()).To(gomega.SatisfyAny(
			gomega.Equal("/some/path"),
			gomega.Equal("/other/path"),
		))
		g.Expect(virtualService.Http[0].Route).To(gomega.HaveLen(1))
		destination := virtualService.Http[0].Route[0].Destination
		g.Expect(destination.Host).To(gomega.Equal(matchHostname))
		g.Expect(destination.Port).To(gomega.Equal(&networking.PortSelector{
			Port: &networking.PortSelector_Number{
				Number: 8080,
			},
		}))
		g.Expect(destination.Subset).To(gomega.SatisfyAny(
			gomega.Equal(routeHashOne),
			gomega.Equal(routeHashTwo),
		))
	}

	g.Expect(destinationRules).To(gomega.HaveLen(2))

	for _, rule := range destinationRules {
		g.Expect(rule.Host).To(gomega.Equal("some-external-route.example.com"))
		g.Expect(rule.Subsets).To(gomega.HaveLen(1))
		g.Expect(rule.Subsets[0].Name).To(gomega.SatisfyAny(
			gomega.Equal(routeHashOne),
			gomega.Equal(routeHashTwo),
		))
		g.Expect(rule.Subsets[0].Labels).To(gomega.SatisfyAny(
			gomega.Equal(map[string]string{"cf-service-instance": routeHashOne}),
			gomega.Equal(map[string]string{"cf-service-instance": routeHashTwo}),
		))
	}
}

func TestCloudFoundrySnapshotDestinationRuleCache(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	routesResponses := []*copilotapi.RoutesResponse{
		{
			Routes: []*copilotapi.RouteWithBackends{
				{
					Hostname: "some-external-route.example.com",
					Path:     "/some/path",
					Backends: nil,
				},
				{
					Hostname: "some-other-external-route.example.com",
					Path:     "/other/path",
					Backends: nil,
				},
			},
		},
		{
			Routes: []*copilotapi.RouteWithBackends{
				{
					Hostname: "some-external-route.example.com",
					Path:     "/some/path",
					Backends: nil,
				},
			},
		},
	}
	copilotSnapshot, err := bootstrap(routesResponses)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	configs, _ := copilotSnapshot.ReadConfigFiles()
	g.Expect(configs).To(gomega.HaveLen(4))

	configs, _ = copilotSnapshot.ReadConfigFiles()
	g.Expect(configs).To(gomega.HaveLen(2))

	for _, config := range configs {
		if destinationRule, ok := config.Spec.(*networking.DestinationRule); ok {
			g.Expect(destinationRule.GetHost()).To(gomega.Equal("some-external-route.example.com"))
			g.Expect(destinationRule.GetSubsets()[0].Labels).To(
				gomega.Equal(map[string]string{"cf-service-instance": routeHashOne}))
		}
	}
}

func TestCloudFoundrySnapshotVirtualServiceCache(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	routesResponses := []*copilotapi.RoutesResponse{
		{
			Routes: []*copilotapi.RouteWithBackends{
				{
					Hostname: "some-external-route.example.com",
					Backends: nil,
				},
				{
					Hostname: "some-other-external-route.example.com",
					Backends: nil,
				},
			},
		},
		{
			Routes: []*copilotapi.RouteWithBackends{
				{
					Hostname: "some-external-route.example.com",
					Backends: nil,
				},
			},
		},
	}

	copilotSnapshot, err := bootstrap(routesResponses)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	virtualServices, _ := copilotSnapshot.ReadConfigFiles()
	g.Expect(virtualServices).To(gomega.HaveLen(2))

	virtualServices, _ = copilotSnapshot.ReadConfigFiles()
	g.Expect(virtualServices).To(gomega.HaveLen(1))

	c := virtualServices[0].Spec.(*networking.VirtualService)
	g.Expect(c.Http).To(gomega.Equal([]*networking.HTTPRoute{
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
					},
				},
			},
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
