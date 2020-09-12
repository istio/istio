// Copyright Istio Authors
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
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"net"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	ocprom "contrib.go.opencensus.io/exporter/prometheus"
	"github.com/hashicorp/go-multierror"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/expfmt"
	"go.opencensus.io/stats/view"

	"istio.io/pkg/env"

	"istio.io/istio/pilot/pkg/model"

	"istio.io/pkg/log"

	"istio.io/istio/pilot/cmd/pilot-agent/status/ready"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

const (
	// readyPath is for the pilot agent readiness itself.
	readyPath = "/healthz/ready"
	// quitPath is to notify the pilot agent to quit.
	quitPath = "/quitquitquit"
	// KubeAppProberEnvName is the name of the command line flag for pilot agent to pass app prober config.
	// The json encoded string to pass app HTTP probe information from injector(istioctl or webhook).
	// For example, ISTIO_KUBE_APP_PROBERS='{"/app-health/httpbin/livez":{"httpGet":{"path": "/hello", "port": 8080}}.
	// indicates that httpbin container liveness prober port is 8080 and probing path is /hello.
	// This environment variable should never be set manually.
	KubeAppProberEnvName = "ISTIO_KUBE_APP_PROBERS"
)

var PrometheusScrapingConfig = env.RegisterStringVar("ISTIO_PROMETHEUS_ANNOTATIONS", "", "")

var (
	appProberPattern = regexp.MustCompile(`^/app-health/[^/]+/(livez|readyz|startupz)$`)

	promRegistry *prometheus.Registry
)

// KubeAppProbers holds the information about a Kubernetes pod prober.
// It's a map from the prober URL path to the Kubernetes Prober config.
// For example, "/app-health/hello-world/livez" entry contains liveness prober config for
// container "hello-world".
type KubeAppProbers map[string]*Prober

// Prober represents a single container prober
type Prober struct {
	HTTPGet        *corev1.HTTPGetAction `json:"httpGet"`
	TimeoutSeconds int32                 `json:"timeoutSeconds,omitempty"`
}

// Config for the status server.
type Config struct {
	LocalHostAddr string
	// KubeAppProbers is a json with Kubernetes application prober config encoded.
	KubeAppProbers string
	NodeType       model.NodeType
	StatusPort     uint16
	AdminPort      uint16
}

// Server provides an endpoint for handling status probes.
type Server struct {
	ready               *ready.Probe
	prometheus          *PrometheusScrapeConfiguration
	mutex               sync.RWMutex
	appKubeProbers      KubeAppProbers
	statusPort          uint16
	lastProbeSuccessful bool
	envoyStatsPort      int
}

func init() {
	registry := prometheus.NewRegistry()
	wrapped := prometheus.WrapRegistererWithPrefix("istio_agent_", prometheus.Registerer(registry))
	wrapped.MustRegister(prometheus.NewProcessCollector(prometheus.ProcessCollectorOpts{}))
	wrapped.MustRegister(prometheus.NewGoCollector())

	promRegistry = registry
	// go collector metrics collide with other metrics.
	exporter, err := ocprom.NewExporter(ocprom.Options{Registry: registry, Registerer: wrapped})
	if err != nil {
		log.Fatalf("could not setup exporter: %v", err)
	}
	view.RegisterExporter(exporter)
}

// NewServer creates a new status server.
func NewServer(config Config) (*Server, error) {
	s := &Server{
		statusPort: config.StatusPort,
		ready: &ready.Probe{
			LocalHostAddr: config.LocalHostAddr,
			AdminPort:     config.AdminPort,
			NodeType:      config.NodeType,
		},
		envoyStatsPort: 15090,
	}
	if config.KubeAppProbers == "" {
		return s, nil
	}
	if err := json.Unmarshal([]byte(config.KubeAppProbers), &s.appKubeProbers); err != nil {
		return nil, fmt.Errorf("failed to decode app prober err = %v, json string = %v", err, config.KubeAppProbers)
	}
	// Validate the map key matching the regex pattern.
	for path, prober := range s.appKubeProbers {
		if !appProberPattern.Match([]byte(path)) {
			return nil, fmt.Errorf(`invalid key, must be in form of regex pattern ^/app-health/[^\/]+/(livez|readyz)$`)
		}
		if prober.HTTPGet == nil {
			return nil, fmt.Errorf(`invalid prober type, must be of type httpGet`)
		}
		if prober.HTTPGet.Port.Type != intstr.Int {
			return nil, fmt.Errorf("invalid prober config for %v, the port must be int type", path)
		}
	}

	// Enable prometheus server if its configured and a sidecar
	// Because port 15020 is exposed in the gateway Services, we cannot safely serve this endpoint
	// If we need to do this in the future, we should use envoy to do routing or have another port to make this internal
	// only. For now, its not needed for gateway, as we can just get Envoy stats directly, but if we
	// want to expose istio-agent metrics we may want to revisit this.
	if cfg, f := PrometheusScrapingConfig.Lookup(); config.NodeType == model.SidecarProxy && f {
		var prom PrometheusScrapeConfiguration
		if err := json.Unmarshal([]byte(cfg), &prom); err != nil {
			return nil, fmt.Errorf("failed to unmarshal %s: %v", PrometheusScrapingConfig.Name, err)
		}
		log.Infof("Prometheus scraping configuration: %v", prom)
		s.prometheus = &prom
		if s.prometheus.Path == "" {
			s.prometheus.Path = "/metrics"
		}
		if s.prometheus.Port == "" {
			s.prometheus.Port = "80"
		}
		if s.prometheus.Port == strconv.Itoa(int(config.StatusPort)) {
			return nil, fmt.Errorf("invalid prometheus scrape configuration: "+
				"application port is the same as agent port, which may lead to a recursive loop. "+
				"Ensure pod does not have prometheus.io/port=%d label, or that injection is not happening multiple times", config.StatusPort)
		}
	}

	return s, nil
}

