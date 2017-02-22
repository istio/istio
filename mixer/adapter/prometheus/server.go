// Copyright 2017 the Istio Authors.
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
	"fmt"
	"io"
	"net"
	"net/http"

	"github.com/prometheus/client_golang/prometheus/promhttp"

	"istio.io/mixer/pkg/adapter"
)

// TODO: rethink when we switch to go1.8. the graceful shutdown changes
// coming to http.Server should help tremendously

type (
	server interface {
		io.Closer

		Start(adapter.Logger) error
	}

	serverInst struct {
		server

		addr       string
		connCloser io.Closer
	}
)

const (
	metricsPath = "/metrics"
	defaultAddr = ":42422"
)

func newServer(addr string) server {
	return &serverInst{addr: addr}
}

func (s *serverInst) Start(logger adapter.Logger) error {
	var listener net.Listener
	srv := &http.Server{Addr: s.addr}
	listener, err := net.Listen("tcp", s.addr)
	if err != nil {
		return fmt.Errorf("could not start prometheus metrics server: %v", err)
	}

	http.Handle(metricsPath, promhttp.Handler())
	go func() {
		logger.Infof("serving prometheus metrics on %s", s.addr)
		if err := srv.Serve(listener.(*net.TCPListener)); err != nil {
			_ = logger.Errorf("prometheus HTTP server error: %v", err)
		}
	}()

	s.connCloser = listener
	return nil
}

func (s *serverInst) Close() error {
	if s.connCloser != nil {
		return s.connCloser.Close()
	}
	return nil
}
