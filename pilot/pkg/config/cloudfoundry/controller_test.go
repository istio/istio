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

package cloudfoundry_test

import (
	"errors"
	"fmt"
	"sort"
	"sync"
	"testing"
	"time"

	copilotapi "code.cloudfoundry.org/copilot/api"

	networking "istio.io/api/networking/v1alpha3"

	"github.com/onsi/gomega"

	"istio.io/istio/pilot/pkg/config/cloudfoundry"
	"istio.io/istio/pilot/pkg/config/cloudfoundry/fakes"
	"istio.io/istio/pilot/pkg/config/memory"
	"istio.io/istio/pilot/pkg/model"
)

func TestRegisterEventHandler(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	mockCopilotClient := &fakes.CopilotClient{}
	logger := &fakes.Logger{}
	configDescriptor := model.ConfigDescriptor{}
	store := memory.Make(configDescriptor)

	controller := cloudfoundry.NewController(mockCopilotClient, store, logger, 100*time.Millisecond, 100*time.Millisecond)

	var (
		callCount int
		m         sync.Mutex
	)

	controller.RegisterEventHandler("virtual-service", func(model.Config, model.Event) {
		m.Lock()
		callCount++
		m.Unlock()
	})

	stop := make(chan struct{})
	defer func() { stop <- struct{}{} }()

	controller.Run(stop)

	g.Eventually(func() int {
		m.Lock()
		defer m.Unlock()
		return callCount
	}).Should(gomega.Equal(1))
}

func TestConfigDescriptor(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	mockCopilotClient := &fakes.CopilotClient{}
	logger := &fakes.Logger{}
	configDescriptor := model.ConfigDescriptor{}
	store := memory.Make(configDescriptor)

	controller := cloudfoundry.NewController(mockCopilotClient, store, logger, 100*time.Millisecond, 100*time.Millisecond)

	descriptors := controller.ConfigDescriptor()

	g.Expect(descriptors.Types()).To(gomega.ConsistOf([]string{model.VirtualService.Type, model.DestinationRule.Type}))
}

func TestGet(t *testing.T) {
	mockCopilotClient := &fakes.CopilotClient{}
	logger := &fakes.Logger{}
	configDescriptor := model.ConfigDescriptor{
		model.DestinationRule,
		model.VirtualService,
		model.Gateway,
	}
	store := memory.Make(configDescriptor)

	controller := cloudfoundry.NewController(mockCopilotClient, store, logger, 100*time.Millisecond, 100*time.Millisecond)

	routeResponses := []*copilotapi.RoutesResponse{{Routes: routes}}

	t.Run("invalid type", func(t *testing.T) {
		g := gomega.NewGomegaWithT(t)

		stop := make(chan struct{})
		defer func() { stop <- struct{}{} }()

		controller.Run(stop)

		g.Eventually(func() string {
			controller.Get("unknown-type", "virtual-service-for-some-external-route.example.com", "")

			if logger.InfofCallCount() == 0 {
				return ""
			}

			format, message := logger.InfofArgsForCall(0)
			return fmt.Sprintf(format, message...)
		}).Should(gomega.Equal("get type not supported: unknown-type"))
	})

	t.Run("same hostname and different paths", func(t *testing.T) {
		g := gomega.NewGomegaWithT(t)

		for idx, response := range routeResponses {
			mockCopilotClient.RoutesReturnsOnCall(idx, response, nil)
		}

		for _, gatewayConfig := range gatewayConfigs {
			if _, exists := store.Get(gatewayConfig.ConfigMeta.Type, gatewayConfig.ConfigMeta.Name, ""); !exists {
				_, err := store.Create(gatewayConfig)
				g.Expect(err).NotTo(gomega.HaveOccurred())
			}
		}

		stop := make(chan struct{})
		defer func() { stop <- struct{}{} }()

		controller.Run(stop)

		var config *model.Config
		g.Eventually(func() bool {
			var found bool
			config, found = controller.Get("virtual-service", "virtual-service-for-some-external-route.example.com", "")

			return found
		}).Should(gomega.BeTrue())

		vs := config.Spec.(*networking.VirtualService)
		g.Expect(vs.Hosts[0]).To(gomega.Equal("some-external-route.example.com"))
		g.Expect(vs.Gateways).To(gomega.ConsistOf([]string{"some-gateway", "some-other-gateway"}))

		g.Expect(vs.Http).To(gomega.HaveLen(2))
		g.Expect(vs.Http).To(gomega.Equal([]*networking.HTTPRoute{
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
						Weight: 100,
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
						Weight: 100,
					},
				},
			},
		}))

		g.Eventually(func() bool {
			var found bool
			config, found = controller.Get("destination-rule", "dest-rule-for-some-external-route.example.com", "")

			return found
		}).Should(gomega.BeTrue())

		g.Expect(config).NotTo(gomega.BeNil())
	})

	t.Run("same hostname and no prefixes", func(t *testing.T) {
		g := gomega.NewGomegaWithT(t)

		for idx, response := range routeResponses {
			mockCopilotClient.RoutesReturnsOnCall(idx, response, nil)
		}

		for _, gatewayConfig := range gatewayConfigs {
			if _, exists := store.Get(gatewayConfig.ConfigMeta.Type, gatewayConfig.ConfigMeta.Name, ""); !exists {
				_, err := store.Create(gatewayConfig)
				g.Expect(err).NotTo(gomega.HaveOccurred())
			}
		}

		stop := make(chan struct{})
		defer func() { stop <- struct{}{} }()

		controller.Run(stop)

		var config *model.Config
		g.Eventually(func() bool {
			var found bool
			config, found = controller.Get("virtual-service", "virtual-service-for-awesome-external-route.example.com", "")

			return found
		}).Should(gomega.BeTrue())

		vs := config.Spec.(*networking.VirtualService)
		g.Expect(vs.Hosts[0]).To(gomega.Equal("awesome-external-route.example.com"))
		g.Expect(vs.Gateways).To(gomega.ConsistOf([]string{"some-gateway", "some-other-gateway"}))

		g.Expect(vs.Http).To(gomega.HaveLen(1))
		g.Expect(vs.Http).To(gomega.Equal([]*networking.HTTPRoute{
			{
				Route: []*networking.DestinationWeight{
					{
						Destination: &networking.Destination{
							Host: "awesome-external-route.example.com",
							Port: &networking.PortSelector{
								Port: &networking.PortSelector_Number{
									Number: 8080,
								},
							},
							Subset: "some-guid-x",
						},
						Weight: 50,
					},
					{
						Destination: &networking.Destination{
							Host: "awesome-external-route.example.com",
							Port: &networking.PortSelector{
								Port: &networking.PortSelector_Number{
									Number: 8080,
								},
							},
							Subset: "some-guid-y",
						},
						Weight: 50,
					},
				},
			},
		}))

		g.Eventually(func() bool {
			var found bool
			config, found = controller.Get("destination-rule", "dest-rule-for-awesome-external-route.example.com", "")

			return found
		}).Should(gomega.BeTrue())

		g.Expect(config).NotTo(gomega.BeNil())
	})
}

