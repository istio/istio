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
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"istio.io/istio/pilot/cmd/pilot-agent/status/ready"
	"istio.io/istio/pkg/log"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

const (
	// readyPath is for the pilot agent readiness itself.
	readyPath = "/healthz/ready"
	// IstioAppPortHeader is the header name to indicate the app port for health check.
	IstioAppPortHeader = "istio-app-probe-port"
)

var (
	appProberPattern = regexp.MustCompile(`^/app-health/[^\/]+/(livez|readyz)$`)
)

// // KubeAppProbers holds the information about a Kubernetes pod prober.
// type KubeAppProbers struct {
// 	// Probers is the map from the prober URL path to the Kubernetes Prober config.
// 	// For example, "/app-health/hello-world/liveness" entry contains livenss prober config for
// 	// container "hello-world".
// 	Probers map[string]*corev1.HTTPGetAction `json:"probers"`
// }

// KubeAppProbers holds the information about a Kubernetes pod prober.
// It's a map from the prober URL path to the Kubernetes Prober config.
// For example, "/app-health/hello-world/liveness" entry contains livenss prober config for
// container "hello-world".
type KubeAppProbers map[string]*corev1.HTTPGetAction

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
	// Validate the map key conforms the regex pattern.
	for path, prober := range s.appKubeProbers {
		// fmt.Println("path is ", path)
		if !appProberPattern.Match([]byte(path)) {
			return nil, fmt.Errorf("invalid key, must be in form of /app-health/container-name/ready|live")
		}
		if prober.Port.Type != intstr.Int {
			return nil, fmt.Errorf("invalid prober config for %v, the port must be int type", path)
		}
	}
	return s, nil
}

// Run opens a the status port and begins accepting probes.
func (s *Server) Run(ctx context.Context) {
	log.Infof("Opening status port %d\n", s.statusPort)

	// Add the handler for ready probes.
	http.HandleFunc(readyPath, s.handleReadyProbe)
	http.HandleFunc("/", s.handleAppProbe)

	http.HandleFunc("/app-health", s.handleAppProbe)

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
	// Validate the request first.
	path := req.URL.Path
	if !strings.HasPrefix(path, "/") {
		path = "/" + req.URL.Path
	}
	prober, exists := s.appKubeProbers[path]
	if !exists {
		log.Errorf("Prober does not exists url %v", path)
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(fmt.Sprintf("app prober config does not exists for %v", path)))
		return
	}

	// Construct a request sent to the application.
	httpClient := &http.Client{
		// TODO: figure out the appropriate timeout?
		Timeout: 10 * time.Second,
	}
	url := fmt.Sprintf("http://127.0.0.1:%v%s", prober.Port.IntValue(), prober.Path)
	// TODO: should body be restricted?
	appReq, err := http.NewRequest("GET", url, nil)
	if err != nil {
		log.Errorf("Failed to create request to probe app %v, original url %v", err, path)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	for _, header := range prober.HTTPHeaders {
		appReq.Header[header.Name] = []string{header.Value}
	}

	// Send the request.
	response, err := httpClient.Do(appReq)
	if err != nil {
		log.Errorf("Request to probe app failed: %v, original URL path = %v\napp URL path = %v", err, path, prober.Path)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	defer response.Body.Close()

	// We only write the status code to the response.
	w.WriteHeader(response.StatusCode)
}
