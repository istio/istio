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
	"encoding/json"
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
	corev1 "k8s.io/api/core/v1"
)

const (
	// readyPath is for the pilot agent readiness itself.
	readyPath = "/healthz/ready"
	// IstioAppPortHeader is the header name to indicate the app port for health check.
	IstioAppPortHeader = "istio-app-probe-port"
)

// KubeHTTPProbers holds the information about a Kubernetes pod prober.
type KubeAppProbers struct {
	// Liveness is the map from container name of a pod to the liveness probe.
	Liveness map[string]*corev1.HTTPGetAction `json:"liveness"`
	// Readiness is the map from container name of a pod to the readiness probe.
	Readiness map[string]*corev1.HTTPGetAction `json:"readiness"`
}

// Config for the status server.
type Config struct {
	StatusPort         uint16
	AdminPort          uint16
	ApplicationPorts   []uint16
	KubeAppHTTPProbers string
}

// Server provides an endpoint for handling status probes.
type Server struct {
	statusPort          uint16
	ready               *ready.Probe
	mutex               sync.RWMutex
	lastProbeSuccessful bool
	appKubeProbers      KubeAppProbers
}

// NewServer creates a new status server.
func NewServer(config Config) (*Server, error) {
	s := &Server{
		statusPort: config.StatusPort,
		ready: &ready.Probe{
			AdminPort:        config.AdminPort,
			ApplicationPorts: config.ApplicationPorts,
		},
	}
	if config.KubeAppHTTPProbers == "" {
		return s, nil
	}
	if err := json.Unmarshal([]byte(config.KubeAppHTTPProbers), &s.appKubeProbers); err != nil {
		return nil, fmt.Errorf("failed to decode app http prober err = %v, json string = %v", err, config.KubeAppHTTPProbers)
	}
	return s, nil
}

// Run opens a the status port and begins accepting probes.
func (s *Server) Run(ctx context.Context) {
	log.Infof("Opening status port %d\n", s.statusPort)

	// Add the handler for ready probes.
	http.HandleFunc(readyPath, s.handleReadyProbe)
	http.HandleFunc("/", s.handleAppProbe)

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

	s.mutex.Lock()
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
	s.mutex.Unlock()
}

func (s *Server) handleAppProbe(w http.ResponseWriter, req *http.Request) {
	appPort := req.Header.Get(IstioAppPortHeader)
	if _, err := strconv.Atoi(appPort); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	httpClient := &http.Client{
		// TODO: figure out the appropriate timeout?
		Timeout: 10 * time.Second,
	}
	path := req.URL.Path
	if !strings.HasPrefix(req.URL.Path, "/") {
		path = "/" + req.URL.Path
	}
	url := fmt.Sprintf("http://127.0.0.1:%s%s", appPort, path)
	appReq, err := http.NewRequest(req.Method, url, req.Body)
	for key, value := range req.Header {
		appReq.Header[key] = value
	}

	if err != nil {
		log.Errorf("Failed to copy request to probe app %v, url %v", err, path)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	response, err := httpClient.Do(appReq)
	if err != nil {
		log.Errorf("Request to probe app failed: %v, url %v", err, path)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	defer response.Body.Close()

	// We only write the status code to the response.
	w.WriteHeader(response.StatusCode)
}
