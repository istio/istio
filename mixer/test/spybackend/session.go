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

package spybackend

import (
	"context"
	"fmt"
	"net"

	"google.golang.org/grpc"

	adptModel "istio.io/api/mixer/adapter/model/v1beta1"
	"istio.io/istio/mixer/template/listentry"
	"istio.io/istio/mixer/template/metric"
	"istio.io/istio/mixer/template/quota"
)

type sessionServer struct {
	noSessionServer
}

var _ adptModel.InfrastructureBackendServer = &sessionServer{}
var _ metric.HandleMetricServiceServer = &sessionServer{}
var _ listentry.HandleListEntryServiceServer = &sessionServer{}
var _ quota.HandleQuotaServiceServer = &sessionServer{}

// Non request time rpcs
func (s *sessionServer) Validate(c context.Context, r *adptModel.ValidateRequest) (*adptModel.ValidateResponse, error) {
	s.requests.validateRequest = append(s.requests.validateRequest, r)
	return s.behavior.validateResponse, s.behavior.validateError
}
func (s *sessionServer) CreateSession(c context.Context, r *adptModel.CreateSessionRequest) (*adptModel.CreateSessionResponse, error) {
	s.requests.createSessionRequest = append(s.requests.createSessionRequest, r)
	return s.behavior.createSessionResponse, s.behavior.createSessionError
}
func (s *sessionServer) CloseSession(c context.Context, r *adptModel.CloseSessionRequest) (*adptModel.CloseSessionResponse, error) {
	s.requests.closeSessionRequest = append(s.requests.closeSessionRequest, r)
	return s.behavior.closeSessionResponse, s.behavior.closeSessionError
}

// nolint:deadcode
func newSessionServer(a *args) (server, error) {
	s := &sessionServer{noSessionServer{behavior: a.behavior, requests: a.requests}}
	s.server = grpc.NewServer()
	var err error

	if s.listener, err = net.Listen("tcp", fmt.Sprintf(":%d", 0)); err != nil {
		_ = s.Close()
		return nil, fmt.Errorf("unable to listen on socket: %v", err)
	}

	adptModel.RegisterInfrastructureBackendServer(s.server, s)
	metric.RegisterHandleMetricServiceServer(s.server, s)
	listentry.RegisterHandleListEntryServiceServer(s.server, s)
	quota.RegisterHandleQuotaServiceServer(s.server, s)

	return s, nil
}