// FormatProberURL returns a set of HTTP URLs that pilot agent will serve to take over Kubernetes
// app probers.
func FormatProberURL(container string) (string, string, string) {
	return fmt.Sprintf("/app-health/%v/readyz", container),
		fmt.Sprintf("/app-health/%v/livez", container),
		fmt.Sprintf("/app-health/%v/startupz", container)
}

// Run opens a the status port and begins accepting probes.
func (s *Server) Run(ctx context.Context) {
	log.Infof("Opening status port %d\n", s.statusPort)

	mux := http.NewServeMux()

	// Add the handler for ready probes.
	mux.HandleFunc(readyPath, s.handleReadyProbe)
	mux.HandleFunc(`/stats/prometheus`, s.handleStats)
	mux.HandleFunc(quitPath, s.handleQuit)
	mux.HandleFunc("/app-health/", s.handleAppProbe)

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
			select {
			case <-ctx.Done():
				// We are shutting down already, don't trigger SIGTERM
				return
			default:
				// If the server errors then pilot-agent can never pass readiness or liveness probes
				// Therefore, trigger graceful termination by sending SIGTERM to the binary pid
				notifyExit()
			}
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

		log.Warnf("Envoy proxy is NOT ready: %s", err.Error())
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

func isRequestFromLocalhost(r *http.Request) bool {
	ip, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return false
	}

	userIP := net.ParseIP(ip)
	return userIP.IsLoopback()
}

type PrometheusScrapeConfiguration struct {
	Scrape string `json:"scrape"`
	Path   string `json:"path"`
	Port   string `json:"port"`
}

// handleStats handles prometheus stats scraping. This will scrape envoy metrics, and, if configured,
// the application metrics and merge them together.
// The merge here is a simple string concatenation. This works for almost all cases, assuming the application
// is not exposing the same metrics as Envoy.
// Note that we do not return any errors here. If we do, we will drop metrics. For example, the app may be having issues,
// but we still want Envoy metrics. Instead, errors are tracked in the failed scrape metrics/logs.
func (s *Server) handleStats(w http.ResponseWriter, r *http.Request) {
	scrapeTotals.Increment()
	var envoy, application, agent []byte
	var err error
	// Gather all the metrics we will merge
	if envoy, err = s.scrape(fmt.Sprintf("http://localhost:%d/stats/prometheus", s.envoyStatsPort), r.Header); err != nil {
		log.Errorf("failed scraping envoy metrics: %v", err)
		envoyScrapeErrors.Increment()
	}
	if s.prometheus != nil {
		url := fmt.Sprintf("http://localhost:%s%s", s.prometheus.Port, s.prometheus.Path)
		if application, err = s.scrape(url, r.Header); err != nil {
			log.Errorf("failed scraping application metrics: %v", err)
			appScrapeErrors.Increment()
		}
	}
	if agent, err = scrapeAgentMetrics(); err != nil {
		log.Errorf("failed scraping agent metrics: %v", err)
		agentScrapeErrors.Increment()
	}

	// Write out the metrics
	if _, err := w.Write(envoy); err != nil {
		log.Errorf("failed to write envoy metrics: %v", err)
		envoyScrapeErrors.Increment()
	}
	if _, err := w.Write(application); err != nil {
		log.Errorf("failed to write application metrics: %v", err)
		appScrapeErrors.Increment()
	}
	if _, err := w.Write(agent); err != nil {
		log.Errorf("failed to write agent metrics: %v", err)
		agentScrapeErrors.Increment()
	}
}

