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

type (
	server interface {
		Addr() net.Addr
		Close() error
		Run()
	}

	noSessionServer struct {
		listener net.Listener
		shutdown chan error
		server   *grpc.Server
		behavior *behavior
		requests *requests
	}
)

var _ metric.HandleMetricServiceServer = &noSessionServer{}
var _ listentry.HandleListEntryServiceServer = &noSessionServer{}
var _ quota.HandleQuotaServiceServer = &noSessionServer{}

func (s *noSessionServer) HandleMetric(c context.Context, r *metric.HandleMetricRequest) (*adptModel.ReportResult, error) {
	s.requests.handleMetricRequest = append(s.requests.handleMetricRequest, r)
	return s.behavior.handleMetricResult, s.behavior.handleMetricError
}
func (s *noSessionServer) HandleListEntry(c context.Context, r *listentry.HandleListEntryRequest) (*adptModel.CheckResult, error) {
	s.requests.handleListEntryRequest = append(s.requests.handleListEntryRequest, r)
	return s.behavior.handleListEntryResult, s.behavior.handleListEntryError
}
func (s *noSessionServer) HandleQuota(c context.Context, r *quota.HandleQuotaRequest) (*adptModel.QuotaResult, error) {
	s.requests.handleQuotaRequest = append(s.requests.handleQuotaRequest, r)
	return s.behavior.handleQuotaResult, s.behavior.handleQuotaError
}

func (s *noSessionServer) Addr() net.Addr {
	return s.listener.Addr()
}

func (s *noSessionServer) Run() {
	s.shutdown = make(chan error, 1)
	go func() {
		err := s.server.Serve(s.listener)

		// notify closer we're done
		s.shutdown <- err
	}()
}

func (s *noSessionServer) Wait() error {
	if s.shutdown == nil {
		return fmt.Errorf("server not running")
	}

	err := <-s.shutdown
	s.shutdown = nil
	return err
}

func (s *noSessionServer) Close() error {
	if s.shutdown != nil {
		s.server.GracefulStop()
		_ = s.Wait()
	}

	if s.listener != nil {
		_ = s.listener.Close()
	}

	return nil
}

// nolint:deadcode
func newNoSessionServer(a *args) (server, error) {
	s := &noSessionServer{behavior: a.behavior, requests: a.requests}
	var err error

	if s.listener, err = net.Listen("tcp", fmt.Sprintf(":%d", 0)); err != nil {
		_ = s.Close()
		return nil, fmt.Errorf("unable to listen on socket: %v", err)
	}

	s.server = grpc.NewServer()
	metric.RegisterHandleMetricServiceServer(s.server, s)
	listentry.RegisterHandleListEntryServiceServer(s.server, s)
	quota.RegisterHandleQuotaServiceServer(s.server, s)

	return s, nil
}
