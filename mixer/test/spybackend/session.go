// Copyright Istio Authors
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

// nolint:lll
//go:generate go run $REPO_ROOT/mixer/tools/mixgen/main.go adapter -n spybackend-session -s=true -t metric -t quota -t listentry -o session.yaml -d example

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
	NoSessionServer
}

var _ adptModel.InfrastructureBackendServer = &sessionServer{}
var _ metric.HandleMetricServiceServer = &sessionServer{}
var _ listentry.HandleListEntryServiceServer = &sessionServer{}
var _ quota.HandleQuotaServiceServer = &sessionServer{}

// Non request time rpcs
func (s *sessionServer) Validate(c context.Context, r *adptModel.ValidateRequest) (*adptModel.ValidateResponse, error) {
	s.Requests.ValidateRequest = append(s.Requests.ValidateRequest, r)
	return s.Behavior.ValidateResponse, s.Behavior.ValidateError
}
func (s *sessionServer) CreateSession(c context.Context, r *adptModel.CreateSessionRequest) (*adptModel.CreateSessionResponse, error) {
	s.Requests.CreateSessionRequest = append(s.Requests.CreateSessionRequest, r)
	return s.Behavior.CreateSessionResponse, s.Behavior.CreateSessionError
}
func (s *sessionServer) CloseSession(c context.Context, r *adptModel.CloseSessionRequest) (*adptModel.CloseSessionResponse, error) {
	s.Requests.CloseSessionRequest = append(s.Requests.CloseSessionRequest, r)
	return s.Behavior.CloseSessionResponse, s.Behavior.CloseSessionError
}

// nolint:deadcode
func newSessionServer(a *Args) (Server, error) {
	s := &sessionServer{NoSessionServer{Behavior: a.Behavior, Requests: a.Requests}}
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
