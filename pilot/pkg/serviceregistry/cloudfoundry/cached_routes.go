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

package cloudfoundry

import (
	"context"
	"fmt"
	"time"

	copilotapi "code.cloudfoundry.org/copilot/api"
)

//go:generate counterfeiter -o ./fakes/copilot_client.go --fake-name CopilotClient . copilotClient
// CopilotClient defines a local interface for interacting with Cloud Foundry Copilot
type copilotClient interface {
	copilotapi.IstioCopilotClient
}

//go:generate counterfeiter -o fakes/logger.go --fake-name Logger . logger
type logger interface {
	Infoa(args ...interface{})
}

type cachedRoutes struct {
	client           copilotClient
	repo             *copilotapi.RoutesResponse
	logger           logger
	routeRefreshTime time.Duration
	lastUpdated      time.Time
}

func NewCachedRoutes(client copilotClient, logger logger, routeRefreshTime string) *cachedRoutes {
	refresh, _ := time.ParseDuration(routeRefreshTime)

	return &cachedRoutes{
		client:           client,
		repo:             &copilotapi.RoutesResponse{},
		logger:           logger,
		routeRefreshTime: refresh,
		lastUpdated:      time.Now().Add(-refresh),
	}
}

func (r *cachedRoutes) Get() (*copilotapi.RoutesResponse, error) {
	now := time.Now()
	if now.Sub(r.lastUpdated) > r.routeRefreshTime {
		r.logger.Infoa("retrieving routes from copilot")

		resp, err := r.client.Routes(context.Background(), new(copilotapi.RoutesRequest))
		if err != nil {
			return nil, fmt.Errorf("getting services from copilot: %s", err)
		}
		r.repo = resp
		r.lastUpdated = now
	} else {
		r.logger.Infoa("retrieving routes from cache")
	}

	return r.repo, nil
}

func (r *cachedRoutes) GetInternal() (*copilotapi.InternalRoutesResponse, error) {
	return r.client.InternalRoutes(context.Background(), new(copilotapi.InternalRoutesRequest))
}
