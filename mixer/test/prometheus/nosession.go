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
//go:generate go run $REPO_ROOT/mixer/tools/mixgen/main.go adapter -n prometheus-nosession -s=false  -c $REPO_ROOT/mixer/adapter/prometheus/config/config.proto_descriptor   -t metric -o prometheus-nosession.yaml -d example

package prometheus

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"sync"

	"google.golang.org/grpc"

	adptModel "istio.io/api/mixer/adapter/model/v1beta1"
	"istio.io/api/policy/v1beta1"
	"istio.io/istio/mixer/adapter/prometheus"
	"istio.io/istio/mixer/adapter/prometheus/config"
	"istio.io/istio/mixer/pkg/adapter"
	"istio.io/istio/mixer/pkg/runtime/handler"
	"istio.io/istio/mixer/template/metric"
	"istio.io/pkg/pool"
)

type (
	// Server is basic server interface
	Server interface {
		Addr() string
		Close() error
		PromPort() int
		Run()
	}
	promServer interface {
		io.Closer
		Port() int
	}

	// NoSessionServer models no session adapter backend.
	NoSessionServer struct {
		listener net.Listener
		shutdown chan error
		server   *grpc.Server

		rawcfg      []byte
		builder     adapter.HandlerBuilder
		env         adapter.Env
		builderLock sync.RWMutex

		h metric.Handler

		promServer promServer
	}
)

var _ metric.HandleMetricServiceServer = &NoSessionServer{}

func (s *NoSessionServer) getHandler(rawcfg []byte) (metric.Handler, error) {
	s.builderLock.RLock()
	if bytes.Equal(rawcfg, s.rawcfg) {
		h := s.h
		s.builderLock.RUnlock()
		return h, nil
	}
	s.builderLock.RUnlock()

	// establish session

	cfg := &config.Params{}

	if err := cfg.Unmarshal(rawcfg); err != nil {
		return nil, err
	}

	s.builderLock.Lock()
	defer s.builderLock.Unlock()

	// check again if someone else beat you to this.
	if bytes.Equal(rawcfg, s.rawcfg) {
		return s.h, nil
	}

	s.env.Logger().Infof("Loaded handler with: %v", cfg)

	s.builder.SetAdapterConfig(cfg)
	if ce := s.builder.Validate(); ce != nil {
		return nil, ce
	}

	h, err := s.builder.Build(context.Background(), s.env)
	if err != nil {
		return nil, s.env.Logger().Errorf("could not build: %v", err)
	}
	s.rawcfg = rawcfg
	s.h = h.(metric.Handler)
	return s.h, err
}

func instances(in []*metric.InstanceMsg) []*metric.Instance {
	out := make([]*metric.Instance, 0, len(in))
	for _, inst := range in {
		out = append(out, &metric.Instance{
			Name:       inst.Name,
			Value:      decodeValue(inst.Value.GetValue()),
			Dimensions: decodeDimensions(inst.Dimensions),
		})
	}
	return out
}

func decodeDimensions(in map[string]*v1beta1.Value) map[string]interface{} {
	out := make(map[string]interface{}, len(in))
	for k, v := range in {
		out[k] = decodeValue(v.GetValue())
	}
	return out
}

func decodeValue(in interface{}) interface{} {
	switch t := in.(type) {
	case *v1beta1.Value_StringValue:
		return t.StringValue
	case *v1beta1.Value_Int64Value:
		return t.Int64Value
	case *v1beta1.Value_DoubleValue:
		return t.DoubleValue
	default:
		return fmt.Sprintf("%v", in)
	}
}

// HandleMetric records metric entries and responds with the programmed response
func (s *NoSessionServer) HandleMetric(ctx context.Context, r *metric.HandleMetricRequest) (*adptModel.ReportResult, error) {
	h, err := s.getHandler(r.AdapterConfig.Value)
	if err != nil {
		return nil, err
	}

	if err = h.HandleMetric(ctx, instances(r.Instances)); err != nil {
		return nil, s.env.Logger().Errorf("Could not process: %v", err)
	}

	return &adptModel.ReportResult{}, nil
}

// Addr returns the listening address of the server
func (s *NoSessionServer) Addr() string {
	return s.listener.Addr().String()
}

// PromPort returns the listening address of the prometheus server
func (s *NoSessionServer) PromPort() int {
	return s.promServer.Port()
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

	if s.promServer != nil {
		return s.promServer.Close()
	}

	return nil
}

// NewNoSessionServer creates a new no session server from given args.
func NewNoSessionServer(addr uint16, promAddr uint16) (*NoSessionServer, error) {
	saddr := fmt.Sprintf(":%d", addr)
	pddr := fmt.Sprintf(":%d", promAddr)

	gp := pool.NewGoroutinePool(5, false)
	inf, srv := prometheus.GetInfoWithAddr(pddr)
	s := &NoSessionServer{builder: inf.NewBuilder(),
		env:    handler.NewEnv(0, "prometheus-nosession", gp, []string{""}),
		rawcfg: []byte{0xff, 0xff},
	}
	var err error
	if s.listener, err = net.Listen("tcp", saddr); err != nil {
		_ = s.Close()
		return nil, fmt.Errorf("unable to listen on socket: %v", err)
	}

	fmt.Printf("listening on :%v\n", s.listener.Addr())
	s.server = grpc.NewServer()
	metric.RegisterHandleMetricServiceServer(s.server, s)
	if _, err = s.getHandler(nil); err != nil {
		return nil, err
	}
	s.promServer = srv
	return s, nil
}
