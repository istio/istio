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

package bypass

import (
	"context"
	"fmt"
	"net"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc"

	rpc "istio.io/gogo-genproto/googleapis/google/rpc"

	"istio.io/api/mixer/adapter/model/v1beta1"
	"istio.io/istio/mixer/adapter/bypass/config"
	"istio.io/istio/mixer/pkg/adapter"
	"istio.io/istio/mixer/pkg/adapter/test"
	"istio.io/istio/mixer/template/checknothing"
	"istio.io/istio/mixer/template/metric"
	"istio.io/istio/mixer/template/quota"
	"istio.io/istio/mixer/template/reportnothing"
)

func TestBasic(t *testing.T) {
	// Setup the service
	s := &service{}
	port, err := s.start()
	if err != nil {
		t.Fatalf("service start error: %v", err)
	}
	defer s.stop()

	info := GetInfo()

	if !contains(info.SupportedTemplates, checknothing.TemplateName) ||
		!contains(info.SupportedTemplates, reportnothing.TemplateName) ||
		!contains(info.SupportedTemplates, metric.TemplateName) ||
		!contains(info.SupportedTemplates, quota.TemplateName) {
		t.Error("Didn't find all expected supported templates")
	}

	cfg := info.DefaultConfig.(*config.Params)
	cfg.BackendAddress = fmt.Sprintf(":%d", port)

	b := info.NewBuilder().(*builder)
	b.SetCheckNothingTypes(nil)
	b.SetReportNothingTypes(nil)
	b.SetMetricTypes(nil)
	b.SetQuotaTypes(nil)
	b.SetAdapterConfig(cfg)

	if err := b.Validate(); err != nil {
		t.Errorf("Got error %v, expecting success", err)
	}

	handler, buildErr := b.Build(context.Background(), test.NewEnv(t))
	if buildErr != nil {
		t.Errorf("Got error %v, expecting success", buildErr)
	}

	checkNothingHandler := handler.(checknothing.Handler)
	cnInstance := &checknothing.Instance{
		Name: "foo",
	}
	if result, err := checkNothingHandler.HandleCheckNothing(context.TODO(), cnInstance); err != nil {
		t.Errorf("Got error %v, expecting success", err)
	} else {
		if !reflect.DeepEqual(result.Status, rpc.Status{Code: int32(rpc.OK)}) {
			t.Errorf("Got status %v, expecting %v", result.Status, rpc.Status{Code: int32(rpc.OK)})
		}
		if result.ValidDuration < 1000*time.Second {
			t.Errorf("Got duration of %v, expecting at least 1000 seconds", result.ValidDuration)
		}
		if result.ValidUseCount < 1000 {
			t.Errorf("Got use count of %d, expecting at least 1000", result.ValidUseCount)
		}
	}

	reportNothingHandler := handler.(reportnothing.Handler)
	if err := reportNothingHandler.HandleReportNothing(context.TODO(), nil); err != nil {
		t.Errorf("Got error %v, expecting success", err)
	}

	metricHandler := handler.(metric.Handler)
	if err := metricHandler.HandleMetric(context.TODO(), nil); err != nil {
		t.Errorf("Got error %v, expecting success", err)
	}

	quotaHandler := handler.(quota.Handler)
	qInstance := &quota.Instance{
		Name: "foo",
	}
	if result, err := quotaHandler.HandleQuota(context.TODO(), qInstance, adapter.QuotaArgs{QuotaAmount: 100}); err != nil {
		t.Errorf("Got error %v, expecting success", err)
	} else {
		if result.ValidDuration < 1000*time.Second {
			t.Errorf("Got duration of %v, expecting at least 1000 seconds", result.ValidDuration)
		}
		if result.Amount != 100 {
			t.Errorf("Got %d quota, expecting 100", result.Amount)
		}
	}

	if err := handler.Close(); err != nil {
		t.Errorf("Got error %v, expecting success", err)
	}
}

