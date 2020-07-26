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
//go:generate go run $REPO_ROOT/mixer/tools/mixgen/main.go adapter -n listbackend-nosession -c $REPO_ROOT/mixer/adapter/list/config/config.proto_descriptor -s=false -t listentry -o nosession.yaml -d example

package listbackend

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"sync"

	"google.golang.org/grpc"

	adptModel "istio.io/api/mixer/adapter/model/v1beta1"
	"istio.io/istio/mixer/adapter/list"
	"istio.io/istio/mixer/adapter/list/config"
	"istio.io/istio/mixer/pkg/adapter"
	"istio.io/istio/mixer/pkg/runtime/handler"
	"istio.io/istio/mixer/template/listentry"
	"istio.io/pkg/pool"
)

type (
	// Server is basic server interface
	Server interface {
		Addr() net.Addr
		Close() error
		Run()
		Wait() error
	}

	// nosessionServer models no session adapter backend.
	nosessionServer struct {
		listener net.Listener
		shutdown chan error
		server   *grpc.Server

		rawcfg      []byte
		builder     adapter.HandlerBuilder
		env         adapter.Env
		builderLock sync.RWMutex

		h listentry.Handler
	}
)

var _ listentry.HandleListEntryServiceServer = &nosessionServer{}

func (s *nosessionServer) getHandler(rawcfg []byte) (listentry.Handler, error) {
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

	if bytes.Equal(rawcfg, s.rawcfg) {
		return s.h, nil
	}

	s.builder.SetAdapterConfig(cfg)
	if ce := s.builder.Validate(); ce != nil {
		return nil, ce
	}

	h, err := s.builder.Build(context.Background(), s.env)
	if err != nil {
		return nil, err
	}
	s.rawcfg = rawcfg
	s.h = h.(listentry.Handler)
	return s.h, err
}

func instance(in *listentry.InstanceMsg) *listentry.Instance {
	return &listentry.Instance{
		Name:  in.Name,
		Value: in.Value,
	}
}

// HandleListEntry records listrequest and responds with the programmed response
func (s *nosessionServer) HandleListEntry(ctx context.Context, r *listentry.HandleListEntryRequest) (*adptModel.CheckResult, error) {
	s.env.Logger().Infof("received '%v'", *r)
	var h listentry.Handler
	var err error
	if r.AdapterConfig != nil {
		h, err = s.getHandler(r.AdapterConfig.Value)
	} else {
		h, err = s.getHandler(nil)
	}
	if err != nil {
		return nil, err
	}

	var cr adapter.CheckResult
	if cr, err = h.HandleListEntry(ctx, instance(r.Instance)); err != nil {
		return nil, err
	}
	s.env.Logger().Infof("response for '%v' is '%v'", *r, cr)
	return &adptModel.CheckResult{Status: cr.Status, ValidUseCount: cr.ValidUseCount, ValidDuration: cr.ValidDuration}, nil
}

// Addr returns the listening address of the server
func (s *nosessionServer) Addr() net.Addr {
	return s.listener.Addr()
}

// Run starts the server run
func (s *nosessionServer) Run() {
	s.shutdown = make(chan error, 1)
	go func() {
		err := s.server.Serve(s.listener)

		// notify closer we're done
		s.shutdown <- err
	}()
}

// Wait waits for server to stop
func (s *nosessionServer) Wait() error {
	if s.shutdown == nil {
		return fmt.Errorf("server not running")
	}

	err := <-s.shutdown
	s.shutdown = nil
	return err
}

// Close gracefully shuts down the server
func (s *nosessionServer) Close() error {
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
	listInf := list.GetInfo()
	listener, err := net.Listen("tcp", fmt.Sprintf(":%s", addr))
	if err != nil {
		return nil, fmt.Errorf("unable to listen on socket: %v", err)
	}
	s := &nosessionServer{
		listener: listener,
		builder:  listInf.NewBuilder(),
		env:      handler.NewEnv(0, "list-backend-nosession", pool.NewGoroutinePool(5, false), []string{""}),
		rawcfg:   []byte{},
	}
	fmt.Printf("listening on :%v\n", s.listener.Addr())
	s.server = grpc.NewServer()
	listentry.RegisterHandleListEntryServiceServer(s.server, s)
	return s, nil
}
