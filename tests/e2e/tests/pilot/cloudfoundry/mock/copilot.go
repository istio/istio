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

package mock

import (
	"context"
	"sync"

	"code.cloudfoundry.org/copilot/api"
)

type CopilotHandler struct {
	RoutesResponseData map[string]*api.BackendSet
	mutex              sync.Mutex
}

func (h *CopilotHandler) PopulateRoute(host string, ipAddr string, port int) {
	h.mutex.Lock()
	defer h.mutex.Unlock()
	backendSet := h.RoutesResponseData[host]
	if backendSet == nil {
		backendSet = &api.BackendSet{}
	}
	backendSet.Backends = append(backendSet.Backends, &api.Backend{
		Address: ipAddr,
		Port:    uint32(port),
	})
	h.RoutesResponseData[host] = backendSet
}

func (h *CopilotHandler) Health(context.Context, *api.HealthRequest) (*api.HealthResponse, error) {
	return &api.HealthResponse{
		Healthy: true,
	}, nil
}

func (h *CopilotHandler) Routes(context.Context, *api.RoutesRequest) (*api.RoutesResponse, error) {
	h.mutex.Lock()
	defer h.mutex.Unlock()
	return &api.RoutesResponse{
		Backends: h.RoutesResponseData,
	}, nil
}
