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
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"istio.io/istio/pilot/cmd/pilot-agent/status/ready"
	"istio.io/istio/pkg/log"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

const (
	// readyPath is for the pilot agent readiness itself.
	readyPath = "/healthz/ready"
	// KubeAppProberEnvName is the name of the command line flag for pilot agent to pass app prober config.
	// The json encoded string to pass app HTTP probe information from injector(istioctl or webhook).
	// For example, --ISTIO_KUBE_APP_PROBERS='{"/app-health/httpbin/livez":{"path": "/hello", "port": 8080}.
	// indicates that httpbin container liveness prober port is 8080 and probing path is /hello.
	// This environment variable should never be set manually.
	KubeAppProberEnvName = "ISTIO_KUBE_APP_PROBERS"
)

var (
	appProberPattern = regexp.MustCompile(`^/app-health/[^/]+/(livez|readyz)$`)
)

// KubeAppProbers holds the information about a Kubernetes pod prober.
// It's a map from the prober URL path to the Kubernetes Prober config.
// For example, "/app-health/hello-world/livez" entry contains livenss prober config for
// container "hello-world".
type KubeAppProbers map[string]*corev1.HTTPGetAction

// Config for the status server.
type Config struct {
	LocalHostAddr    string
	StatusPort       uint16
	AdminPort        uint16
	ApplicationPorts []uint16
	// KubeAppHTTPProbers is a json with Kubernetes application HTTP prober config encoded.
	KubeAppHTTPProbers string
}

// Server provides an endpoint for handling status probes.
type Server struct {
	ready               *ready.Probe
	mutex               sync.RWMutex
	appKubeProbers      KubeAppProbers
	statusPort          uint16
	lastProbeSuccessful bool
}

// NewServer creates a new status server.
func NewServer(config Config) (*Server, error) {
	s := &Server{
		statusPort: config.StatusPort,
		ready: &ready.Probe{
			LocalHostAddr:    config.LocalHostAddr,
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
	// Validate the map key matching the regex pattern.
	for path, prober := range s.appKubeProbers {
		if !appProberPattern.Match([]byte(path)) {
			return nil, fmt.Errorf(`invalid key, must be in form of regex pattern ^/app-health/[^\/]+/(livez|readyz)$`)
		}
		if prober.Port.Type != intstr.Int {
			return nil, fmt.Errorf("invalid prober config for %v, the port must be int type", path)
		}
	}
	return s, nil
}

// FormatProberURL returns a pair of HTTP URLs that pilot agent will serve to take over Kubernetes
// app probers.
func FormatProberURL(container string) (string, string) {
	return fmt.Sprintf("/app-health/%v/readyz", container),
		fmt.Sprintf("/app-health/%v/livez", container)
}

// Run opens a the status port and begins accepting probes.
func (s *Server) Run(ctx context.Context) {
	log.Infof("Opening status port %d\n", s.statusPort)

	mux := http.NewServeMux()

	// Add the handler for ready probes.
	mux.HandleFunc(readyPath, s.handleReadyProbe)
	mux.HandleFunc("/", s.handleAppProbe)

	mux.HandleFunc("/app-health", s.handleAppProbe)

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
		if err := http.Serve(l, mux); err != nil {
			log.Errora(err)
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
		_, _ = w.Write([]byte(fmt.Sprintf("app prober config does not exists for %v", path)))
		return
	}

	// Construct a request sent to the application.
	httpClient := &http.Client{
		// TODO: figure out the appropriate timeout?
		Timeout: 10 * time.Second,
		// We skip the verification since kubelet skips the verification for HTTPS prober as well
		// https://kubernetes.io/docs/tasks/configure-pod-container/configure-liveness-readiness-probes/#configure-probes
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		},
	}
	var url string
	if prober.Scheme == corev1.URISchemeHTTPS {
		url = fmt.Sprintf("https://127.0.0.1:%v%s", prober.Port.IntValue(), prober.Path)
	} else {
		url = fmt.Sprintf("http://127.0.0.1:%v%s", prober.Port.IntValue(), prober.Path)
	}
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
