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

package noopbackend

import (
	"context"
	"fmt"
	"net"
	"time"

	rpc "github.com/gogo/googleapis/google/rpc"
	"google.golang.org/grpc"

	adptModel "istio.io/api/mixer/adapter/model/v1beta1"
	"istio.io/istio/mixer/template/authorization"
	"istio.io/istio/mixer/template/checknothing"
	"istio.io/istio/mixer/template/listentry"
	"istio.io/istio/mixer/template/logentry"
	"istio.io/istio/mixer/template/metric"
	"istio.io/istio/mixer/template/quota"
	"istio.io/istio/mixer/template/reportnothing"
	"istio.io/istio/mixer/template/tracespan"
)

type (
	// Server is basic server interface
	Server interface {
		Addr() net.Addr
		Close() error
		Run()
		Wait() error
	}
	// NoSessionServer models no session adapter backend.
	NoSessionServer struct {
		listener net.Listener
		shutdown chan error
		server   *grpc.Server
	}
)

var _ authorization.HandleAuthorizationServiceServer = &NoSessionServer{}
var _ checknothing.HandleCheckNothingServiceServer = &NoSessionServer{}
var _ listentry.HandleListEntryServiceServer = &NoSessionServer{}
var _ logentry.HandleLogEntryServiceServer = &NoSessionServer{}
var _ metric.HandleMetricServiceServer = &NoSessionServer{}
var _ quota.HandleQuotaServiceServer = &NoSessionServer{}
var _ reportnothing.HandleReportNothingServiceServer = &NoSessionServer{}
var _ tracespan.HandleTraceSpanServiceServer = &NoSessionServer{}

// HandleAuthorization handles authorization and responds with default check result.
func (s *NoSessionServer) HandleAuthorization(c context.Context, r *authorization.HandleAuthorizationRequest) (*adptModel.CheckResult, error) {
	return &adptModel.CheckResult{
		Status: rpc.Status{
			Code: int32(rpc.OK),
		},
		ValidDuration: 1000000000 * time.Second,
		ValidUseCount: 1000000000}, nil
}

// HandleCheckNothing handles checknothing and responds with default check result.
func (s *NoSessionServer) HandleCheckNothing(c context.Context, r *checknothing.HandleCheckNothingRequest) (*adptModel.CheckResult, error) {
	return &adptModel.CheckResult{
		Status: rpc.Status{
			Code: int32(rpc.OK),
		},
		ValidDuration: 1000000000 * time.Second,
		ValidUseCount: 1000000000}, nil
}

// HandleListEntry handles listentry and responds with default check result.
func (s *NoSessionServer) HandleListEntry(c context.Context, r *listentry.HandleListEntryRequest) (*adptModel.CheckResult, error) {
	return &adptModel.CheckResult{
		Status: rpc.Status{
			Code: int32(rpc.OK),
		},
		ValidDuration: 1000000000 * time.Second,
		ValidUseCount: 1000000000}, nil
}

// HandleLogEntry handles logentry and responds with default report result.
func (s *NoSessionServer) HandleLogEntry(c context.Context, r *logentry.HandleLogEntryRequest) (*adptModel.ReportResult, error) {
	return &adptModel.ReportResult{}, nil
}

// HandleMetric handles metric and responds with default report result.
func (s *NoSessionServer) HandleMetric(c context.Context, r *metric.HandleMetricRequest) (*adptModel.ReportResult, error) {
	return &adptModel.ReportResult{}, nil
}

// HandleQuota handles quota and responds with default quota result.
func (s *NoSessionServer) HandleQuota(c context.Context, r *quota.HandleQuotaRequest) (*adptModel.QuotaResult, error) {
	return &adptModel.QuotaResult{}, nil
}

// HandleTraceSpan handles tracespan and responds with default report result.
func (s *NoSessionServer) HandleTraceSpan(c context.Context, r *tracespan.HandleTraceSpanRequest) (*adptModel.ReportResult, error) {
	return &adptModel.ReportResult{}, nil
}

// HandleReportNothing handles reportnothing and responds with default report result.
func (s *NoSessionServer) HandleReportNothing(c context.Context, r *reportnothing.HandleReportNothingRequest) (*adptModel.ReportResult, error) {
	return &adptModel.ReportResult{}, nil
}

// Addr returns the listening address of the server
func (s *NoSessionServer) Addr() net.Addr {
	return s.listener.Addr()
}

// Run starts the server run
func (s *NoSessionServer) Run() {
	s.shutdown = make(chan error, 1)
	go func() {
		err := s.server.Serve(s.listener)
		// notify closer we're done
		s.shutdown <- err
	}()
}

// Wait waits for server to stop
func (s *NoSessionServer) Wait() error {
	if s.shutdown == nil {
		return fmt.Errorf("server not running")
	}
	err := <-s.shutdown
	s.shutdown = nil
	return err
}

// Close gracefully shuts down the server
func (s *NoSessionServer) Close() error {
	if s.shutdown != nil {
		s.server.GracefulStop()
		_ = s.Wait()
	}
	if s.listener != nil {
		_ = s.listener.Close()
	}
	return nil
}

// NewNoSessionServer creates a new no session server from given args.
func NewNoSessionServer(addr string) (Server, error) {
	if addr == "" {
		addr = "0"
	}
	s := &NoSessionServer{}
	var err error
	if s.listener, err = net.Listen("tcp", fmt.Sprintf(":%s", addr)); err != nil {
		_ = s.Close()
		return nil, fmt.Errorf("unable to listen on socket: %v", err)
	}
	fmt.Printf("listening on :%v", s.listener.Addr())
	s.server = grpc.NewServer()
	authorization.RegisterHandleAuthorizationServiceServer(s.server, s)
	checknothing.RegisterHandleCheckNothingServiceServer(s.server, s)
	listentry.RegisterHandleListEntryServiceServer(s.server, s)
	logentry.RegisterHandleLogEntryServiceServer(s.server, s)
	metric.RegisterHandleMetricServiceServer(s.server, s)
	quota.RegisterHandleQuotaServiceServer(s.server, s)
	reportnothing.RegisterHandleReportNothingServiceServer(s.server, s)
	tracespan.RegisterHandleTraceSpanServiceServer(s.server, s)
	return s, nil
}
