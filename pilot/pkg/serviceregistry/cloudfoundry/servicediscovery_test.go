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
)

const defaultServicePort = 8080

func makeSampleClientResponse() *api.RoutesResponse {
	return &api.RoutesResponse{
		Backends: map[string]*api.BackendSet{
			"process-guid-a.cfapps.internal": {
				Backends: []*api.Backend{
					{
						Address: "10.10.1.5",
						Port:    61005,
					},
					{
						Address: "10.0.40.2",
						Port:    61008,
					},
				},
			},
			"process-guid-b.cfapps.internal": {
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
		},
	}
}

type sdTestState struct {
	mockClient       *mockCopilotClient
	serviceDiscovery *cloudfoundry.ServiceDiscovery
}

func newSDTestState() *sdTestState {
	mockClient := newMockCopilotClient()

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

	state.mockClient.RoutesOutput.Ret0 <- makeSampleClientResponse()
	state.mockClient.RoutesOutput.Ret1 <- nil

	// function under test
	serviceModels, err := state.serviceDiscovery.Services()
	g.Expect(err).To(gomega.BeNil())

	// it returns an Istio service for each Diego process
	g.Expect(serviceModels).To(gomega.HaveLen(2))
	g.Expect(serviceModels).To(gomega.ConsistOf([]*model.Service{
		{
			Hostname: "process-guid-a.cfapps.internal",
			Ports:    []*model.Port{{Port: defaultServicePort, Protocol: model.ProtocolHTTP}},
		},
		{
			Hostname: "process-guid-b.cfapps.internal",
			Ports:    []*model.Port{{Port: defaultServicePort, Protocol: model.ProtocolHTTP}},
		},
	}))
}

func TestServiceDiscovery_ServicesErrorHandling(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	state := newSDTestState()

	state.mockClient.RoutesOutput.Ret0 <- nil
	state.mockClient.RoutesOutput.Ret1 <- errors.New("banana")

	_, err := state.serviceDiscovery.Services()
	g.Expect(err).To(gomega.MatchError("getting services: banana"))
}

func TestServiceDiscovery_GetService_Success(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	state := newSDTestState()

	state.mockClient.RoutesOutput.Ret0 <- makeSampleClientResponse()
	state.mockClient.RoutesOutput.Ret1 <- nil

	serviceModel, err := state.serviceDiscovery.GetService("process-guid-b.cfapps.internal")

	g.Expect(err).To(gomega.BeNil())
	g.Expect(serviceModel).To(gomega.Equal(&model.Service{
		Hostname: "process-guid-b.cfapps.internal",
		Ports:    []*model.Port{{Port: defaultServicePort, Protocol: model.ProtocolHTTP}},
	}))
}

func TestServiceDiscovery_GetService_NotFound(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	state := newSDTestState()

	state.mockClient.RoutesOutput.Ret0 <- makeSampleClientResponse()
	state.mockClient.RoutesOutput.Ret1 <- nil

	serviceModel, err := state.serviceDiscovery.GetService("does-not-exist.cfapps.internal")

	g.Expect(err).To(gomega.BeNil())
	g.Expect(serviceModel).To(gomega.BeNil())
}

func TestServiceDiscovery_GetService_ClientError(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	state := newSDTestState()

	state.mockClient.RoutesOutput.Ret0 <- nil
	state.mockClient.RoutesOutput.Ret1 <- errors.New("potato")

	serviceModel, err := state.serviceDiscovery.GetService("process-guid-b.cfapps.internal")

	g.Expect(err).To(gomega.MatchError("getting services: potato"))
	g.Expect(serviceModel).To(gomega.BeNil())
}

func TestServiceDiscovery_Instances_Filtering(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	state := newSDTestState()

	state.mockClient.RoutesOutput.Ret0 <- makeSampleClientResponse()
	state.mockClient.RoutesOutput.Ret1 <- nil

	instances, err := state.serviceDiscovery.Instances("process-guid-a.cfapps.internal", nil, nil)
	g.Expect(err).To(gomega.BeNil())

	servicePort := &model.Port{
		Port:     defaultServicePort,
		Protocol: model.ProtocolHTTP,
	}
	service := &model.Service{
		Hostname: "process-guid-a.cfapps.internal",
		Ports:    []*model.Port{servicePort},
	}

	g.Expect(instances).To(gomega.ConsistOf([]*model.ServiceInstance{
		{
			Endpoint: model.NetworkEndpoint{
				Address:     "10.10.1.5",
				Port:        61005,
				ServicePort: servicePort,
			},
			Service: service,
		},
		{
			Endpoint: model.NetworkEndpoint{
				Address:     "10.0.40.2",
				Port:        61008,
				ServicePort: servicePort,
			},
			Service: service,
		},
	}))
}

func TestServiceDiscovery_Instances_NotFound(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	state := newSDTestState()

	state.mockClient.RoutesOutput.Ret0 <- makeSampleClientResponse()
	state.mockClient.RoutesOutput.Ret1 <- nil

	instances, err := state.serviceDiscovery.Instances("non-existent.cfapps.internal", nil, nil)
	g.Expect(err).To(gomega.BeNil())
	g.Expect(instances).To(gomega.BeNil())
}

func TestServiceDiscovery_Instances_ClientError(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	state := newSDTestState()

	state.mockClient.RoutesOutput.Ret0 <- nil
	state.mockClient.RoutesOutput.Ret1 <- errors.New("potato")

	serviceModel, err := state.serviceDiscovery.Instances("process-guid-b.cfapps.internal", nil, nil)

	g.Expect(err).To(gomega.MatchError("getting instances: potato"))
	g.Expect(serviceModel).To(gomega.BeNil())
}

func TestServiceDiscovery_GetSidecarServiceInstances(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	state := newSDTestState()

	state.mockClient.RoutesOutput.Ret0 <- makeSampleClientResponse()
	state.mockClient.RoutesOutput.Ret1 <- nil

	instances, err := state.serviceDiscovery.GetSidecarServiceInstances(model.Node{IPAddress: "not-checked"})
	g.Expect(err).To(gomega.BeNil())

	servicePort := &model.Port{
		Port:     defaultServicePort,
		Protocol: model.ProtocolHTTP,
	}

	serviceA := &model.Service{
		Hostname: "process-guid-a.cfapps.internal",
		Ports:    []*model.Port{servicePort},
	}

	serviceB := &model.Service{
		Hostname: "process-guid-b.cfapps.internal",
		Ports:    []*model.Port{servicePort},
	}

	g.Expect(instances).To(gomega.ConsistOf([]*model.ServiceInstance{
		{
			Endpoint: model.NetworkEndpoint{
				Address:     "10.10.1.5",
				Port:        61005,
				ServicePort: servicePort,
			},
			Service: serviceA,
		},
		{
			Endpoint: model.NetworkEndpoint{
				Address:     "10.0.40.2",
				Port:        61008,
				ServicePort: servicePort,
			},
			Service: serviceA,
		},
		{
			Endpoint: model.NetworkEndpoint{
				Address:     "10.0.50.4",
				Port:        61009,
				ServicePort: servicePort,
			},
			Service: serviceB,
		},
		{
			Endpoint: model.NetworkEndpoint{
				Address:     "10.0.60.2",
				Port:        61001,
				ServicePort: servicePort,
			},
			Service: serviceB,
		},
	}))
}

func TestServiceDiscovery_GetSidecarServiceInstances_ClientError(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	state := newSDTestState()

	state.mockClient.RoutesOutput.Ret0 <- nil
	state.mockClient.RoutesOutput.Ret1 <- errors.New("no instances")

	instances, err := state.serviceDiscovery.GetSidecarServiceInstances(model.Node{IPAddress: "not-checked"})

	g.Expect(err).To(gomega.MatchError("getting host instances: no instances"))
	g.Expect(instances).To(gomega.BeNil())
}

func TestServiceDiscovery_GetSidecarServiceInstances_NotFound(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	state := newSDTestState()

	state.mockClient.RoutesOutput.Ret0 <- nil
	state.mockClient.RoutesOutput.Ret1 <- nil

	instances, err := state.serviceDiscovery.GetSidecarServiceInstances(model.Node{IPAddress: "not-checked"})
	g.Expect(err).To(gomega.BeNil())
	g.Expect(instances).To(gomega.BeNil())
}
