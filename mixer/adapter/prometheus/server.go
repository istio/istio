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
	"sync"
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

		lock    sync.Mutex // protects resources below
		srv     *http.Server
		handler *metaHandler
		refCnt  int
	}
)

const (
	metricsPath = "/metrics"
	defaultAddr = ":42422"
)

func newServer(addr string) *serverInst {
	return &serverInst{addr: addr}
}

// metaHandler switches the delegate without downtime.
type metaHandler struct {
	delegate http.Handler
	lock     sync.RWMutex
}

func (m *metaHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	m.lock.RLock()
	m.delegate.ServeHTTP(w, r)
	m.lock.RUnlock()
}

func (m *metaHandler) setDelegate(delegate http.Handler) {
	m.lock.Lock()
	m.delegate = delegate
	m.lock.Unlock()
}

// Start the prometheus singleton listener.
func (s *serverInst) Start(env adapter.Env, metricsHandler http.Handler) (err error) {
	s.lock.Lock()
	defer s.lock.Unlock()

	// if server is already running,
	// just switch the delegate handler.
	if s.srv != nil {
		s.refCnt++
		s.handler.setDelegate(metricsHandler)
		return nil
	}

	listener, err := net.Listen("tcp", s.addr)
	if err != nil {
		return fmt.Errorf("could not start prometheus metrics server: %v", err)
	}

	srvMux := http.NewServeMux()
	s.handler = &metaHandler{delegate: metricsHandler}
	srvMux.Handle(metricsPath, s.handler)
	srv := &http.Server{Addr: s.addr, Handler: srvMux}
	env.ScheduleDaemon(func() {
		env.Logger().Infof("serving prometheus metrics on %s", s.addr)
		if err := srv.Serve(listener.(*net.TCPListener)); err != nil {
			if err == http.ErrServerClosed {
				env.Logger().Infof("HTTP server stopped")
			} else {
				_ = env.Logger().Errorf("prometheus HTTP server error: %v", err) // nolint: gas
			}
		}
	})
	s.srv = srv
	s.refCnt++

	return nil
}

// Close -- closes the HTTP server if reference Count is 0.
func (s *serverInst) Close() error {
	s.lock.Lock()
	defer s.lock.Unlock()

	// not started yet, nothing to close.
	if s.srv == nil {
		return nil
	}

	s.refCnt--
	if s.refCnt > 0 {
		return nil
	}

	// reference count is 0, cleanup.

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	srv := s.srv
	s.srv = nil
	s.handler = nil
	return srv.Shutdown(ctx)
}
