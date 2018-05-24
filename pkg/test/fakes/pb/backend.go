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

package pb

//go:generate protoc --gogo_out=plugins=grpc:. controller.proto

import (
	"context"
	"fmt"
	"net"
	"sync"

	google_rpc "github.com/gogo/googleapis/google/rpc"
	"github.com/golang/protobuf/proto"
	"google.golang.org/grpc"

	"istio.io/api/mixer/adapter/model/v1beta1"
	istio_mixer_adapter_model_v1beta11 "istio.io/api/mixer/adapter/model/v1beta1"
	"istio.io/istio/mixer/template/metric"
	"istio.io/istio/pkg/log"
)

const (
	// DefaultControlPort for the control service.
	DefaultControlPort = 1071
)

var scope = log.RegisterScope("fakes", "Scope for all fakes", 0)

// PolicyBackend is the implementation of a Fake policy backend. It can be ran either in a cluster or locally.
type PolicyBackend struct {
	listener net.Listener
	server   *grpc.Server

	settings map[string]string

	lock    sync.Mutex
	reports []proto.Message
}

var _ metric.HandleMetricServiceServer = &PolicyBackend{}

var _ v1beta1.InfrastructureBackendServer = &PolicyBackend{}

var _ ControllerServiceServer = &PolicyBackend{}

// NewPolicyBackend returns a new instance of PolicyBackend.
func NewPolicyBackend(port int) (*PolicyBackend, error) {
	pb := &PolicyBackend{
		settings: make(map[string]string),
	}

	listener, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		return nil, err
	}

	grpcServer := grpc.NewServer()
	v1beta1.RegisterInfrastructureBackendServer(grpcServer, pb)
	metric.RegisterHandleMetricServiceServer(grpcServer, pb)
	RegisterControllerServiceServer(grpcServer, pb)

	go func() {
		_ = grpcServer.Serve(listener)
	}()

	pb.listener = listener
	pb.server = grpcServer

	return pb, nil
}

// Set method of the control service.
func (b *PolicyBackend) Set(ctx context.Context, req *SetRequest) (*SetResponse, error) {
	scope.Debugf("PolicyBackend.Set %v", req)
	b.lock.Lock()
	defer b.lock.Unlock()

	for k, v := range req.Settings {
		b.settings[k] = v
	}

	return &SetResponse{}, nil
}

// Reset the internal state of the service.
func (b *PolicyBackend) Reset(ctx context.Context, req *ResetRequest) (*ResetResponse, error) {
	scope.Debugf("PolicyBackend.Reset %v", req)
	b.lock.Lock()
	defer b.lock.Unlock()

	for k := range b.settings {
		delete(b.settings, k)
	}

	b.reports = b.reports[0:0]

	return &ResetResponse{}, nil
}

// GetReports method of the control service.
func (b *PolicyBackend) GetReports(ctx context.Context, req *GetReportsRequest) (*GetReportsResponse, error) {
	scope.Debugf("PolicyBackend.GetReports %v", req)
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

	b.reports = b.reports[0:0]

	return resp, nil
}

// Close closes the gRPC backend and the associated listener.
func (b *PolicyBackend) Close() error {
	scope.Debug("PolicyBackend.Close")
	b.server.Stop()
	return b.listener.Close()
}

// Validate is an implementation InfrastructureBackendServer.Validate.
func (b *PolicyBackend) Validate(context.Context, *v1beta1.ValidateRequest) (
	*v1beta1.ValidateResponse, error) {

	return &v1beta1.ValidateResponse{
		Status: &google_rpc.Status{
			Code: int32(google_rpc.OK),
		},
	}, nil
}

// CreateSession is an implementation InfrastructureBackendServer.CreateSession.
func (b *PolicyBackend) CreateSession(context.Context, *v1beta1.CreateSessionRequest) (
	*v1beta1.CreateSessionResponse, error) {

	return &v1beta1.CreateSessionResponse{
		Status: &google_rpc.Status{
			Code: int32(google_rpc.OK),
		},
	}, nil
}

// CloseSession is an implementation InfrastructureBackendServer.CloseSession.
func (b *PolicyBackend) CloseSession(context.Context, *v1beta1.CloseSessionRequest) (
	*v1beta1.CloseSessionResponse, error) {

	return &v1beta1.CloseSessionResponse{
		Status: &google_rpc.Status{
			Code: int32(google_rpc.OK),
		},
	}, nil
}

// HandleMetric is an implementation HandleMetricServiceServer.HandleMetric.
func (b *PolicyBackend) HandleMetric(ctx context.Context, req *metric.HandleMetricRequest) (
	*istio_mixer_adapter_model_v1beta11.ReportResult, error) {
	scope.Debugf("PolicyBackend.HandleMetric %v", req)

	b.lock.Lock()
	defer b.lock.Unlock()
	for _, ins := range req.Instances {
		b.reports = append(b.reports, ins)
	}

	return &istio_mixer_adapter_model_v1beta11.ReportResult{}, nil
}
