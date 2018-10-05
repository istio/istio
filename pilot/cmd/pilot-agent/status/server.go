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
	"fmt"
	"net"
	"net/http"

	"context"
	"os"
	"sync"

	"istio.io/istio/pilot/cmd/pilot-agent/status/ready"
	"istio.io/istio/pkg/log"
	"time"
)

const (
	// readyPath is for the pilot agent readiness itself.
	readyPath = "/healthz/ready"
	// appReadyPath is the path for the application after injecting.
	appReadinessPath = "/app/ready"
	// appHealthPath is the path for the application after injecting.
	appLivenessPath = "/app/liveness"
)

// Config for the status server.
type Config struct {
	StatusPort       uint16
	AdminPort        uint16
	// appReadinessPath is the original url path that app uses for readiness check.
	AppReadinessPath string
	// appLivenessPath is the original url path that app uses for liveness check.
	AppLivenessPath  string
	ApplicationPorts []uint16
}

// Server provides an endpoint for handling status probes.
type Server struct {
	statusPort          uint16
	ready               *ready.Probe
	appLivenessPath     string
	appReadinessPath    string
	mutex               sync.Mutex
	lastProbeSuccessful bool
}

// NewServer creates a new status server.
func NewServer(config Config) *Server {
	return &Server{
		statusPort:       config.StatusPort,
		appLivenessPath:  config.AppLivenessPath,
		appReadinessPath: config.AppReadinessPath,
		ready: &ready.Probe{
			AdminPort:        config.AdminPort,
			ApplicationPorts: config.ApplicationPorts,
		},
	}
}

// Run opens a the status port and begins accepting probes.
func (s *Server) Run(ctx context.Context) {
	log.Infof("Opening status port %d\n", s.statusPort)

	// Add the handler for ready probes.
	http.HandleFunc(readyPath, s.handleReadyProbe)
	http.HandleFunc(appReadinessPath, s.handleAppReadinessProbe)
	http.HandleFunc(appLivenessPath, s.handleAppLivenessProbe)

	l, err := net.Listen("tcp", fmt.Sprintf(":%d", s.statusPort))
	if err != nil {
		log.Errorf("Error listening on status port: %v", err.Error())
		return
	}
	defer l.Close()

	go func() {
		if err := http.Serve(l, nil); err != nil {
			log.Errora(err)
			os.Exit(-1)
		}
	}()

	// Wait for the agent to be shut down.
	<-ctx.Done()
}

func (s *Server) handleReadyProbe(w http.ResponseWriter, _ *http.Request) {
	err := s.ready.Check()

	s.mutex.Lock()
	defer s.mutex.Unlock()
	if err != nil {
		w.WriteHeader(http.StatusServiceUnavailable)

		log.Infof("Envoy proxy is NOT ready: %s", err.Error())
		s.lastProbeSuccessful = false
	} else {
		w.WriteHeader(http.StatusOK)

		if !s.lastProbeSuccessful {
			log.Info("Envoy proxy is ready")
		}
		s.lastProbeSuccessful = true
	}
}

func (s *Server) handleAppReadinessProbe(w http.ResponseWriter, req *http.Request) {
	requestStatusCode(fmt.Sprintf("http://127.0.0.1:%d/%s", s.statusPort, s.appReadinessPath), w, req)
}

func (s *Server) handleAppLivenessProbe(w http.ResponseWriter, req *http.Request) {
	requestStatusCode(fmt.Sprintf("http://127.0.0.1:%d/%s", s.statusPort, s.appLivenessPath), w, req)
}

func requestStatusCode(appURL string, w http.ResponseWriter, req *http.Request) {
	httpClient := &http.Client{
		// TODO: figure out the appropriate timeout?
		Timeout: 5*time.Second,
	}

	if req.URL == nil {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	req.URL.Path = appURL

	response, err := httpClient.Do(req)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
	}
	defer response.Body.Close()
	// We only write the status code to the response.
	w.WriteHeader(response.StatusCode)
}
