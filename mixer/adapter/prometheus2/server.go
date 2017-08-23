// Copyright 2017 Istio Authors.
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

package prometheus

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"

	"istio.io/mixer/pkg/adapter"
)

type (
	server interface {
		io.Closer

		Start(adapter.Env, http.Handler) error
	}

	serverInst struct {
		addr string
		srv  *http.Server
	}
)

const (
	metricsPath = "/metrics"
	defaultAddr = ":42422"
)

func newServer(addr string) server {
	return &serverInst{addr: addr}
}

func (s *serverInst) Start(env adapter.Env, metricsHandler http.Handler) error {
	var listener net.Listener
	listener, err := net.Listen("tcp", s.addr)
	if err != nil {
		return fmt.Errorf("could not start prometheus metrics server: %v", err)
	}
	srvMux := http.NewServeMux()
	srvMux.Handle(metricsPath, metricsHandler)
	s.srv = &http.Server{Addr: s.addr, Handler: srvMux}
	env.ScheduleDaemon(func() {
		env.Logger().Infof("serving prometheus metrics on %s", s.addr)
		if err := s.srv.Serve(listener.(*net.TCPListener)); err != nil {
			_ = env.Logger().Errorf("prometheus HTTP server error: %v", err) // nolint: gas
		}
	})
	return nil
}

func (s *serverInst) Close() error {
	if s.srv == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	return s.srv.Shutdown(ctx)
}
