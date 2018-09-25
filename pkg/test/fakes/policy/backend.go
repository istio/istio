//  Copyright 2018 Istio Authors
//
//  Licensed under the Apache License, Version 2.0 (the "License");
//  you may not use this file except in compliance with the License.
//  You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
//  Unless required by applicable law or agreed to in writing, software
//  distributed under the License is distributed on an "AS IS" BASIS,
//  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//  See the License for the specific language governing permissions and
//  limitations under the License.

package policy

//go:generate $GOPATH/src/istio.io/istio/bin/protoc.sh --gogo_out=plugins=grpc:. controller.proto

import (
	"context"
	"fmt"
	"net"
	"sync"

	google_rpc "github.com/gogo/googleapis/google/rpc"
	"github.com/golang/protobuf/proto"
	"google.golang.org/grpc"

	"istio.io/istio/mixer/template/checknothing"

	"istio.io/api/mixer/adapter/model/v1beta1"
	istio_mixer_adapter_model_v1beta11 "istio.io/api/mixer/adapter/model/v1beta1"
	"istio.io/istio/mixer/template/metric"
	"istio.io/istio/pkg/log"
)

const (
	// DefaultPort for the backend service.
	DefaultPort = 1071
)

var scope = log.RegisterScope("fakes", "Scope for all fakes", 0)

// Backend is the implementation of a Fake policy backend. It can be ran either in a cluster or locally.
type Backend struct {
	port int

	listener net.Listener
	server   *grpc.Server

	settings settings

	lock    sync.Mutex
	reports []proto.Message
}

var _ metric.HandleMetricServiceServer = &Backend{}
var _ checknothing.HandleCheckNothingServiceServer = &Backend{}

var _ v1beta1.InfrastructureBackendServer = &Backend{}

var _ ControllerServiceServer = &Backend{}

// NewPolicyBackend returns a new instance of Backend.
func NewPolicyBackend(port int) *Backend {
	return &Backend{
		settings: make(map[string]string),
		port:     port,
	}
}

// Start the gRPC service for the policy backend.
func (b *Backend) Start() error {
	scope.Infof("Starting Policy Backend at port: %d", b.port)

	listener, err := net.Listen("tcp", fmt.Sprintf(":%d", b.port))
	if err != nil {
		return err
	}

	grpcServer := grpc.NewServer()
	v1beta1.RegisterInfrastructureBackendServer(grpcServer, b)
	metric.RegisterHandleMetricServiceServer(grpcServer, b)
	checknothing.RegisterHandleCheckNothingServiceServer(grpcServer, b)
	RegisterControllerServiceServer(grpcServer, b)

	go func() {
		scope.Info("Starting the GRPC service")
		_ = grpcServer.Serve(listener)
	}()

	b.listener = listener
	b.server = grpcServer

	return nil
}

// Set method of the control service.
func (b *Backend) Set(ctx context.Context, req *SetRequest) (*SetResponse, error) {
	b.lock.Lock()
	defer b.lock.Unlock()

	for k, v := range req.Settings {
		scope.Infof("Backend.Set %s = %s", k, v)
		b.settings[k] = v
	}

	return &SetResponse{}, nil
}

// Reset the internal state of the service.
func (b *Backend) Reset(ctx context.Context, req *ResetRequest) (*ResetResponse, error) {
	scope.Infof("Backend.Reset %v", req)
	b.lock.Lock()
	defer b.lock.Unlock()

	for k := range b.settings {
		delete(b.settings, k)
	}

	b.reports = b.reports[0:0]

	return &ResetResponse{}, nil
}

// GetReports method of the control service.
func (b *Backend) GetReports(ctx context.Context, req *GetReportsRequest) (*GetReportsResponse, error) {
	scope.Infof("Backend.GetReports %v", req)
	b.lock.Lock()
	defer b.lock.Unlock()

	resp := &GetReportsResponse{}
	for _, ins := range b.reports {
		a, err := toAny(ins)
		if err != nil {
			return nil, err
		}
		resp.Instances = append(resp.Instances, a)
	}

	return resp, nil
}

// Close closes the gRPC backend and the associated listener.
func (b *Backend) Close() error {
	scope.Info("Backend.Close")
	b.server.Stop()
	return b.listener.Close()
}

// Validate is an implementation InfrastructureBackendServer.Validate.
func (b *Backend) Validate(ctx context.Context, req *v1beta1.ValidateRequest) (
	*v1beta1.ValidateResponse, error) {
	scope.Infof("Backend.Validate %v", req)

	return &v1beta1.ValidateResponse{
		Status: &google_rpc.Status{
			Code: int32(google_rpc.OK),
		},
	}, nil
}

// CreateSession is an implementation InfrastructureBackendServer.CreateSession.
func (b *Backend) CreateSession(ctx context.Context, req *v1beta1.CreateSessionRequest) (
	*v1beta1.CreateSessionResponse, error) {
	scope.Infof("Backend.CreateSession %v", req)

	return &v1beta1.CreateSessionResponse{
		Status: &google_rpc.Status{
			Code: int32(google_rpc.OK),
		},
	}, nil
}

// CloseSession is an implementation InfrastructureBackendServer.CloseSession.
func (b *Backend) CloseSession(ctx context.Context, req *v1beta1.CloseSessionRequest) (
	*v1beta1.CloseSessionResponse, error) {
	scope.Infof("Backend.CloseSession %v", req)

	return &v1beta1.CloseSessionResponse{
		Status: &google_rpc.Status{
			Code: int32(google_rpc.OK),
		},
	}, nil
}

// HandleMetric is an implementation HandleMetricServiceServer.HandleMetric.
func (b *Backend) HandleMetric(ctx context.Context, req *metric.HandleMetricRequest) (
	*istio_mixer_adapter_model_v1beta11.ReportResult, error) {
	scope.Infof("Backend.HandleMetric %v", req)

	b.lock.Lock()
	defer b.lock.Unlock()
	for _, ins := range req.Instances {
		b.reports = append(b.reports, ins)
	}

	return &istio_mixer_adapter_model_v1beta11.ReportResult{}, nil
}

// HandleCheckNothing is an implementation of HandleCheckNothingServiceServer.HandleCheckNothing.
func (b *Backend) HandleCheckNothing(ctx context.Context, req *checknothing.HandleCheckNothingRequest) (
	*istio_mixer_adapter_model_v1beta11.CheckResult, error) {
	scope.Infof("Backend.HandleCheckNothing %v", req)

	b.lock.Lock()
	defer b.lock.Unlock()

	if b.settings.getDenyCheck() {
		return &istio_mixer_adapter_model_v1beta11.CheckResult{
			Status: google_rpc.Status{
				Code:    int32(google_rpc.UNAUTHENTICATED),
				Message: "bypass-backend-unauthenticated",
			},
		}, nil
	}

	return &istio_mixer_adapter_model_v1beta11.CheckResult{
		Status: google_rpc.Status{
			Code: int32(google_rpc.OK),
		},
		ValidDuration: 0,
		ValidUseCount: 1,
	}, nil
}