func TestList(t *testing.T) {
	mockCopilotClient := &fakes.CopilotClient{}
	logger := &fakes.Logger{}
	configDescriptor := model.ConfigDescriptor{
		model.DestinationRule,
		model.VirtualService,
		model.Gateway,
	}
	store := memory.Make(configDescriptor)

	controller := cloudfoundry.NewController(mockCopilotClient, store, logger, 100*time.Millisecond, 100*time.Millisecond)

	routeResponses := []*copilotapi.RoutesResponse{{Routes: routes}}

	t.Run("invalid type", func(t *testing.T) {
		g := gomega.NewGomegaWithT(t)

		stop := make(chan struct{})
		defer func() { stop <- struct{}{} }()

		controller.Run(stop)

		g.Eventually(func() string {
			controller.List("unknown-type", "")

			if logger.InfofCallCount() == 0 {
				return ""
			}

			format, message := logger.InfofArgsForCall(0)
			return fmt.Sprintf(format, message...)
		}).Should(gomega.Equal("list type not supported: unknown-type"))
	})

	t.Run("valid type", func(t *testing.T) {
		g := gomega.NewGomegaWithT(t)

		for idx, response := range routeResponses {
			mockCopilotClient.RoutesReturnsOnCall(idx, response, nil)
		}

		for _, gatewayConfig := range gatewayConfigs {
			_, err := store.Create(gatewayConfig)
			g.Expect(err).NotTo(gomega.HaveOccurred())
		}

		stop := make(chan struct{})
		defer func() { stop <- struct{}{} }()

		controller.Run(stop)

		var configs []model.Config
		g.Eventually(func() ([]model.Config, error) {
			var err error
			configs, err = controller.List("destination-rule", "")

			return configs, err
		}).Should(gomega.HaveLen(3))

		sort.Slice(configs, func(i, j int) bool { return configs[i].Key() < configs[j].Key() })
		rule := configs[2].Spec.(*networking.DestinationRule)

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

		rule = configs[1].Spec.(*networking.DestinationRule)
		g.Expect(rule.Host).To(gomega.Equal("other.example.com"))
		g.Expect(rule.Subsets).To(gomega.HaveLen(1))
		g.Expect(rule.Subsets).To(gomega.ConsistOf([]*networking.Subset{
			{
				Name:   "some-guid-x",
				Labels: map[string]string{"cfapp": "some-guid-x"},
			},
		}))

		rule = configs[0].Spec.(*networking.DestinationRule)
		g.Expect(rule.Host).To(gomega.Equal("awesome-external-route.example.com"))
		g.Expect(rule.Subsets).To(gomega.HaveLen(2))
		g.Expect(rule.Subsets).To(gomega.ConsistOf([]*networking.Subset{
			{
				Name:   "some-guid-x",
				Labels: map[string]string{"cfapp": "some-guid-x"},
			},
			{
				Name:   "some-guid-y",
				Labels: map[string]string{"cfapp": "some-guid-y"},
			},
		}))

		g.Eventually(func() ([]model.Config, error) {
			var err error
			configs, err = controller.List("virtual-service", "")

			return configs, err
		}).Should(gomega.HaveLen(3))
	})
}

