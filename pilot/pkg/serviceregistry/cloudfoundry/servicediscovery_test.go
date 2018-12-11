// Copyright 2017 Istio Authors
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
	"testing"

	"code.cloudfoundry.org/copilot/api"
	"github.com/onsi/gomega"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/serviceregistry/cloudfoundry"
	"istio.io/istio/pilot/pkg/serviceregistry/cloudfoundry/fakes"
)

const defaultServicePort = 8080

var routesResponse = &api.RoutesResponse{
	Routes: []*api.RouteWithBackends{
		{
			Hostname: "process-guid-a.cfapps.io",
			Path:     "/other/path",
			Backends: &api.BackendSet{
				Backends: []*api.Backend{
					{
						Address: "10.10.1.5",
						Port:    61005,
					},
				},
			},
			CapiProcessGuid: "some-guid-a",
		},
		{
			Hostname: "process-guid-a.cfapps.io",
			Path:     "/another/path",
			Backends: &api.BackendSet{
				Backends: []*api.Backend{
					{
						Address: "10.10.1.2",
						Port:    61006,
					},
				},
			},
			CapiProcessGuid: "some-guid-x",
		},
		{
			Hostname: "process-guid-b.cfapps.io",
			Backends: &api.BackendSet{
				Backends: []*api.Backend{
					{
						Address: "10.0.50.4",
						Port:    61009,
					},
					{
						Address: "10.0.60.2",
						Port:    61001,
					},
				},
			},
			CapiProcessGuid: "some-guid-b",
		},
		{
			Hostname: "process-guid-z.cfapps.io",
			Backends: &api.BackendSet{
				Backends: []*api.Backend{
					{
						Address: "10.0.40.2",
						Port:    61008,
					},
				},
			},
			CapiProcessGuid: "some-guid-z",
		},
	},
}

var internalRoutesResponse = &api.InternalRoutesResponse{
	InternalRoutes: []*api.InternalRouteWithBackends{
		{
			Hostname: "something.apps.internal",
			Vip:      "127.1.1.1",
			Backends: &api.BackendSet{
				Backends: []*api.Backend{
					{
						Address: "10.255.30.1",
						Port:    6868,
					},
				},
			},
		},
	},
}

type sdTestState struct {
	mockClient       *fakes.CopilotClient
	serviceDiscovery *cloudfoundry.ServiceDiscovery
}

func newSDTestState() *sdTestState {
	mockClient := &fakes.CopilotClient{}

	// initialize object under test
	serviceDiscovery := &cloudfoundry.ServiceDiscovery{
		Client:      mockClient,
		ServicePort: defaultServicePort,
	}

	return &sdTestState{
		mockClient:       mockClient,
		serviceDiscovery: serviceDiscovery,
	}
}

func TestServiceDiscovery_Services(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	state := newSDTestState()

	state.mockClient.RoutesReturns(routesResponse, nil)
	state.mockClient.InternalRoutesReturns(internalRoutesResponse, nil)

	// function under test
	serviceModels, err := state.serviceDiscovery.Services()
	g.Expect(err).To(gomega.BeNil())

	// it returns an Istio service for each Diego process
	g.Expect(serviceModels).To(gomega.HaveLen(5))
	g.Expect(serviceModels).To(gomega.ConsistOf([]*model.Service{
		{
			Hostname:   "process-guid-a.cfapps.io",
			Ports:      []*model.Port{{Port: defaultServicePort, Protocol: model.ProtocolHTTP, Name: "http"}},
			Attributes: model.ServiceAttributes{Name: "process-guid-a.cfapps.io", Namespace: "default"},
		},
		{
			Hostname:   "process-guid-a.cfapps.io",
			Ports:      []*model.Port{{Port: defaultServicePort, Protocol: model.ProtocolHTTP, Name: "http"}},
			Attributes: model.ServiceAttributes{Name: "process-guid-a.cfapps.io", Namespace: "default"},
		},
		{
			Hostname:   "process-guid-b.cfapps.io",
			Ports:      []*model.Port{{Port: defaultServicePort, Protocol: model.ProtocolHTTP, Name: "http"}},
			Attributes: model.ServiceAttributes{Name: "process-guid-b.cfapps.io", Namespace: "default"},
		},
		{
			Hostname:   "process-guid-z.cfapps.io",
			Ports:      []*model.Port{{Port: defaultServicePort, Protocol: model.ProtocolHTTP, Name: "http"}},
			Attributes: model.ServiceAttributes{Name: "process-guid-z.cfapps.io", Namespace: "default"},
		},
		{
			Hostname:   "something.apps.internal",
			Address:    "127.1.1.1",
			Ports:      []*model.Port{{Port: defaultServicePort, Protocol: model.ProtocolTCP, Name: "tcp"}},
			Attributes: model.ServiceAttributes{Name: "something.apps.internal", Namespace: "default"},
		},
	}))
}

func TestServiceDiscovery_ServicesErrorHandling(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	state := newSDTestState()

	state.mockClient.RoutesReturns(nil, errors.New("banana"))

	_, err := state.serviceDiscovery.Services()
	g.Expect(err).To(gomega.MatchError("getting services: banana"))
}