func scrapeAgentMetrics() ([]byte, error) {
	buf := &bytes.Buffer{}
	mfs, err := promRegistry.Gather()
	enc := expfmt.NewEncoder(buf, expfmt.FmtText)
	if err != nil {
		return nil, err
	}
	var errs error
	for _, mf := range mfs {
		if err := enc.Encode(mf); err != nil {
			errs = multierror.Append(errs, err)
		}
	}
	return buf.Bytes(), errs
}

func applyHeaders(into http.Header, from http.Header, keys ...string) {
	for _, key := range keys {
		val := from.Get(key)
		if val != "" {
			into.Set(key, val)
		}
	}
}

// getHeaderTimeout parse a string like (1.234) representing number of seconds
func getHeaderTimeout(timeout string) (time.Duration, error) {
	timeoutSeconds, err := strconv.ParseFloat(timeout, 64)
	if err != nil {
		return 0 * time.Second, err
	}

	return time.Duration(timeoutSeconds * 1e9), nil
}

// scrape will send a request to the provided url to scrape metrics from
// This will attempt to mimic some of Prometheus functionality by passing some of the headers through
// such as timeout and user agent
func (s *Server) scrape(url string, header http.Header) ([]byte, error) {
	ctx := context.Background()
	if timeoutString := header.Get("X-Prometheus-Scrape-Timeout-Seconds"); timeoutString != "" {
		timeout, err := getHeaderTimeout(timeoutString)
		if err != nil {
			log.Warnf("Failed to parse timeout header %v: %v", timeoutString, err)
		} else {
			c, cancel := context.WithTimeout(ctx, timeout)
			ctx = c
			defer cancel()
		}
	}

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}
	applyHeaders(req.Header, header, "Accept",
		"User-Agent",
		"X-Prometheus-Scrape-Timeout-Seconds",
	)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("error scraping %s: %v", url, err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("error scraping %s, status code: %v", url, resp.StatusCode)
	}
	defer resp.Body.Close()
	metrics, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("error reading %s: %v", url, err)
	}

	return metrics, nil
}

func (s *Server) handleQuit(w http.ResponseWriter, r *http.Request) {
	if !isRequestFromLocalhost(r) {
		http.Error(w, "Only requests from localhost are allowed", http.StatusForbidden)
		return
	}
	if r.Method != "POST" {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("OK"))
	log.Infof("handling %s, notifying pilot-agent to exit", quitPath)
	notifyExit()
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
		Timeout: time.Duration(prober.TimeoutSeconds) * time.Second,
		// We skip the verification since kubelet skips the verification for HTTPS prober as well
		// https://kubernetes.io/docs/tasks/configure-pod-container/configure-liveness-readiness-probes/#configure-probes
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		},
	}
	proberPath := prober.HTTPGet.Path
	if !strings.HasPrefix(proberPath, "/") {
		proberPath = "/" + proberPath
	}
	var url string
	if prober.HTTPGet.Scheme == corev1.URISchemeHTTPS {
		url = fmt.Sprintf("https://localhost:%v%s", prober.HTTPGet.Port.IntValue(), proberPath)
	} else {
		url = fmt.Sprintf("http://localhost:%v%s", prober.HTTPGet.Port.IntValue(), proberPath)
	}
	appReq, err := http.NewRequest("GET", url, nil)
	if err != nil {
		log.Errorf("Failed to create request to probe app %v, original url %v", err, path)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	// Forward incoming headers to the application.
	for name, values := range req.Header {
		newValues := make([]string, len(values))
		copy(newValues, values)
		appReq.Header[name] = newValues
	}

	for _, h := range prober.HTTPGet.HTTPHeaders {
		if h.Name == "Host" || h.Name == ":authority" {
			// Probe has specific host header override; honor it
			appReq.Host = h.Value
		} else {
			appReq.Header.Add(h.Name, h.Value)
		}
	}

	// Send the request.
	response, err := httpClient.Do(appReq)
	if err != nil {
		log.Errorf("Request to probe app failed: %v, original URL path = %v\napp URL path = %v", err, path, proberPath)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	defer func() {
		// Drain and close the body to let the Transport reuse the connection
		_, _ = io.Copy(ioutil.Discard, response.Body)
		_ = response.Body.Close()
	}()

	// We only write the status code to the response.
	w.WriteHeader(response.StatusCode)
}

// notifyExit sends SIGTERM to itself
func notifyExit() {
	p, err := os.FindProcess(os.Getpid())
	if err != nil {
		log.Errora(err)
	}
	if err := p.Signal(syscall.SIGTERM); err != nil {
		log.Errorf("failed to send SIGTERM to self: %v", err)
	}
}
