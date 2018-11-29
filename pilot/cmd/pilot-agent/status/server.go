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

package status

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"sync"
	"syscall"

	"istio.io/istio/pilot/cmd/pilot-agent/status/app"
	"istio.io/istio/pilot/cmd/pilot-agent/status/ready"
	"istio.io/istio/pkg/log"
)

// Config for the status server.
type Config struct {
	StatusPort       uint16
	ProxyAdminPort   uint16
	ApplicationPorts []uint16
	AppProbeMap      app.ProbeMap
}

// Server provides an endpoint for handling status probes.
type Server struct {
	config Config
	mutex  sync.RWMutex
	cond   *sync.Cond
	ready  bool
}

// NewServer creates a new status server.
func NewServer(config Config) *Server {
	s := &Server{
		config: config,
	}
	s.cond = sync.NewCond(&s.mutex)
	return s
}

// Run opens a the status port and begins accepting probes.
func (s *Server) Run(ctx context.Context) {
	l, err := net.Listen("tcp", fmt.Sprintf(":%d", s.config.StatusPort))
	if err != nil {
		log.Errorf("Error listening on status port: %v", err.Error())
		return
	}
	defer func() { _ = l.Close() }()

	// for testing.
	if s.config.StatusPort == 0 {
		s.mutex.Lock()
		s.config.StatusPort = uint16(l.Addr().(*net.TCPAddr).Port)
		s.mutex.Unlock()
	}

	log.Infof("Opening status port %d\n", s.config.StatusPort)

	// Register the handlers for all probes.
	handler := http.NewServeMux()
	ready.RegisterHTTPHandlerFunc(handler, s.config.ProxyAdminPort, s.config.ApplicationPorts)
	app.RegisterHTTPHandlerFunc(handler, s.config.AppProbeMap)

	go func() {
		// Indicate that the server is ready.
		s.notifyReady()

		if err := http.Serve(l, handler); err != nil {
			log.Errorf("Status HTTP server exited with err: %s", err.Error())

			// If the server errors then pilot-agent can never pass readiness or liveness probes
			// Therefore, trigger graceful termination by sending SIGTERM to the binary pid
			p, err := os.FindProcess(os.Getpid())
			if err != nil {
				log.Errora(err)
			}
			log.Errora(p.Signal(syscall.SIGTERM))
		}
	}()

	// Wait for the agent to be shut down.
	<-ctx.Done()
	log.Info("Status server has successfully terminated")
	s.notifyReady()
}

// WaitForReady waits until the server is up and running and capable of receiving requests.
func (s *Server) WaitForReady() {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	if !s.ready {
		s.cond.Wait()
	}
}

// GetConfig provides access to the configuration for this server.
func (s *Server) GetConfig() Config {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	return s.config
}

func (s *Server) notifyReady() {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	if !s.ready {
		s.ready = true
		s.cond.Broadcast()
	}
}
