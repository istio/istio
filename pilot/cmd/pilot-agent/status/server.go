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

// RunAndTerminateProcessOnError runs the server and terminates this process (via SIGTERM)
// if a (non-graceful, i.e. ErrServerClosed) serving error occurs. When the status server
// is down, the the pilot-agent will no longer be able to pass readiness or liveness
// probes. For this reason, the pilot agent calls this variant of Run.
func (s *Server) RunAndTerminateProcessOnError(ctx context.Context) {
	if err := s.Run(ctx); err != nil {
		p, err := os.FindProcess(os.Getpid())
		if err != nil {
			log.Errora(err)
		}
		// TODO(nmittler): Should this be in an 'else'?
		log.Errora(p.Signal(syscall.SIGTERM))
	}
}

// Run opens a the status port and begins accepting probes. This should only be called directly
// from tests.
func (s *Server) Run(ctx context.Context) error {
	l, err := net.Listen("tcp", fmt.Sprintf(":%d", s.config.StatusPort))
	if err != nil {
		log.Errorf("Error listening on status port: %v", err.Error())
		return err
	}
	defer func() { _ = l.Close() }()

	// Set the status port used by the listener. This may have been dynamically assigned if
	// StatusPort was originally set to 0.
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

	httpServer := &http.Server{
		Handler: handler,
	}

	// Used to detect any non-graceful errors encountered while serving requests.
	var servingErr error

	servingStopped := make(chan struct{}, 1)
	go func() {
		// When this function exits, signal back that serving thread has terminated.
		defer close(servingStopped)

		// Indicate that the server is now serving requests.
		s.notifyReady()

		if err := httpServer.Serve(l); err != nil && err != http.ErrServerClosed {
			// Save the serving error.
			servingErr = err
			log.Errorf("status HTTP server exited with err: %s", err.Error())
		}
	}()

	// When a shutdown is triggered, close the server.
	go func() {
		<-ctx.Done()
		_ = httpServer.Close()
	}()

	// Wait for the serving thread to exit.
	<-servingStopped

	log.Info("Status server has terminated")

	// Return any error encountered while serving
	return servingErr
}

// WaitForReady waits until the server is up and running and capable of receiving requests.
// For testing purposes only.
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

// notifyReady is used to provide a signal to the caller of WaitForReady that this server is now
// running (i.e. serving HTTP).
func (s *Server) notifyReady() {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	if !s.ready {
		s.ready = true
		s.cond.Broadcast()
	}
}
