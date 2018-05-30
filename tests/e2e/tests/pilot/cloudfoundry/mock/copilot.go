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
	"crypto/md5"
	"fmt"
	"io"
	"sync"
	"time"

	"code.cloudfoundry.org/copilot/api"
)

type CopilotHandler struct {
	RoutesResponseData         []*api.RouteWithBackends
	InternalRoutesResponseData []*api.InternalRouteWithBackends
	mutex                      sync.Mutex
}

func (h *CopilotHandler) PopulateInternalRoute(port int, hostname, vip, ip string) {
	h.mutex.Lock()
	defer h.mutex.Unlock()
	h.InternalRoutesResponseData = append(h.InternalRoutesResponseData, &api.InternalRouteWithBackends{
		Hostname: hostname,
		Vip:      vip,
		Backends: &api.BackendSet{
			Backends: []*api.Backend{
				{
					Address: ip,
					Port:    uint32(port),
				},
			},
		},
	})
}

func (h *CopilotHandler) PopulateRoute(host string, ipAddr string, port int, path string) {
	h.mutex.Lock()
	defer h.mutex.Unlock()
	hasher := md5.New()
	io.WriteString(hasher, time.Now().String())
	h.RoutesResponseData = append(h.RoutesResponseData, &api.RouteWithBackends{
		Hostname: host,
		Path:     path,
		Backends: &api.BackendSet{
			Backends: []*api.Backend{
				{
					Address: ipAddr,
					Port:    uint32(port),
				},
			},
		},
		CapiProcessGuid: fmt.Sprintf("%x", hasher.Sum(nil)),
	})
}

func (h *CopilotHandler) Health(context.Context, *api.HealthRequest) (*api.HealthResponse, error) {
	return &api.HealthResponse{
		Healthy: true,
	}, nil
}

func (h *CopilotHandler) InternalRoutes(context.Context, *api.InternalRoutesRequest) (*api.InternalRoutesResponse, error) {
	h.mutex.Lock()
	defer h.mutex.Unlock()
	return &api.InternalRoutesResponse{
		InternalRoutes: h.InternalRoutesResponseData,
	}, nil
}

func (h *CopilotHandler) Routes(context.Context, *api.RoutesRequest) (*api.RoutesResponse, error) {
	h.mutex.Lock()
	defer h.mutex.Unlock()
	return &api.RoutesResponse{
		Routes: h.RoutesResponseData,
	}, nil
}
