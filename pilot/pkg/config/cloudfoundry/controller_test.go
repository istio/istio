package cloudfoundry_test

import (
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
	configDescriptor := model.ConfigDescriptor{}
	store := memory.Make(configDescriptor)

	controller := cloudfoundry.NewController(mockCopilotClient, store, 100*time.Millisecond, 100*time.Millisecond)

	var callCount int
	controller.RegisterEventHandler("virtual-service", func(model.Config, model.Event) {
		callCount++
	})

	stop := make(chan struct{})
	defer func() { stop <- struct{}{} }()

	controller.Run(stop)

	g.Eventually(func() int { return callCount }).Should(gomega.Equal(1))
}

func TestConfigDescriptor(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	mockCopilotClient := &fakes.CopilotClient{}
	configDescriptor := model.ConfigDescriptor{}
	store := memory.Make(configDescriptor)

	controller := cloudfoundry.NewController(mockCopilotClient, store, 100*time.Millisecond, 100*time.Millisecond)

	descriptors := controller.ConfigDescriptor()

	g.Expect(descriptors.Types()).To(gomega.ConsistOf([]string{model.VirtualService.Type, model.DestinationRule.Type}))
}

func TestGet(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	mockCopilotClient := &fakes.CopilotClient{}
	configDescriptor := model.ConfigDescriptor{
		model.DestinationRule,
		model.VirtualService,
		model.Gateway,
	}
	store := memory.Make(configDescriptor)

	controller := cloudfoundry.NewController(mockCopilotClient, store, 100*time.Millisecond, 100*time.Millisecond)

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

	g.Eventually(func() bool {
		var found bool
		config, found = controller.Get("destination-rule", "dest-rule-for-some-external-route.example.com", "")

		return found
	}).Should(gomega.BeTrue())

	g.Expect(config).NotTo(gomega.BeNil())
}

func TestList(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	mockCopilotClient := &fakes.CopilotClient{}
	configDescriptor := model.ConfigDescriptor{
		model.DestinationRule,
		model.VirtualService,
		model.Gateway,
	}
	store := memory.Make(configDescriptor)

	controller := cloudfoundry.NewController(mockCopilotClient, store, 100*time.Millisecond, 100*time.Millisecond)

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
		configs, err = controller.List("destination-rule", "")

		return configs, err
	}).Should(gomega.HaveLen(2))

	rule := configs[1].Spec.(*networking.DestinationRule)

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

	rule = configs[0].Spec.(*networking.DestinationRule)
	g.Expect(rule.Host).To(gomega.Equal("other.example.com"))
	g.Expect(rule.Subsets).To(gomega.HaveLen(1))
	g.Expect(rule.Subsets).To(gomega.ConsistOf([]*networking.Subset{
		{
			Name:   "some-guid-x",
			Labels: map[string]string{"cfapp": "some-guid-x"},
		},
	}))

	g.Eventually(func() ([]model.Config, error) {
		var err error
		configs, err = controller.List("virtual-service", "")

		return configs, err
	}).Should(gomega.HaveLen(2))
}

func TestCacheClear(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	mockCopilotClient := &fakes.CopilotClient{}
	configDescriptor := model.ConfigDescriptor{
		model.DestinationRule,
		model.VirtualService,
		model.Gateway,
	}
	store := memory.Make(configDescriptor)

	controller := cloudfoundry.NewController(mockCopilotClient, store, 100*time.Millisecond, 100*time.Millisecond)

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
	}).Should(gomega.HaveLen(2))

	g.Eventually(func() ([]model.Config, error) {
		var err error
		configs, err = controller.List("virtual-service", "")

		return configs, err
	}, "2s").Should(gomega.BeEmpty())
}

var routes = []*copilotapi.RouteWithBackends{
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