func TestCacheClear(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	mockCopilotClient := &fakes.CopilotClient{}
	logger := &fakes.Logger{}
	configDescriptor := model.ConfigDescriptor{
		model.DestinationRule,
		model.VirtualService,
		model.Gateway,
	}
	store := memory.Make(configDescriptor)

	controller := cloudfoundry.NewController(mockCopilotClient, store, logger, 100*time.Millisecond, 100*time.Millisecond)

	routeResponses := []*copilotapi.RoutesResponse{{Routes: routes}}

	for idx, response := range routeResponses {
		mockCopilotClient.RoutesReturnsOnCall(idx, response, nil)
	}

	for _, gatewayConfig := range gatewayConfigs {
		_, err := store.Create(gatewayConfig)
		g.Expect(err).NotTo(gomega.HaveOccurred())
	}

	stop := make(chan struct{})
	defer func() { stop <- struct{}{} }()

	controller.Run(stop)

	var configs []model.Config
	g.Eventually(func() ([]model.Config, error) {
		var err error
		configs, err = controller.List("virtual-service", "")

		return configs, err
	}).Should(gomega.HaveLen(3))

	g.Eventually(func() ([]model.Config, error) {
		var err error
		configs, err = controller.List("virtual-service", "")

		return configs, err
	}, "2s").Should(gomega.BeEmpty())
}

func TestStoreFailure(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	mockCopilotClient := &fakes.CopilotClient{}
	logger := &fakes.Logger{}
	store := &fakes.Store{}

	controller := cloudfoundry.NewController(mockCopilotClient, store, logger, 100*time.Millisecond, 100*time.Millisecond)

	stop := make(chan struct{})
	defer func() { stop <- struct{}{} }()

	store.ListReturns([]model.Config{}, errors.New("store failed"))
	controller.Run(stop)

	g.Eventually(func() string {
		if logger.WarnfCallCount() == 0 {
			return ""
		}

		format, message := logger.WarnfArgsForCall(0)
		return fmt.Sprintf(format, message...)
	}).Should(gomega.Equal("failed to list gateways: store failed"))
}

func TestCopilotFailure(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	mockCopilotClient := &fakes.CopilotClient{}
	logger := &fakes.Logger{}
	configDescriptor := model.ConfigDescriptor{
		model.DestinationRule,
		model.VirtualService,
		model.Gateway,
	}
	store := memory.Make(configDescriptor)

	controller := cloudfoundry.NewController(mockCopilotClient, store, logger, 100*time.Millisecond, 100*time.Millisecond)

	stop := make(chan struct{})
	defer func() { stop <- struct{}{} }()

	mockCopilotClient.RoutesReturns(&copilotapi.RoutesResponse{}, errors.New("copilot failed"))
	controller.Run(stop)

	g.Eventually(func() string {
		if logger.WarnfCallCount() == 0 {
			return ""
		}

		format, message := logger.WarnfArgsForCall(0)
		return fmt.Sprintf(format, message...)
	}).Should(gomega.Equal("failed to fetch routes from copilot: copilot failed"))
}

var routes = []*copilotapi.RouteWithBackends{
	{
		Hostname:        "some-external-route.example.com",
		Backends:        nil,
		CapiProcessGuid: "some-guid-z",
		RouteWeight:     100,
	},
	{
		Hostname:        "some-external-route.example.com",
		Path:            "/some/path",
		Backends:        nil,
		CapiProcessGuid: "some-guid-a",
		RouteWeight:     100,
	},
	{
		Hostname:        "other.example.com",
		Backends:        nil,
		CapiProcessGuid: "some-guid-x",
		RouteWeight:     100,
	},
	{
		Hostname:        "awesome-external-route.example.com",
		Backends:        nil,
		CapiProcessGuid: "some-guid-x",
		RouteWeight:     50,
	},
	{
		Hostname:        "awesome-external-route.example.com",
		Backends:        nil,
		CapiProcessGuid: "some-guid-y",
		RouteWeight:     50,
	},
}

var gatewayConfigs = []model.Config{
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
