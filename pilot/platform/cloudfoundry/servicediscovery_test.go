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

	"code.cloudfoundry.org/copilot/api"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	"istio.io/istio/pilot/model"
	"istio.io/istio/pilot/platform/cloudfoundry"
)

var _ = Describe("ServiceDiscovery", func() {
	var (
		client           *mockCopilotClient
		serviceDiscovery *cloudfoundry.ServiceDiscovery
		routesResponse   *api.RoutesResponse
	)

	BeforeEach(func() {
		client = newMockCopilotClient()
		routesResponse = &api.RoutesResponse{
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
		serviceDiscovery = &cloudfoundry.ServiceDiscovery{
			Client: client,
		}
	})

	Describe("Services", func() {
		It("returns an Istio service for each Diego process", func() {
			client.RoutesOutput.Ret0 <- routesResponse
			client.RoutesOutput.Ret1 <- nil
			serviceModels, err := serviceDiscovery.Services()
			Expect(err).NotTo(HaveOccurred())

			Expect(serviceModels).To(HaveLen(2))
			Expect(serviceModels).To(ConsistOf([]*model.Service{
				{
					Hostname: "process-guid-a.cfapps.internal",
					Ports:    []*model.Port{{Port: 8080, Protocol: model.ProtocolTCP}},
				},
				{
					Hostname: "process-guid-b.cfapps.internal",
					Ports:    []*model.Port{{Port: 8080, Protocol: model.ProtocolTCP}},
				},
			}))
		})

		Context("when the CloudFoundry client returns an error", func() {
			BeforeEach(func() {
				client.RoutesOutput.Ret0 <- nil
				client.RoutesOutput.Ret1 <- errors.New("banana")
			})
			It("wraps and returns the error", func() {
				_, err := serviceDiscovery.Services()
				Expect(err).To(MatchError("getting services: banana"))
			})
		})
	})

	Describe("GetService", func() {
		Context("when the CloudFoundry client returns an error", func() {
			BeforeEach(func() {
				client.RoutesOutput.Ret0 <- nil
				client.RoutesOutput.Ret1 <- errors.New("banana")
			})
			It("wraps and returns the error", func() {
				_, err := serviceDiscovery.GetService("foo")
				Expect(err).To(MatchError("getting services: banana"))
			})
		})

		It("returns an Istio service by a hostname", func() {
			client.RoutesOutput.Ret0 <- routesResponse
			client.RoutesOutput.Ret1 <- nil
			serviceModel, err := serviceDiscovery.GetService("process-guid-b.cfapps.internal")
			Expect(err).NotTo(HaveOccurred())
			Expect(serviceModel).To(Equal(
				&model.Service{
					Hostname: "process-guid-b.cfapps.internal",
					Ports:    []*model.Port{{Port: 8080, Protocol: model.ProtocolTCP}},
				},
			))
		})

		It("returns a nil service when a hostname is not found", func() {
			client.RoutesOutput.Ret0 <- routesResponse
			client.RoutesOutput.Ret1 <- nil
			service, err := serviceDiscovery.GetService("non-existent-service.whatever")
			Expect(err).NotTo(HaveOccurred())
			Expect(service).To(BeNil())
		})
	})

	Describe("Instances", func() {
		Context("when the provided hostname does not exist", func() {
			It("returns nil with no error", func() {
				client.RoutesOutput.Ret0 <- routesResponse
				client.RoutesOutput.Ret1 <- nil
				instances, err := serviceDiscovery.Instances("non-existent-process-guid.whatever", nil, nil)
				Expect(err).NotTo(HaveOccurred())

				Expect(instances).To(BeEmpty())
			})
		})

		Context("when the CloudFoundry client return an error", func() {
			BeforeEach(func() {
				client.RoutesOutput.Ret0 <- nil
				client.RoutesOutput.Ret1 <- errors.New("banana")
			})

			It("wraps and returns the error", func() {
				_, err := serviceDiscovery.Instances("something", nil, nil)
				Expect(err).To(MatchError("getting instances: banana"))
			})
		})

		Context("when the provided hostname points to a known process guid", func() {
			It("returns the filtered set of instances for the given hostname", func() {
				client.RoutesOutput.Ret0 <- routesResponse
				client.RoutesOutput.Ret1 <- nil
				instances, err := serviceDiscovery.Instances("process-guid-a.cfapps.internal", nil, nil)
				Expect(err).NotTo(HaveOccurred())

				servicePort := &model.Port{
					Port:     8080,
					Protocol: model.ProtocolTCP,
				}
				service := &model.Service{
					Hostname: "process-guid-a.cfapps.internal",
					Ports:    []*model.Port{servicePort},
				}

				Expect(instances).To(ConsistOf([]*model.ServiceInstance{
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
			})
		})
	})
})
