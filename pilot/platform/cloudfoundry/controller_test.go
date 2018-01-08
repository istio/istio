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
	"testing"

	"code.cloudfoundry.org/copilot/api"

	"istio.io/istio/pilot/model"
	"istio.io/istio/pilot/platform/cloudfoundry"
	"istio.io/istio/pilot/platform/test"
)

func buildBackends() map[string]*api.BackendSet {
	return map[string]*api.BackendSet{
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
	}
}

func buildExpectedControllerView(backends map[string]*api.BackendSet) *model.ControllerView {
	modelSvcs := []*model.Service{}
	modelInsts := []*model.ServiceInstance{}

	for hostname := range backends {
		service := &model.Service{
			Hostname: hostname,
			Ports: []*model.Port{
				{
					Port:     8080,
					Protocol: model.ProtocolTCP,
				},
			},
		}
		modelSvcs = append(modelSvcs, service)
		backendSet, ok := backends[hostname]
		if !ok {
			continue
		}
		for _, backend := range backendSet.GetBackends() {
			modelInsts = append(modelInsts, &model.ServiceInstance{
				Endpoint: model.NetworkEndpoint{
					Address:     backend.Address,
					Port:        int(backend.Port),
					ServicePort: service.Ports[0],
				},
				Service: service,
			})
		}
	}
	return test.BuildExpectedControllerView(modelSvcs, modelInsts)
}

// The only thing being tested are the public interfaces of the controller
// namely: Handle() and Run(). Everything else is implementation detail.
func TestController(t *testing.T) {
	testBackends := buildBackends()
	client := newMockCopilotClient()
	client.RoutesOutput.Ret0 <- &api.RoutesResponse{
		Backends: testBackends,
	}
	client.RoutesOutput.Ret1 <- nil
	mockHandler := test.NewMockControllerViewHandler()
	controller := cloudfoundry.NewController(client, *mockHandler.GetTicker())
	mockHandler.AssertControllerOK(t, controller, buildExpectedControllerView(testBackends))
}
