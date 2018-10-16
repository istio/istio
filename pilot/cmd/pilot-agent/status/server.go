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
	"strconv"
	"strings"
	"sync"
	"time"

	"istio.io/istio/pilot/cmd/pilot-agent/status/ready"
	"istio.io/istio/pkg/log"
)

const (
	// readyPath is for the pilot agent readiness itself.
	readyPath = "/healthz/ready"
	// appReadinessPath is the path handled by pilot agent for application's readiness probe.
	appReadinessPath = "/app/ready"
	// appLivenessPath is the path handled by pilot agent for application's liveness probe.
	appLivenessPath = "/app/live"
)

// AppProbeInfo defines the information for Pilot agent to take over application probing.
type AppProbeInfo struct {
	Path string
	Port uint16
}

// Config for the status server.
type Config struct {
	StatusPort       uint16
	AdminPort        uint16
	ApplicationPorts []uint16
	// AppReadinessURL specifies the path, including the port to take over Kubernetes readiness probe.
	// This allows Kubernetes probing to work even mTLS is turned on for the workload.
	AppReadinessURL string
	// AppLivenessURL specifies the path, including the port to take over Kubernetes liveness probe.
	// This allows Kubernetes probing to work even mTLS is turned on for the workload.
	AppLivenessURL string
}

// Server provides an endpoint for handling status probes.
type Server struct {
	statusPort          uint16
	ready               *ready.Probe
	appLiveURL          string
	appReadyURL         string
	mutex               sync.RWMutex
	lastProbeSuccessful bool
}

// NewServer creates a new status server.
func NewServer(config Config) *Server {
	return &Server{
		statusPort:  config.StatusPort,
		appLiveURL:  config.AppLivenessURL,
		appReadyURL: config.AppReadinessURL,
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

	// TODO: we require non empty url to take over the health check. Make sure this is consistent in injector.
	if s.appReadyURL != "" {
		log.Infof("Pilot agent takes over readiness probe, path %v", s.appReadyURL)
		http.HandleFunc(appReadinessPath, s.handleAppReadinessProbe)
	}

	if s.appLiveURL != "" {
		log.Infof("Pilot agent takes over liveness probe, path %v", s.appLiveURL)
		http.HandleFunc(appLivenessPath, s.handleAppLivenessProbe)
	}

	l, err := net.Listen("tcp", fmt.Sprintf(":%d", s.statusPort))
	if err != nil {
		log.Errorf("Error listening on status port: %v", err.Error())
		return
	}
	// for testing.
	if s.statusPort == 0 {
		addrs := strings.Split(l.Addr().String(), ":")
		allocatedPort, _ := strconv.Atoi(addrs[len(addrs)-1])
		s.mutex.Lock()
		s.statusPort = uint16(allocatedPort)
		s.mutex.Unlock()
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

	var lastProbeSuccessful bool
	if err != nil {
		w.WriteHeader(http.StatusServiceUnavailable)

		log.Infof("Envoy proxy is NOT ready: %s", err.Error())
		lastProbeSuccessful = false
	} else {
		w.WriteHeader(http.StatusOK)

		if !s.lastProbeSuccessful {
			log.Info("Envoy proxy is ready")
		}
		lastProbeSuccessful = true
	}
	s.mutex.Lock()
	s.lastProbeSuccessful = lastProbeSuccessful
	s.mutex.Unlock()
}

func (s *Server) handleAppReadinessProbe(w http.ResponseWriter, req *http.Request) {
	requestStatusCode(fmt.Sprintf("http://127.0.0.1%s", s.appReadyURL), w, req)
}

func (s *Server) handleAppLivenessProbe(w http.ResponseWriter, req *http.Request) {
	requestStatusCode(fmt.Sprintf("http://127.0.0.1%s", s.appLiveURL), w, req)
}

func requestStatusCode(appURL string, w http.ResponseWriter, req *http.Request) {
	httpClient := &http.Client{
		// TODO: figure out the appropriate timeout?
		Timeout: 10 * time.Second,
	}

	appReq, err := http.NewRequest(req.Method, appURL, req.Body)
	for key, value := range req.Header {
		appReq.Header[key] = value
	}

	if err != nil {
		log.Errorf("Failed to copy request to probe app %v", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	response, err := httpClient.Do(appReq)
	if err != nil {
		log.Errorf("Request to probe app failed: %v", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	// We only write the status code to the response.
	w.WriteHeader(response.StatusCode)
}
