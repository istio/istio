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

	"github.com/onsi/gomega"
	"google.golang.org/grpc"

	copilotapi "code.cloudfoundry.org/copilot/api"

	networking "istio.io/api/networking/v1alpha3"
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
	timeout := 20 * time.Millisecond
	copilotSnapshot := monitor.NewCopilotSnapshot(store, mockCopilotClient, []string{".internal"}, timeout)

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
		g.Expect(err).NotTo(gomega.HaveOccurred())
	}

	virtualServices, _ := copilotSnapshot.ReadConfigFiles()
	g.Expect(virtualServices).To(gomega.HaveLen(2))

	for _, untypedConfig := range virtualServices {
		c := untypedConfig.Spec.(*networking.VirtualService)

		g.Expect(c.Hosts).To(gomega.HaveLen(1))
		matchHostname := c.Hosts[0]

		g.Expect(c.Gateways).To(gomega.ConsistOf([]string{"some-gateway", "some-other-gateway"}))

		g.Expect(c.Http).To(gomega.Equal([]*networking.HTTPRoute{
			{
				Route: []*networking.DestinationWeight{
					{
						Destination: &networking.Destination{
							Name: matchHostname,
						},
					},
				},
			},
		}))
	}
}

func TestCloudFoundrySnapshotVirtualServiceCache(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	mockCopilotClient := &fakes.CopilotClient{}

	mockCopilotClient.RoutesReturnsOnCall(0, &copilotapi.RoutesResponse{
		Backends: map[string]*copilotapi.BackendSet{
			"some-external-route.example.com":                 nil,
			"some-other-external-route-to-delete.example.com": nil,
		},
	}, nil)

	mockCopilotClient.RoutesReturnsOnCall(1, &copilotapi.RoutesResponse{
		Backends: map[string]*copilotapi.BackendSet{
			"some-external-route.example.com": nil,
		},
	}, nil)

	configDescriptor := model.ConfigDescriptor{
		model.VirtualService,
		model.Gateway,
	}
	store := memory.Make(configDescriptor)
	timeout := 20 * time.Millisecond
	copilotSnapshot := monitor.NewCopilotSnapshot(store, mockCopilotClient, []string{".internal"}, timeout)

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
	}

	for _, gatewayConfig := range gatewayConfigs {
		_, err := store.Create(gatewayConfig)
		g.Expect(err).NotTo(gomega.HaveOccurred())
	}

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
						Name: "some-external-route.example.com",
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
	copilotSnapshot := monitor.NewCopilotSnapshot(store, mockCopilotClient, []string{".internal"}, timeout)

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
	copilotSnapshot := monitor.NewCopilotSnapshot(store, mockCopilotClient, []string{".internal"}, timeout)

	virtualServices, err := copilotSnapshot.ReadConfigFiles()
	g.Expect(err).To(gomega.BeAssignableToTypeOf(context.DeadlineExceeded))
	g.Expect(virtualServices).To(gomega.BeNil())
}