func TestServiceDiscovery_GetService_Success(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	state := newSDTestState()

	state.mockClient.RoutesReturns(routesResponse, nil)

	serviceModel, err := state.serviceDiscovery.GetService("process-guid-b.cfapps.io")

	g.Expect(err).To(gomega.BeNil())
	g.Expect(serviceModel).To(gomega.Equal(&model.Service{
		Hostname:   "process-guid-b.cfapps.io",
		Ports:      []*model.Port{{Port: defaultServicePort, Protocol: model.ProtocolHTTP, Name: "http"}},
		Attributes: model.ServiceAttributes{Name: "process-guid-b.cfapps.io", Namespace: "default"},
	}))
}

func TestServiceDiscovery_GetService_NotFound(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	state := newSDTestState()

	state.mockClient.RoutesReturns(routesResponse, nil)

	serviceModel, err := state.serviceDiscovery.GetService("does-not-exist.cfapps.io")

	g.Expect(err).To(gomega.BeNil())
	g.Expect(serviceModel).To(gomega.BeNil())
}

func TestServiceDiscovery_GetService_ClientError(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	state := newSDTestState()

	state.mockClient.RoutesReturns(nil, errors.New("potato"))

	serviceModel, err := state.serviceDiscovery.GetService("process-guid-b.cfapps.io")

	g.Expect(err).To(gomega.MatchError("getting services: potato"))
	g.Expect(serviceModel).To(gomega.BeNil())
}

func TestServiceDiscovery_Internal_Instances_Filtering_By_Hostname(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	state := newSDTestState()

	state.mockClient.InternalRoutesReturns(internalRoutesResponse, nil)

	instances, err := state.serviceDiscovery.InstancesByPort("something.apps.internal", 0, nil)
	g.Expect(err).To(gomega.BeNil())

	g.Expect(instances).To(gomega.ConsistOf([]*model.ServiceInstance{
		{
			Endpoint: model.NetworkEndpoint{
				Address: "10.255.30.1",
				Port:    6868,
				ServicePort: &model.Port{
					Port:     defaultServicePort,
					Protocol: model.ProtocolTCP,
					Name:     "tcp",
				},
			},
			Service: &model.Service{
				Hostname: "something.apps.internal",
				Address:  "127.1.1.1",
				Ports: []*model.Port{
					{
						Port:     defaultServicePort,
						Protocol: model.ProtocolTCP,
						Name:     "tcp",
					},
				},
				Attributes: model.ServiceAttributes{
					Name:      "something.apps.internal",
					Namespace: "default",
				},
			},
		},
	}))
}

func TestServiceDiscovery_Instances_Filtering_By_Label(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	state := newSDTestState()

	state.mockClient.RoutesReturns(routesResponse, nil)
	state.mockClient.InternalRoutesReturns(internalRoutesResponse, nil)

	labelFilter := []model.Labels{
		map[string]string{"cfapp": "some-guid-a"},
	}
	instances, err := state.serviceDiscovery.InstancesByPort("process-guid-a.cfapps.io", 0, labelFilter)
	g.Expect(err).To(gomega.BeNil())

	servicePort := &model.Port{
		Port:     defaultServicePort,
		Protocol: model.ProtocolHTTP,
		Name:     "http",
	}
	service := &model.Service{
		Hostname: "process-guid-a.cfapps.io",
		Ports:    []*model.Port{servicePort},
		Attributes: model.ServiceAttributes{
			Name:      "process-guid-a.cfapps.io",
			Namespace: "default",
		},
	}

	g.Expect(instances).To(gomega.Equal([]*model.ServiceInstance{
		{
			Endpoint: model.NetworkEndpoint{
				Address:     "10.10.1.5",
				Port:        61005,
				ServicePort: servicePort,
			},
			Service: service,
			Labels:  model.Labels{"cfapp": "some-guid-a"},
		},
	}))
}

func TestServiceDiscovery_Instances_NotFound(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	state := newSDTestState()

	state.mockClient.RoutesReturns(routesResponse, nil)

	instances, err := state.serviceDiscovery.InstancesByPort("non-existent.cfapps.io", 0, nil)
	g.Expect(err).To(gomega.BeNil())
	g.Expect(instances).To(gomega.BeEmpty())
}

func TestServiceDiscovery_Instances_ClientRoutesError(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	state := newSDTestState()

	state.mockClient.RoutesReturns(nil, errors.New("potato"))

	serviceModel, err := state.serviceDiscovery.InstancesByPort("process-guid-b.cfapps.io", 0, nil)

	g.Expect(err).To(gomega.MatchError("getting routes: potato"))
	g.Expect(serviceModel).To(gomega.BeNil())
}

func TestServiceDiscovery_Instances_ClientInternalRoutesError(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	state := newSDTestState()

	state.mockClient.InternalRoutesReturns(nil, errors.New("banana"))

	serviceModel, err := state.serviceDiscovery.InstancesByPort("something.apps.internal", 0, nil)

	g.Expect(err).To(gomega.MatchError("getting internal routes: banana"))
	g.Expect(serviceModel).To(gomega.BeNil())
}

func TestServiceDiscovery_GetProxyServiceInstances(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	state := newSDTestState()

	instances, err := state.serviceDiscovery.GetProxyServiceInstances(&model.Proxy{IPAddress: "not-checked"})
	g.Expect(err).To(gomega.BeNil())
	g.Expect(instances).To(gomega.BeEmpty())
}