func contains(s []string, e string) bool {
	for _, a := range s {
		if a == e {
			return true
		}
	}
	return false
}

type service struct {
	listener net.Listener
	server   *grpc.Server
}

var _ checknothing.HandleCheckNothingServiceServer = &service{}
var _ reportnothing.HandleReportNothingServiceServer = &service{}
var _ metric.HandleMetricServiceServer = &service{}
var _ quota.HandleQuotaServiceServer = &service{}

var _ v1beta1.InfrastructureBackendServer = &service{}

func (s *service) HandleCheckNothing(context.Context, *checknothing.HandleCheckNothingRequest) (*v1beta1.CheckResult, error) {

	return &v1beta1.CheckResult{
		ValidDuration: time.Second * 1000,
		ValidUseCount: 1000,
		Status: rpc.Status{
			Code: int32(rpc.OK),
		},
	}, nil
}

func (s *service) HandleReportNothing(context.Context, *reportnothing.HandleReportNothingRequest) (*v1beta1.ReportResult, error) {

	return &v1beta1.ReportResult{}, nil
}

// HandleMetric is an implementation HandleMetricServiceServer.HandleMetric.
func (s *service) HandleMetric(ctx context.Context, req *metric.HandleMetricRequest) (
	*v1beta1.ReportResult, error) {

	return &v1beta1.ReportResult{}, nil
}

func (s *service) HandleQuota(ctx context.Context, req *quota.HandleQuotaRequest) (*v1beta1.QuotaResult, error) {
	return &v1beta1.QuotaResult{
		Quotas: map[string]v1beta1.QuotaResult_Result{
			req.Instance.Name: {
				ValidDuration: time.Second * 1000,
				GrantedAmount: 100,
			},
		},
	}, nil
}

// Close closes the gRPC backend and the associated listener.
func (s *service) Close() error {
	return nil
}

// Validate is an implementation InfrastructureBackendServer.Validate.
func (s *service) Validate(context.Context, *v1beta1.ValidateRequest) (
	*v1beta1.ValidateResponse, error) {

	return &v1beta1.ValidateResponse{
		Status: &rpc.Status{
			Code: int32(rpc.OK),
		},
	}, nil
}

// CreateSession is an implementation InfrastructureBackendServer.CreateSession.
func (s *service) CreateSession(context.Context, *v1beta1.CreateSessionRequest) (
	*v1beta1.CreateSessionResponse, error) {

	return &v1beta1.CreateSessionResponse{
		Status: &rpc.Status{
			Code: int32(rpc.OK),
		},
	}, nil
}

// CloseSession is an implementation InfrastructureBackendServer.CloseSession.
func (s *service) CloseSession(context.Context, *v1beta1.CloseSessionRequest) (
	*v1beta1.CloseSessionResponse, error) {

	return &v1beta1.CloseSessionResponse{
		Status: &rpc.Status{
			Code: int32(rpc.OK),
		},
	}, nil
}

func (s *service) start() (int, error) {
	listener, err := net.Listen("tcp", ":0")
	if err != nil {
		return 0, err
	}

	grpcServer := grpc.NewServer()
	v1beta1.RegisterInfrastructureBackendServer(grpcServer, s)
	checknothing.RegisterHandleCheckNothingServiceServer(grpcServer, s)
	reportnothing.RegisterHandleReportNothingServiceServer(grpcServer, s)
	metric.RegisterHandleMetricServiceServer(grpcServer, s)
	quota.RegisterHandleQuotaServiceServer(grpcServer, s)

	go func() {
		_ = grpcServer.Serve(listener)
	}()

	s.listener = listener
	s.server = grpcServer

	addr := s.listener.Addr().String()
	parts := strings.Split(addr, ":")
	port, err := strconv.ParseInt(parts[len(parts)-1], 10, 32)
	return int(port), err
}

func (s *service) stop() {
	s.server.Stop()
	_ = s.listener.Close()
}
