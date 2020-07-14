//  Copyright Istio Authors
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

//nolint: lll
//go:generate $REPO_ROOT/bin/protoc.sh --gogoslick_out=plugins=grpc,Mgoogle/protobuf/any.proto=github.com/gogo/protobuf/types:. -I. controller.proto

import (
	"context"
	"fmt"
	"net"
	"sync"
	"time"

	types "github.com/gogo/protobuf/types"
	"github.com/golang/protobuf/proto"
	"google.golang.org/grpc"

	google_rpc "istio.io/gogo-genproto/googleapis/google/rpc"

	"istio.io/api/mixer/adapter/model/v1beta1"
	policy "istio.io/api/policy/v1beta1"
	"istio.io/istio/mixer/pkg/status"
	"istio.io/istio/mixer/template/checknothing"
	"istio.io/istio/mixer/template/metric"
	"istio.io/istio/mixer/test/keyval"
	"istio.io/pkg/log"
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
var _ keyval.HandleKeyvalServiceServer = &Backend{}

var _ v1beta1.InfrastructureBackendServer = &Backend{}

var _ ControllerServiceServer = &Backend{}

// NewPolicyBackend returns a new instance of Backend.
func NewPolicyBackend(port int) *Backend {
	return &Backend{
		settings: make(map[string]string),
		port:     port,
	}
}

// Port returns the port number of the backend.
func (b *Backend) Port() int {
	return b.port
}

// Start the gRPC service for the policy backend.
func (b *Backend) Start() error {
	scope.Info("Starting Policy Backend")

	listener, err := net.Listen("tcp", fmt.Sprintf(":%d", b.port))
	if err != nil {
		return err
	}

	b.port = listener.Addr().(*net.TCPAddr).Port

	grpcServer := grpc.NewServer()
	v1beta1.RegisterInfrastructureBackendServer(grpcServer, b)
	metric.RegisterHandleMetricServiceServer(grpcServer, b)
	checknothing.RegisterHandleCheckNothingServiceServer(grpcServer, b)
	keyval.RegisterHandleKeyvalServiceServer(grpcServer, b)
	RegisterControllerServiceServer(grpcServer, b)

	go func() {
		scope.Infof("Starting the GRPC service at port: %d", b.port)
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
	scope.Debugf("Backend.GetReports %v", req)
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
	_ = b.listener.Close()
	return nil
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
	*v1beta1.ReportResult, error) {
	scope.Infof("Backend.HandleMetric %v", req)

	b.lock.Lock()
	defer b.lock.Unlock()
	for _, ins := range req.Instances {
		b.reports = append(b.reports, ins)
	}

	return &v1beta1.ReportResult{}, nil
}

// HandleCheckNothing is an implementation of HandleCheckNothingServiceServer.HandleCheckNothing.
func (b *Backend) HandleCheckNothing(ctx context.Context, req *checknothing.HandleCheckNothingRequest) (
	*v1beta1.CheckResult, error) {
	scope.Infof("Backend.HandleCheckNothing %v", req)

	b.lock.Lock()
	defer b.lock.Unlock()

	params := &Params{}
	if req.AdapterConfig != nil {
		if err := types.UnmarshalAny(req.AdapterConfig, params); err != nil {
			return nil, err
		}
	}
	if b.settings.getDenyCheck() || (params.CheckParams != nil && !params.CheckParams.CheckAllow) {
		scope.Infof("Backend.HandleCheckNothing => UNAUTHENTICATED")
		return &v1beta1.CheckResult{
			Status: google_rpc.Status{
				Code:    int32(google_rpc.UNAUTHENTICATED),
				Message: "bypass-backend-unauthenticated",
			},
		}, nil
	}

	scope.Infof("Backend.HandleCheckNothing => OK")
	validDuration := b.settings.getValidDuration()
	validCount := b.settings.getValidCount()
	if params.CheckParams != nil {
		validDuration, _ = types.DurationFromProto(params.CheckParams.ValidDuration)
		validCount = int32(params.CheckParams.ValidCount)
	}
	return &v1beta1.CheckResult{
		Status: google_rpc.Status{
			Code: int32(google_rpc.OK),
		},
		ValidDuration: validDuration,
		ValidUseCount: validCount,
	}, nil
}

// HandleKeyval is an implementation of HandleKeyvalServiceServer.HandleKeyval.
func (b *Backend) HandleKeyval(ctx context.Context, req *keyval.HandleKeyvalRequest) (*keyval.HandleKeyvalResponse, error) {
	params := &Params{}
	if err := params.Unmarshal(req.AdapterConfig.Value); err != nil {
		return nil, err
	}
	key := req.Instance.Key
	scope.Infof("look up %q\n", key)
	value, ok := params.Table[key]
	if ok {
		return &keyval.HandleKeyvalResponse{
			Result: &v1beta1.CheckResult{ValidDuration: 5 * time.Second},
			Output: &keyval.OutputMsg{Value: value},
		}, nil
	}
	return &keyval.HandleKeyvalResponse{
		Result: &v1beta1.CheckResult{
			Status: google_rpc.Status{
				Code: int32(google_rpc.NOT_FOUND),
				Details: []*types.Any{status.PackErrorDetail(&policy.DirectHttpResponse{
					Body: fmt.Sprintf("<error_detail>key %q not found</error_detail>", key),
				})},
			},
		},
	}, nil
}
