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
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/http/pprof"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/klauspost/compress/gzhttp"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	dto "github.com/prometheus/client_model/go"
	"github.com/prometheus/common/expfmt"
	"golang.org/x/net/http2"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	grpcHealth "google.golang.org/grpc/health/grpc_health_v1"
	grpcStatus "google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	k8sUtilIo "k8s.io/utils/io"

	"istio.io/istio/pilot/cmd/pilot-agent/metrics"
	"istio.io/istio/pilot/cmd/pilot-agent/status/grpcready"
	"istio.io/istio/pilot/cmd/pilot-agent/status/ready"
	"istio.io/istio/pilot/pkg/features"
	dnsProto "istio.io/istio/pkg/dns/proto"
	"istio.io/istio/pkg/env"
	commonFeatures "istio.io/istio/pkg/features"
	"istio.io/istio/pkg/kube/apimirror"
	"istio.io/istio/pkg/log"
	"istio.io/istio/pkg/model"
	"istio.io/istio/pkg/monitoring"
	"istio.io/istio/pkg/network"
	"istio.io/istio/pkg/slices"
	istioNetUtil "istio.io/istio/pkg/util/net"
	"istio.io/istio/pkg/util/protomarshal"
)

const (
	// readyPath is for the pilot agent readiness itself.
	readyPath = "/healthz/ready"
	// quitPath is to notify the pilot agent to quit.
	quitPath  = "/quitquitquit"
	drainPath = "/drain"
	// KubeAppProberEnvName is the name of the command line flag for pilot agent to pass app prober config.
	// The json encoded string to pass app HTTP probe information from injector(istioctl or webhook).
	// For example, ISTIO_KUBE_APP_PROBERS='{"/app-health/httpbin/livez":{"httpGet":{"path": "/hello", "port": 8080}}.
	// indicates that httpbin container liveness prober port is 8080 and probing path is /hello.
	// This environment variable should never be set manually.
	KubeAppProberEnvName = "ISTIO_KUBE_APP_PROBERS"

	localHostIPv4     = "127.0.0.1"
	localHostIPv6     = "::1"
	maxRespBodyLength = 10 * 1 << 10
)

// probes is a logging scope for probe responses. Since these logs are about the application, not Istio, users may need
// to configure them to meet their preferences.
var probes = log.RegisterScope("probes", "Status of forwarded application probes")

var (
	UpstreamLocalAddressIPv4 = &net.TCPAddr{IP: net.ParseIP("127.0.0.6")}
	UpstreamLocalAddressIPv6 = &net.TCPAddr{IP: net.ParseIP("::6")}
)

var PrometheusScrapingConfig = env.Register("ISTIO_PROMETHEUS_ANNOTATIONS", "", "")

var (
	appProberPattern = regexp.MustCompile(`^/(app-health|app-lifecycle)/[^/]+/(livez|readyz|startupz|prestopz|poststartz)$`)

	EnableHTTP2Probing = env.Register("ISTIO_ENABLE_HTTP2_PROBING", true,
		"If enabled, HTTP2 probes will be enabled for HTTPS probes, following Kubernetes").Get()

	LegacyLocalhostProbeDestination = env.Register("REWRITE_PROBE_LEGACY_LOCALHOST_DESTINATION", false,
		"If enabled, readiness probes will be sent to 'localhost'. Otherwise, they will be sent to the Pod's IP, matching Kubernetes' behavior.")

	ProbeKeepaliveConnections = env.Register("ENABLE_PROBE_KEEPALIVE_CONNECTIONS", false,
		"If enabled, readiness probes will keep the connection from pilot-agent to the application alive. "+
			"This mirrors older Istio versions' behaviors, but not kubelet's.").Get()
)

// KubeAppProbers holds the information about a Kubernetes pod prober.
// It's a map from the prober URL path to the Kubernetes Prober config.
// For example, "/app-health/hello-world/livez" entry contains liveness prober config for
// container "hello-world".
type KubeAppProbers map[string]*Prober

// Prober represents a single container prober
type Prober struct {
	HTTPGet        *apimirror.HTTPGetAction   `json:"httpGet,omitempty"`
	TCPSocket      *apimirror.TCPSocketAction `json:"tcpSocket,omitempty"`
	GRPC           *apimirror.GRPCAction      `json:"grpc,omitempty"`
	TimeoutSeconds int32                      `json:"timeoutSeconds,omitempty"`
}

// Options for the status server.
type Options struct {
	// Ip of the pod. Note: this is only applicable for Kubernetes pods and should only be used for
	// the prober.
	PodIP string
	// KubeAppProbers is a json with Kubernetes application prober config encoded.
	KubeAppProbers      string
	NodeType            model.NodeType
	StatusPort          uint16
	AdminPort           uint16
	IPv6                bool
	Probes              []ready.Prober
	EnvoyPrometheusPort int
	Context             context.Context
	FetchDNS            func() *dnsProto.NameTable
	NoEnvoy             bool
	GRPCBootstrap       string
	EnableProfiling     bool
	// PrometheusRegistry to use. Just for testing.
	PrometheusRegistry prometheus.Gatherer
	Shutdown           context.CancelCauseFunc
	TriggerDrain       func()
	DisableDrain       func()
}

// Server provides an endpoint for handling status probes.
type Server struct {
	ready                 []ready.Prober
	prometheus            *PrometheusScrapeConfiguration
	mutex                 sync.RWMutex
	appProbersDestination string
	appKubeProbers        KubeAppProbers
	appProbeClient        map[string]*http.Client
	statusPort            uint16
	lastProbeSuccessful   bool
	envoyStatsPort        int
	fetchDNS              func() *dnsProto.NameTable
	upstreamLocalAddress  *net.TCPAddr
	config                Options
	http                  *http.Client
	enableProfiling       bool
	registry              prometheus.Gatherer
	shutdown              context.CancelCauseFunc
	drain                 func()
	disableDrain          func()

	// maxAppBodyBytes caps the per-target app metrics body to bound agent memory
	// across N concurrent scrape targets. Reads above the cap are dropped as failures
	// and incremented on AppScrapeErrors. Defaults to defaultMaxAppMetricsBodyBytes;
	// tests may override via the struct literal.
	maxAppBodyBytes int
}

const defaultMaxAppMetricsBodyBytes = 10 * 1024 * 1024 // 10 MiB

func initializeMonitoring() (prometheus.Gatherer, error) {
	registry := prometheus.NewRegistry()
	wrapped := prometheus.WrapRegistererWithPrefix("istio_agent_", registry)
	wrapped.MustRegister(collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))
	wrapped.MustRegister(collectors.NewGoCollector())

	_, err := monitoring.RegisterPrometheusExporter(wrapped, registry)
	if err != nil {
		return nil, fmt.Errorf("could not setup exporter: %v", err)
	}
	return registry, nil
}

// NewServer creates a new status server.
func NewServer(config Options) (*Server, error) {
	localhost := localHostIPv4
	upstreamLocalAddress := UpstreamLocalAddressIPv4
	if config.IPv6 {
		localhost = localHostIPv6
		upstreamLocalAddress = UpstreamLocalAddressIPv6
	} else {
		// if not ipv6-only, it can be ipv4-only or dual-stack
		// let InstanceIP decide the localhost
		netIP := net.ParseIP(config.PodIP)
		if netIP.To4() == nil && netIP.To16() != nil && !netIP.IsLinkLocalUnicast() {
			localhost = localHostIPv6
			upstreamLocalAddress = UpstreamLocalAddressIPv6
		}
	}
	probes := make([]ready.Prober, 0)
	if !config.NoEnvoy {
		probes = append(probes, &ready.Probe{
			LocalHostAddr: localhost,
			AdminPort:     config.AdminPort,
			Context:       config.Context,
			NoEnvoy:       config.NoEnvoy,
		})
	}

	if config.GRPCBootstrap != "" {
		probes = append(probes, grpcready.NewProbe(config.GRPCBootstrap))
	}

	probes = append(probes, config.Probes...)
	registry := config.PrometheusRegistry
	if registry == nil {
		var err error
		registry, err = initializeMonitoring()
		if err != nil {
			return nil, err
		}
	}
	s := &Server{
		statusPort:            config.StatusPort,
		ready:                 probes,
		http:                  &http.Client{},
		appProbersDestination: config.PodIP,
		envoyStatsPort:        config.EnvoyPrometheusPort,
		fetchDNS:              config.FetchDNS,
		upstreamLocalAddress:  upstreamLocalAddress,
		config:                config,
		enableProfiling:       config.EnableProfiling,
		registry:              registry,
		shutdown:              config.Shutdown,
		drain:                 config.TriggerDrain,
		disableDrain:          config.DisableDrain,
		maxAppBodyBytes:       defaultMaxAppMetricsBodyBytes,
	}
	if LegacyLocalhostProbeDestination.Get() {
		s.appProbersDestination = "localhost"
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
		if prom.Scrape != "false" {
			s.prometheus = &prom
			if len(s.prometheus.Targets) == 0 {
				// Legacy path: apply defaults and synthesize a single Targets entry.
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
				s.prometheus.Targets = []ScrapeTarget{{Port: s.prometheus.Port, Path: s.prometheus.Path}}
			} else {
				// New-format path: populate legacy Port/Path from Targets[0] if absent so that
				// handleStats (which reads Port/Path directly) scrapes the primary endpoint correctly.
				if s.prometheus.Port == "" {
					s.prometheus.Port = s.prometheus.Targets[0].Port
				}
				if s.prometheus.Path == "" {
					s.prometheus.Path = s.prometheus.Targets[0].Path
				}
			}
			// Default path and validate every target port numerically to catch leading-zero
			// representations (e.g. "015020" dials 15020 at runtime but != "15020" as a string).
			for i, t := range s.prometheus.Targets {
				if t.Path == "" {
					s.prometheus.Targets[i].Path = "/metrics"
				}
				portNum, atoiErr := strconv.Atoi(t.Port)
				if atoiErr != nil || portNum < 1 || portNum > 65535 {
					return nil, fmt.Errorf("invalid prometheus scrape configuration: "+
						"invalid target port %q", t.Port)
				}
				if portNum == int(config.StatusPort) {
					return nil, fmt.Errorf("invalid prometheus scrape configuration: "+
						"target port %s is the same as agent port, which may lead to a recursive loop", t.Port)
				}
				if reason, reserved := IstioReservedPortReason(strconv.Itoa(portNum)); reserved {
					return nil, fmt.Errorf("invalid prometheus scrape configuration: "+
						"target port %s is reserved for Istio (%s) and cannot be scraped", t.Port, reason)
				}
			}
		}
	}

	if config.KubeAppProbers == "" {
		return s, nil
	}
	if err := json.Unmarshal([]byte(config.KubeAppProbers), &s.appKubeProbers); err != nil {
		return nil, fmt.Errorf("failed to decode app prober err = %v, json string = %v", err, config.KubeAppProbers)
	}

	s.appProbeClient = make(map[string]*http.Client, len(s.appKubeProbers))
	// Validate the map key matching the regex pattern.
	for path, prober := range s.appKubeProbers {
		err := validateAppKubeProber(path, prober)
		if err != nil {
			return nil, err
		}
		if prober.HTTPGet != nil {
			d := ProbeDialer()
			d.LocalAddr = s.upstreamLocalAddress
			// nolint: gosec
			// This is matching Kubernetes. It is a reasonable usage of this, as it is just a health check over localhost.
			transport, err := setTransportDefaults(&http.Transport{
				TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
				DialContext:     d.DialContext,
				// https://github.com/kubernetes/kubernetes/blob/0153febd9f0098d4b8d0d484927710eaf899ef40/pkg/probe/http/http.go#L55
				// Match Kubernetes logic. This also ensures idle timeouts do not trigger probe failures
				DisableKeepAlives: !ProbeKeepaliveConnections,
			})
			if err != nil {
				return nil, err
			}
			// Construct a http client and cache it in order to reuse the connection.
			s.appProbeClient[path] = &http.Client{
				Timeout: time.Duration(prober.TimeoutSeconds) * time.Second,
				// We skip the verification since kubelet skips the verification for HTTPS prober as well
				// https://kubernetes.io/docs/tasks/configure-pod-container/configure-liveness-readiness-probes/#configure-probes
				Transport:     transport,
				CheckRedirect: redirectChecker(),
			}
		}
	}

	return s, nil
}

// Copies logic from https://github.com/kubernetes/kubernetes/blob/b152001f459/pkg/probe/http/http.go#L129-L130
func isRedirect(code int) bool {
	return code >= http.StatusMultipleChoices && code < http.StatusBadRequest
}

// Using the same redirect logic that kubelet does: https://github.com/kubernetes/kubernetes/blob/b152001f459/pkg/probe/http/http.go#L141
// This means that:
// * If we exceed 10 redirects, the probe fails
// * If we redirect somewhere external, the probe succeeds (https://github.com/kubernetes/kubernetes/blob/b152001f459/pkg/probe/http/http.go#L130)
// * If we redirect to the same address, the probe will follow the redirect
func redirectChecker() func(*http.Request, []*http.Request) error {
	return func(req *http.Request, via []*http.Request) error {
		if req.URL.Hostname() != via[0].URL.Hostname() {
			return http.ErrUseLastResponse
		}
		// Default behavior: stop after 10 redirects.
		if len(via) >= 10 {
			return errors.New("stopped after 10 redirects")
		}
		return nil
	}
}

func validateAppKubeProber(path string, prober *Prober) error {
	if !appProberPattern.MatchString(path) {
		return fmt.Errorf(`invalid path, must be in form of regex pattern %v`, appProberPattern)
	}
	count := 0
	if prober.HTTPGet != nil {
		count++
	}
	if prober.TCPSocket != nil {
		count++
	}
	if prober.GRPC != nil {
		count++
	}
	if count != 1 {
		return fmt.Errorf(`invalid prober type, must be one of type httpGet, tcpSocket or gRPC`)
	}
	if prober.HTTPGet != nil && prober.HTTPGet.Port.Type != apimirror.Int {
		return fmt.Errorf("invalid prober config for %v, the port must be int type", path)
	}
	if prober.TCPSocket != nil && prober.TCPSocket.Port.Type != apimirror.Int {
		return fmt.Errorf("invalid prober config for %v, the port must be int type", path)
	}
	return nil
}

// FormatProberURL returns a set of HTTP URLs that pilot agent will serve to take over Kubernetes
// app probers.
func FormatProberURL(container string) (string, string, string, string, string) {
	return fmt.Sprintf("/app-health/%v/readyz", container),
		fmt.Sprintf("/app-health/%v/livez", container),
		fmt.Sprintf("/app-health/%v/startupz", container),
		fmt.Sprintf("/app-lifecycle/%v/prestopz", container),
		fmt.Sprintf("/app-lifecycle/%v/poststartz", container)
}

// Run opens the status port and begins accepting probes.
func (s *Server) Run(ctx context.Context) {
	log.Infof("Opening status port %d", s.statusPort)

	mux := http.NewServeMux()

	// Add the handler for ready probes.
	mux.HandleFunc(readyPath, s.handleReadyProbe)
	// Default path for prom
	mux.HandleFunc(`/metrics`, s.handleStats)
	// Envoy uses something else - and original agent used the same.
	// Keep for backward compat with configs.
	mux.HandleFunc(`/stats/prometheus`, s.handleStats)
	mux.HandleFunc(quitPath, s.handleQuit)
	mux.HandleFunc(drainPath, s.handleDrain)
	mux.HandleFunc("/app-health/", s.handleAppProbe)
	mux.HandleFunc("/app-lifecycle/", s.handleAppProbe)

	if s.enableProfiling {
		// Add the handler for pprof.
		mux.HandleFunc("/debug/pprof/", s.handlePprofIndex)
		mux.HandleFunc("/debug/pprof/cmdline", s.handlePprofCmdline)
		mux.HandleFunc("/debug/pprof/profile", s.handlePprofProfile)
		mux.HandleFunc("/debug/pprof/symbol", s.handlePprofSymbol)
		mux.HandleFunc("/debug/pprof/trace", s.handlePprofTrace)
	}
	mux.HandleFunc("/debug/ndsz", s.handleNdsz)

	l, err := net.Listen("tcp", fmt.Sprintf(":%d", s.statusPort))
	if err != nil {
		log.Errorf("Error listening on status port: %v", err.Error())
		return
	}
	// for testing.
	if s.statusPort == 0 {
		_, hostPort, _ := net.SplitHostPort(l.Addr().String())
		allocatedPort, _ := strconv.Atoi(hostPort)
		s.mutex.Lock()
		s.statusPort = uint16(allocatedPort)
		s.mutex.Unlock()
	}
	defer l.Close()

	go func() {
		if err := http.Serve(l, gzhttp.GzipHandler(mux)); err != nil {
			if network.IsUnexpectedListenerError(err) {
				log.Error(err)
			}
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

func (s *Server) handlePprofIndex(w http.ResponseWriter, r *http.Request) {
	if !istioNetUtil.IsRequestFromLocalhost(r) {
		http.Error(w, "Only requests from localhost are allowed", http.StatusForbidden)
		return
	}

	pprof.Index(w, r)
}

func (s *Server) handlePprofCmdline(w http.ResponseWriter, r *http.Request) {
	if !istioNetUtil.IsRequestFromLocalhost(r) {
		http.Error(w, "Only requests from localhost are allowed", http.StatusForbidden)
		return
	}

	pprof.Cmdline(w, r)
}

func (s *Server) handlePprofSymbol(w http.ResponseWriter, r *http.Request) {
	if !istioNetUtil.IsRequestFromLocalhost(r) {
		http.Error(w, "Only requests from localhost are allowed", http.StatusForbidden)
		return
	}

	pprof.Symbol(w, r)
}

func (s *Server) handlePprofProfile(w http.ResponseWriter, r *http.Request) {
	if !istioNetUtil.IsRequestFromLocalhost(r) {
		http.Error(w, "Only requests from localhost are allowed", http.StatusForbidden)
		return
	}

	pprof.Profile(w, r)
}

func (s *Server) handlePprofTrace(w http.ResponseWriter, r *http.Request) {
	if !istioNetUtil.IsRequestFromLocalhost(r) {
		http.Error(w, "Only requests from localhost are allowed", http.StatusForbidden)
		return
	}

	pprof.Trace(w, r)
}

func (s *Server) handleReadyProbe(w http.ResponseWriter, _ *http.Request) {
	err := s.isReady()
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

func (s *Server) isReady() error {
	for _, p := range s.ready {
		if err := p.Check(); err != nil {
			return err
		}
	}
	return nil
}

// ScrapeTarget represents a single application metrics endpoint to scrape.
type ScrapeTarget struct {
	Port string `json:"port"`
	Path string `json:"path"`
}

type PrometheusScrapeConfiguration struct {
	Scrape  string         `json:"scrape"`
	Path    string         `json:"path"`
	Port    string         `json:"port"`
	Targets []ScrapeTarget `json:"targets,omitempty"`
}

// IsEmpty reports whether no scrape configuration is set.
func (p PrometheusScrapeConfiguration) IsEmpty() bool {
	return p.Scrape == "" && p.Path == "" && p.Port == "" && len(p.Targets) == 0
}

// istioReservedPorts are data-plane ports the Istio sidecar/agent owns. Users must
// not point metrics-merging scrape targets at any of these: they either hit a
// non-application surface (admin, tunnel, traffic capture, DNS) or would create a
// recursive / double-counted scrape (15020 is the merged endpoint itself; 15090 is
// Envoy's raw Prom output, already merged).
var istioReservedPorts = map[string]string{
	"15000": "Envoy admin",
	"15001": "outbound traffic capture",
	"15004": "pilot debug",
	"15006": "inbound traffic capture",
	"15008": "HBONE tunnel",
	"15020": "pilot-agent status / merged Prometheus",
	"15021": "Envoy health",
	"15053": "DNS proxy",
	"15090": "Envoy Prometheus (already merged)",
}

// IstioReservedPortReason returns the human-readable reason port is reserved by the
// Istio data plane, along with whether it is reserved. Callers should surface the
// reason to users; the error message is more actionable when users see *why*.
func IstioReservedPortReason(port string) (string, bool) {
	reason, ok := istioReservedPorts[port]
	return reason, ok
}

// ParseScrapeTargets parses a comma-separated list of "port:path" pairs into ScrapeTargets.
// Whitespace is trimmed from each entry. An empty path defaults to "/metrics".
// Returns an error if any entry has an empty port.
func ParseScrapeTargets(raw string) ([]ScrapeTarget, error) {
	var targets []ScrapeTarget
	for _, entry := range strings.Split(raw, ",") {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		parts := strings.SplitN(entry, ":", 2)
		port := strings.TrimSpace(parts[0])
		if port == "" {
			return nil, fmt.Errorf("empty port in scrape target %q", entry)
		}
		path := "/metrics"
		if len(parts) == 2 && strings.TrimSpace(parts[1]) != "" {
			path = strings.TrimSpace(parts[1])
		}
		targets = append(targets, ScrapeTarget{Port: port, Path: path})
	}
	return targets, nil
}

// handleStats handles prometheus stats scraping. This will scrape envoy metrics, and, if configured,
// the application metrics and merge them together.
// The merge here is a simple string concatenation. This works for almost all cases, assuming the application
// is not exposing the same metrics as Envoy.
// This merging works for both FmtText and FmtOpenMetrics and will use the format of the application metrics
// Note that we do not return any errors here. If we do, we will drop metrics. For example, the app may be having issues,
// but we still want Envoy metrics. Instead, errors are tracked in the failed scrape metrics/logs.
// handleStats handles prometheus stats scraping. This will scrape envoy metrics, and, if configured,
// the application metrics and merge them together.
// The merge here is a simple string concatenation for the all-text and all-OpenMetrics cases.
// This works for almost all cases, assuming the application is not exposing the same metrics as Envoy.
// When at least one upstream advertises the delimited Prometheus protobuf format
// (application/vnd.google.protobuf; proto=io.prometheus.client.MetricFamily; encoding=delimited
// — required for native Prometheus histograms), the merger switches to a parse-and-re-encode
// path that decodes each upstream into MetricFamily values, gathers agent self-metrics, and
// re-emits the merged stream in the negotiated format. Mixed formats (e.g. one proto upstream
// and one text upstream) downgrade to text — converting text to proto would require the
// CPU/memory cost-comparison study mandated by the Istio TOC and is deferred to a follow-up.
// Note that we do not return any errors here. If we do, we will drop metrics. For example, the app may be having issues,
// but we still want Envoy metrics. Instead, errors are tracked in the failed scrape metrics/logs.
func (s *Server) handleStats(w http.ResponseWriter, r *http.Request) {
	if commonFeatures.MetricsLocalhostAccessOnly && !istioNetUtil.IsRequestFromLocalhost(r) {
		http.Error(w, "Only requests from localhost are allowed", http.StatusForbidden)
		return
	}
	metrics.ScrapeTotals.Increment()
	var err error
	var envoy, application io.ReadCloser
	var envoyCancel, appCancel context.CancelFunc
	defer func() {
		if envoy != nil {
			err = envoy.Close()
			if err != nil {
				log.Infof("envoy connection is not closed: %v", err)
			}
		}
		if application != nil {
			err = application.Close()
			if err != nil {
				log.Infof("app connection is not closed: %v", err)
			}
		}
		if envoyCancel != nil {
			envoyCancel()
		}
		if appCancel != nil {
			appCancel()
		}
	}()

	// Gather all the metrics we will merge.
	var envoyFormat expfmt.Format
	if !s.config.NoEnvoy && features.AgentMergeEnvoyStats {
		scrapeURL := fmt.Sprintf("http://localhost:%d/stats/prometheus", s.envoyStatsPort)
		if r.URL != nil && len(r.URL.RawQuery) > 0 {
			scrapeURL = fmt.Sprintf("%s?%s", scrapeURL, r.URL.RawQuery)
		}
		var envoyCT string
		if envoy, envoyCancel, envoyCT, err = s.scrape(scrapeURL, r.Header); err != nil {
			log.Errorf("failed scraping envoy metrics: %v", err)
			metrics.EnvoyScrapeErrors.Increment()
		} else {
			envoyFormat = negotiateMetricsFormat(envoyCT)
		}
	}

	// Scrape app metrics. Multi-target buffers each response so we can rewrite the
	// OpenMetrics "# EOF" trailer; single-target streams via io.Copy.
	var appFormat expfmt.Format
	var appBodies [][]byte
	// appBodyFormats is the per-body negotiated format for the multi-target case.
	// When targets disagree (mixed proto/text), scrapeMultipleApps aggregates
	// `appFormat` to FmtText, but the underlying body bytes are still in their
	// original Content-Types. The proto path needs the per-body formats so a
	// binary proto body is not text-parsed (which would silently drop the target).
	var appBodyFormats []expfmt.Format
	if s.prometheus != nil {
		if len(s.prometheus.Targets) > 1 {
			appFormat, appBodies, appBodyFormats = s.scrapeMultipleApps(r)
		} else {
			var contentType string
			if application, appCancel, contentType, err = s.scrape(appMetricsURL(s.prometheus.Port, s.prometheus.Path), r.Header); err != nil {
				log.Errorf("failed scraping application metrics: %v", err)
				metrics.AppScrapeErrors.Increment()
			} else {
				appFormat = negotiateMetricsFormat(contentType)
			}
		}
	}

	// Compute the negotiated output format.
	//
	//   1. If any upstream returns anything other than text/plain (i.e. proto or
	//      OpenMetrics), we must parse-and-re-encode the merged stream. The
	//      byte-concatenation fast path would otherwise corrupt the stream:
	//      proto bodies can't be byte-merged with text, and an OpenMetrics body's
	//      trailing "# EOF" would end up mid-stream when other sources are
	//      appended after it. mergeFormat decides the output format and downgrades
	//      to text on disagreement.
	//
	//   2. If every upstream is text/plain, the historical byte-concat behavior
	//      is wire-correct and we preserve it for performance: the app's format
	//      wins (matching the prior implementation that discarded the Envoy
	//      content-type in single-target mode). When there is no app at all,
	//      we default to FmtText.
	//
	// Behavior change vs prior istio releases: clients that previously requested
	// protobuf and were force-downgraded to text by istio#60622 (because the
	// merger could not safely combine proto with text) will now actually receive
	// protobuf. For clients that did not request protobuf, the output is
	// unchanged.
	// Take the parse-and-re-encode merger path when either:
	//   (a) any upstream advertised the delimited Prometheus protobuf format
	//       (the byte-concat fast path cannot merge binary proto with text); or
	//   (b) Envoy returns a non-text/plain body (e.g. OpenMetrics). The
	//       byte-concat fast path writes Envoy bytes mid-stream, before app
	//       bodies, so an OM "# EOF" in the Envoy body would land mid-stream
	//       and corrupt the response. Today Envoy never serves OpenMetrics —
	//       it only supports text/plain and proto — but the check defends
	//       against any future Envoy format support without further code
	//       changes here.
	//
	// All-text-plain and all-OpenMetrics-via-apps cases keep the byte-concat
	// fast path (the multi-target loop already strips + reappends a single
	// "# EOF" trailer; the single-target app body is the last thing written
	// in this branch, so its own "# EOF" terminates the response correctly).
	envoyNonText := envoyFormat != "" && envoyFormat.FormatType() != expfmt.TypeTextPlain
	anyProto := envoyFormat.FormatType() == expfmt.TypeProtoDelim ||
		appFormat.FormatType() == expfmt.TypeProtoDelim
	needsParseMerge := anyProto || envoyNonText

	var outFormat expfmt.Format
	if needsParseMerge {
		outFormat = mergeFormat(envoyFormat, appFormat)
	} else {
		outFormat = appFormat
		if outFormat == "" {
			outFormat = FmtText
		}
	}

	w.Header().Set("Content-Type", string(outFormat))

	// Record the delivered format. A downgrade is "any upstream offered proto but we
	// emitted text because another upstream did not". This is the metric a future
	// PR3 (text→proto upconversion behind a feature gate) will optimize against.
	switch {
	case anyProto && outFormat.FormatType() != expfmt.TypeProtoDelim:
		metrics.ScrapeFormatDowngraded.Increment()
	default:
		switch scrapeFormatLabel(outFormat) {
		case metrics.ScrapeFormatLabelProto:
			metrics.ScrapeFormatProto.Increment()
		case metrics.ScrapeFormatLabelOpenMetrics:
			metrics.ScrapeFormatOpenMetric.Increment()
		default:
			metrics.ScrapeFormatText.Increment()
		}
	}

	// PARSE-AND-RE-ENCODE PATH: at least one upstream is non-text/plain. We must
	// decode all upstream bodies (any text ones get decoded too — they will be
	// re-encoded in outFormat below) and re-encode them as a single stream so the
	// merged response is wire-correct. Byte concatenation is unsafe here because:
	//   (1) when outFormat is proto, concatenating text and delimited bytes corrupts the stream;
	//   (2) when outFormat is text (downgrade), the binary proto upstream cannot be passed through;
	//   (3) when an OpenMetrics body carries a trailing "# EOF", concatenating
	//       additional bodies after it lands the EOF marker mid-stream.
	if needsParseMerge {
		s.writeMergedProtoPath(w, outFormat, envoy, envoyFormat, application, appFormat, appBodies, appBodyFormats)
		return
	}

	// TEXT / OPENMETRICS FAST PATH: byte-concatenation is wire-correct when
	// Envoy is text/plain (or absent) and the apps return either text or
	// OpenMetrics — the multi-target loop below strips and reappends a single
	// "# EOF" terminator for the OM case, and the single-target branch writes
	// the app body last so its own "# EOF" terminates the response.
	if err = scrapeAndWriteAgentMetrics(s.registry, io.Writer(w)); err != nil {
		log.Errorf("failed scraping and writing agent metrics: %v", err)
		metrics.AgentScrapeErrors.Increment()
	}

	if envoy != nil {
		_, err = io.Copy(w, envoy)
		if err != nil {
			log.Errorf("failed to scraping and writing envoy metrics: %v", err)
			metrics.EnvoyScrapeErrors.Increment()
		}
	}

	// App metrics must go last because if they are FmtOpenMetrics,
	// they will have a trailing "# EOF" which terminates the full exposition.
	// The two branches are mutually exclusive: the dispatcher above sets exactly
	// one of `application` (single-target streaming) or `appBodies` (multi-target buffered).
	if appBodies != nil {
		// Multi-target path: write each target's buffered body in Targets order.
		// EOF-handling rule:
		//   - Strip "# EOF" from every body unconditionally.
		//   - When the negotiated format is OpenMetrics, append a single "# EOF\n" after
		//     all bodies via expfmt.FinalizeOpenMetrics. When the format is text, no
		//     terminator is appended.
		// Newline rule: ensure a "\n" separates concatenated bodies in case a target
		// response does not end with one. Split into a second w.Write so the stripped
		// sub-slice does not trigger a body-sized append+realloc.
		openMetrics := outFormat.FormatType() == expfmt.TypeOpenMetrics
		for i, body := range appBodies {
			if body == nil {
				continue
			}
			out := stripOpenMetricsEOF(body)
			if _, werr := w.Write(out); werr != nil {
				log.Errorf("failed writing application metrics from target %d: %v", i, werr)
				metrics.AppScrapeErrors.Increment()
			} else if len(out) > 0 && out[len(out)-1] != '\n' {
				if _, werr := w.Write([]byte{'\n'}); werr != nil {
					log.Errorf("failed writing newline separator for target %d: %v", i, werr)
					metrics.AppScrapeErrors.Increment()
				}
			}
			// Release the body for GC before the next iteration; the merged response is
			// already on the wire (or in the buffered ResponseWriter) at this point.
			appBodies[i] = nil
		}
		if openMetrics {
			if _, werr := expfmt.FinalizeOpenMetrics(w); werr != nil {
				log.Errorf("failed writing application metrics EOF marker: %v", werr)
				metrics.AppScrapeErrors.Increment()
			}
		}
	} else if application != nil {
		if _, err = io.Copy(w, application); err != nil {
			log.Errorf("failed to scraping and writing application metrics: %v", err)
			metrics.AppScrapeErrors.Increment()
		}
	}
}

// writeMergedProtoPath is the parse-and-re-encode merger used whenever at least one
// upstream returned the delimited Prometheus protobuf format. It decodes every upstream
// (proto and non-proto alike) into MetricFamily values, gathers the agent's own
// MetricFamily values directly from the registry, and re-encodes the union in outFormat.
//
// Failure handling matches the text fast path: a malformed upstream is dropped with an
// error log and a scrape-errors counter increment; the merged response still contains
// the remaining upstreams and the agent self-metrics. Ordering inside the encoded
// stream is envoy → agent → app(s); this preserves the agent-metrics-precede-trailer
// invariant the text path relies on, which keeps log analyzers and grafana dashboards
// happy when format negotiation flips back and forth.
//
// Memory safety: each external upstream Reader (Envoy and single-target app) is wrapped
// in a body-cap LimitReader bounded by s.maxAppBodyBytes. Reading past the cap is
// detected (we read cap+1 bytes) and surfaced as a per-upstream drop, mirroring the
// multi-target text path's pre-existing cap in scrapeMultipleApps. The agent path
// reads directly from the in-process registry and needs no cap.
//
// appBodyFormats is the per-body negotiated format for the multi-target case; the
// merger decodes each buffered body in its own format so a binary proto body and a
// text body in the same target list are both correctly parsed even though the
// aggregated appFormat has been downgraded to text.
//
// Note: when an app upstream is text on the mixed-format downgrade path, the
// underlying expfmt textDecoder emits MetricFamilies in Go map-iteration order,
// which is non-deterministic across scrapes. Output is still wire-correct; only
// the human-visible ordering of app metrics jitters between scrapes.
func (s *Server) writeMergedProtoPath(
	w io.Writer,
	outFormat expfmt.Format,
	envoy io.Reader,
	envoyFormat expfmt.Format,
	application io.Reader,
	appFormat expfmt.Format,
	appBodies [][]byte,
	appBodyFormats []expfmt.Format,
) {
	enc := expfmt.NewEncoder(w, outFormat)

	encodeAll := func(mfs []*dto.MetricFamily, errLabel string, counter monitoring.Metric) {
		for _, mf := range mfs {
			if err := enc.Encode(mf); err != nil {
				log.Errorf("failed encoding %s metric family %q: %v", errLabel, mf.GetName(), err)
				counter.Increment()
				return
			}
		}
	}

	// decodeCapped wraps r in a body-cap LimitReader and runs the format-aware decoder.
	// Returns the decoded families plus any error (parse error OR body-too-large).
	// Partial decodes are preserved on overflow, matching the text path's behavior of
	// surfacing whatever data was readable before the failure.
	decodeCapped := func(r io.Reader, format expfmt.Format) ([]*dto.MetricFamily, error) {
		bodyCap := int64(s.maxAppBodyBytes)
		counted := &countingReader{R: io.LimitReader(r, bodyCap+1)}
		mfs, err := decodeAll(counted, format)
		if counted.N > bodyCap {
			return mfs, fmt.Errorf("upstream body exceeds %d bytes; dropping", s.maxAppBodyBytes)
		}
		return mfs, err
	}

	// Envoy first.
	if envoy != nil {
		mfs, err := decodeCapped(envoy, envoyFormat)
		if err != nil {
			log.Errorf("failed decoding envoy metrics (format=%q): %v", envoyFormat, err)
			metrics.EnvoyScrapeErrors.Increment()
		}
		encodeAll(mfs, "envoy", metrics.EnvoyScrapeErrors)
	}

	// Agent self-metrics: native MetricFamily values from the gatherer; no parse step.
	mfs, err := s.registry.Gather()
	if err != nil {
		log.Errorf("failed gathering agent metrics: %v", err)
		metrics.AgentScrapeErrors.Increment()
	} else {
		encodeAll(mfs, "agent", metrics.AgentScrapeErrors)
	}

	// Application metrics: either a single-target streaming body or multi-target
	// buffered bodies, mirroring the text fast-path dispatch.
	switch {
	case appBodies != nil:
		for i, body := range appBodies {
			if body == nil {
				continue
			}
			// Use the per-body negotiated format. When appBodyFormats is missing
			// or short (defensive — shouldn't happen if scrapeMultipleApps is the
			// only producer), fall back to the aggregated appFormat.
			perBodyFormat := appFormat
			if i < len(appBodyFormats) && appBodyFormats[i] != "" {
				perBodyFormat = appBodyFormats[i]
			}
			// Multi-target bodies are already capped at maxAppBodyBytes inside
			// scrapeMultipleApps; decode directly from bytes.NewReader without
			// re-applying the cap (which would just no-op anyway).
			appMfs, decodeErr := decodeAll(bytes.NewReader(body), perBodyFormat)
			if decodeErr != nil {
				log.Errorf("failed decoding app target %d (format=%q): %v", i, perBodyFormat, decodeErr)
				metrics.AppScrapeErrors.Increment()
			}
			encodeAll(appMfs, "application", metrics.AppScrapeErrors)
			appBodies[i] = nil
		}
	case application != nil:
		appMfs, decodeErr := decodeCapped(application, appFormat)
		if decodeErr != nil {
			log.Errorf("failed decoding application metrics (format=%q): %v", appFormat, decodeErr)
			metrics.AppScrapeErrors.Increment()
		}
		encodeAll(appMfs, "application", metrics.AppScrapeErrors)
	}

	// Close the encoder: for OpenMetrics this writes "# EOF"; for proto/text it is a no-op.
	if closer, ok := enc.(expfmt.Closer); ok {
		if cerr := closer.Close(); cerr != nil {
			log.Errorf("failed closing merged metrics encoder: %v", cerr)
		}
	}
}

// countingReader counts the bytes returned by an underlying Reader. It is used by
// writeMergedProtoPath to enforce s.maxAppBodyBytes on proto-path upstreams: pair
// it with io.LimitReader(r, cap+1) and check N > cap after decoding to distinguish
// an at-cap legitimate body from one that saturated the limit.
type countingReader struct {
	R io.Reader
	N int64
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.R.Read(p)
	c.N += int64(n)
	return n, err
}

// scrapeMultipleApps fans out one scrape per entry in s.prometheus.Targets concurrently and
// returns the bodies indexed by Targets position. Failed targets are represented by nil entries;
// each failure increments metrics.AppScrapeErrors once. The returned aggregated format is the
// first successful target's negotiated format (which matches Targets[0] when Targets[0]
// succeeds), falling back to FmtText when no target succeeds; if any later successful target
// disagrees, the aggregated format downgrades to FmtText. The returned per-body formats slice
// gives each body's own negotiated format so the proto path can decode each body in its true
// format when the aggregated format has been downgraded.
func (s *Server) scrapeMultipleApps(r *http.Request) (expfmt.Format, [][]byte, []expfmt.Format) {
	bodies := make([][]byte, len(s.prometheus.Targets))
	contentTypes := make([]string, len(s.prometheus.Targets))
	var wg sync.WaitGroup
	for i, t := range s.prometheus.Targets {
		wg.Add(1)
		go func(idx int, tgt ScrapeTarget) {
			defer wg.Done()
			// fail* dedup the log+counter+return shape across the three failure paths.
			// Path is %q-quoted to neutralize CWE-117 log injection via newline-containing
			// annotation values (the path comes from prometheus.istio.io/scrape-targets).
			failErr := func(format string, args ...any) {
				log.Errorf("scrape target %q:%q: "+format, append([]any{tgt.Port, tgt.Path}, args...)...)
				metrics.AppScrapeErrors.Increment()
			}
			failWarn := func(format string, args ...any) {
				log.Warnf("scrape target %q:%q: "+format, append([]any{tgt.Port, tgt.Path}, args...)...)
				metrics.AppScrapeErrors.Increment()
			}
			body, cancel, contentType, err := s.scrape(appMetricsURL(tgt.Port, tgt.Path), r.Header)
			if cancel != nil {
				defer cancel()
			}
			if err != nil {
				failErr("scrape failed: %v", err)
				return
			}
			defer body.Close()
			// Pre-size the read buffer to 64 KiB so io.ReadAll's power-of-two growth does
			// not allocate ~14 times for a 10 MiB body. cap+1 lets us distinguish at-cap
			// (legitimate boundary) from saturation (cap+1 or more bytes consumed).
			buf := bytes.NewBuffer(make([]byte, 0, 64*1024))
			if _, readErr := buf.ReadFrom(io.LimitReader(body, int64(s.maxAppBodyBytes)+1)); readErr != nil {
				failErr("read failed: %v", readErr)
				return
			}
			if buf.Len() > s.maxAppBodyBytes {
				failWarn("response exceeds %d bytes; dropping target", s.maxAppBodyBytes)
				return
			}
			bodies[idx] = buf.Bytes()
			contentTypes[idx] = contentType
		}(i, t)
	}
	wg.Wait()

	// Format selection: first successful target's format wins, unless any later successful
	// target disagrees — in which case we downgrade to text. An OpenMetrics-claimed
	// response containing a text body (or vice versa) is invalid. Single-format scrapes
	// (all-text or all-OM) keep their format and EOF semantics.
	//
	// bodyFormats parallels bodies and gives the proto path each body's own negotiated
	// format for per-body decoding when the aggregated format has been downgraded.
	var format expfmt.Format
	bodyFormats := make([]expfmt.Format, len(bodies))
	for i, ct := range contentTypes {
		if bodies[i] == nil {
			continue
		}
		f := negotiateMetricsFormat(ct)
		bodyFormats[i] = f
		if format == "" {
			format = f
			continue
		}
		if f != format {
			format = FmtText
		}
	}
	if format == "" {
		format = FmtText
	}
	return format, bodies, bodyFormats
}

func appMetricsURL(port, path string) string {
	return fmt.Sprintf("http://localhost:%s%s", port, path)
}

// openMetricsEOF is the OpenMetrics exposition terminator without trailing newline.
var openMetricsEOF = []byte("# EOF")

// stripOpenMetricsEOF returns body without its trailing "# EOF" line if present,
// or the body unchanged if not. Trailing whitespace and "\r\n" line endings around
// the terminator are tolerated. This is the "strip" half of the OpenMetrics merge
// rule; the matching reappend uses expfmt.FinalizeOpenMetrics.
func stripOpenMetricsEOF(body []byte) []byte {
	trimmed := bytes.TrimRight(body, " \t\r\n")
	if bytes.HasSuffix(trimmed, openMetricsEOF) {
		return trimmed[:len(trimmed)-len(openMetricsEOF)]
	}
	return body
}

const (
	// nolint: revive, stylecheck
	FmtOpenMetrics_0_0_1 = expfmt.OpenMetricsType + `; version=` + expfmt.OpenMetricsVersion_0_0_1 + `; charset=utf-8`
	// nolint: revive, stylecheck
	FmtOpenMetrics_1_0_0 = expfmt.OpenMetricsType + `; version=` + expfmt.OpenMetricsVersion_1_0_0 + `; charset=utf-8`
	FmtText              = `text/plain; version=` + expfmt.TextVersion + `; charset=utf-8`
)

// FmtProtoDelim is the delimited Prometheus protobuf exposition format. Re-exported
// from prometheus/common so the merger can negotiate and emit it symmetrically with
// FmtText and the FmtOpenMetrics_* constants. Native Prometheus histograms are only
// defined for this wire format.
//
// We declare it as a var rather than a const because expfmt.NewFormat is a function
// call (the upstream library deprecated the corresponding constant); the value is
// computed once at init.
//
//nolint:gochecknoglobals // see comment above
var FmtProtoDelim = expfmt.NewFormat(expfmt.TypeProtoDelim)

// negotiateMetricsFormat inspects an upstream's response Content-Type and maps it to a
// known expfmt.Format. Recognized formats are OpenMetrics (versions 0.0.1 / 1.0.0), the
// delimited Prometheus protobuf format (used for native histograms), and text/plain
// (the default for anything unrecognized, preserving back-compat).
//
// The mapping for the protobuf branch mirrors expfmt.ResponseFormat in prometheus/common:
// the media type must be application/vnd.google.protobuf, the "proto" parameter must be
// io.prometheus.client.MetricFamily, and the "encoding" parameter must be "delimited".
// Anything else (a different proto, a different encoding, or absent params) falls through
// to text — the merger then either decodes text or, if the upstream lied about its
// Content-Type, the downstream decoder surfaces the error and the upstream is dropped.
func negotiateMetricsFormat(contentType string) expfmt.Format {
	mediaType, params, err := mime.ParseMediaType(contentType)
	if err != nil {
		return FmtText
	}
	switch mediaType {
	case expfmt.OpenMetricsType:
		switch params["version"] {
		case expfmt.OpenMetricsVersion_1_0_0:
			return FmtOpenMetrics_1_0_0
		case expfmt.OpenMetricsVersion_0_0_1, "":
			return FmtOpenMetrics_0_0_1
		}
	case expfmt.ProtoType:
		// Both parameters are mandatory per the Prometheus native-histogram spec.
		// We accept "" for proto only when absent (matching expfmt.ResponseFormat
		// which treats absent as "matches").
		if proto, ok := params["proto"]; ok && proto != expfmt.ProtoProtocol {
			return FmtText
		}
		if encoding, ok := params["encoding"]; ok && encoding != "delimited" {
			return FmtText
		} else if !ok {
			// expfmt.ResponseFormat tolerates absent encoding; we require it
			// explicitly so the merger never tries to delimit-decode a body
			// that the upstream advertised as compact-text or proto-text.
			return FmtText
		}
		return FmtProtoDelim
	}
	return FmtText
}

// mergeFormat returns the negotiated output format given the formats negotiated for each
// successful upstream. Disagreement downgrades to FmtText (the universal common
// denominator). An empty argument list returns FmtText.
//
// The downgrade rule is deliberately conservative: if any two upstreams disagree we emit
// text, even when one of them is protobuf. The alternative (text→protobuf upconversion)
// is gated on a CPU/memory cost-comparison study mandated by the Istio TOC and is the
// subject of PR3 in the native-histogram contribution sequence.
func mergeFormat(formats ...expfmt.Format) expfmt.Format {
	var out expfmt.Format
	for _, f := range formats {
		if f == "" {
			continue
		}
		if out == "" {
			out = f
			continue
		}
		if f != out {
			return FmtText
		}
	}
	if out == "" {
		return FmtText
	}
	return out
}

// (the dead legacy helper has been intentionally removed; the protobuf-aware
// negotiateMetricsFormat above is the single source of truth.)

// scrapeFormatLabel maps an expfmt.Format to the value used for the
// istio_agent_scrape_format_total{format=...} label.
func scrapeFormatLabel(f expfmt.Format) string {
	switch f.FormatType() {
	case expfmt.TypeProtoDelim, expfmt.TypeProtoText, expfmt.TypeProtoCompact:
		return metrics.ScrapeFormatLabelProto
	case expfmt.TypeOpenMetrics:
		return metrics.ScrapeFormatLabelOpenMetrics
	default:
		return metrics.ScrapeFormatLabelText
	}
}

// decodeAll reads body as `format` and returns the decoded MetricFamily slice.
// Used only on the protobuf path; for the text-only fast path the merger keeps
// byte concatenation so adding a parse step would be a regression.
//
// OpenMetrics bodies are dispatched to decodeOpenMetricsToFamilies (which uses
// prometheus/prometheus/model/textparse.OpenMetricsParser) so that exemplars,
// _created timestamps, # UNIT, INFO, and STATESET survive the round-trip.
// The expfmt text decoder silently drops these — see prometheus/common#812.
func decodeAll(body io.Reader, format expfmt.Format) ([]*dto.MetricFamily, error) {
	if format.FormatType() == expfmt.TypeOpenMetrics {
		return decodeOpenMetricsToFamilies(body)
	}
	dec := expfmt.NewDecoder(body, format)
	var mfs []*dto.MetricFamily
	for {
		var mf dto.MetricFamily
		if err := dec.Decode(&mf); err != nil {
			if errors.Is(err, io.EOF) {
				return mfs, nil
			}
			return mfs, err
		}
		mfs = append(mfs, &mf)
	}
}

// scrapeAndWriteAgentMetrics writes the agent's own self-metrics as the first
// chunk of the byte-concatenation fast path. It always encodes as plain text.
//
// This chunk is followed by the Envoy and application bodies, so it must not be
// a self-contained document. The OpenMetrics encoder requires a closing "# EOF"
// marker to be a valid document (expfmt's encoder emits it on Close), and
// writing that terminator here would place it mid-stream and corrupt the merged
// response. Plain text has no such terminator; the response is terminated
// correctly by the application body, which is written last. Proto and
// OpenMetrics output never flow through this function — the parse-and-re-encode
// path (writeMergedProtoPath) gathers and encodes the agent registry itself.
func scrapeAndWriteAgentMetrics(registry prometheus.Gatherer, w io.Writer) error {
	mfs, err := registry.Gather()
	if err != nil {
		return err
	}
	enc := expfmt.NewEncoder(w, FmtText)
	for _, mf := range mfs {
		if err := enc.Encode(mf); err != nil {
			return err
		}
	}
	return nil
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
// such as accept, timeout, and user agent
// Returns the scraped metrics reader as well as the response's "Content-Type" header to determine the metrics format
func (s *Server) scrape(url string, header http.Header) (io.ReadCloser, context.CancelFunc, string, error) {
	var cancel context.CancelFunc
	ctx := context.Background()
	if timeoutString := header.Get("X-Prometheus-Scrape-Timeout-Seconds"); timeoutString != "" {
		timeout, err := getHeaderTimeout(timeoutString)
		if err != nil {
			log.Warnf("Failed to parse timeout header %v: %v", timeoutString, err)
		} else {
			ctx, cancel = context.WithTimeout(ctx, timeout)
		}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, cancel, "", err
	}
	applyHeaders(req.Header, header, "Accept",
		"User-Agent",
		"X-Prometheus-Scrape-Timeout-Seconds",
	)

	resp, err := s.http.Do(req)
	if err != nil {
		return nil, cancel, "", fmt.Errorf("error scraping %s: %v", url, err)
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, cancel, "", fmt.Errorf("error scraping %s, status code: %v", url, resp.StatusCode)
	}
	format := resp.Header.Get("Content-Type")
	return resp.Body, cancel, format, nil
}

func (s *Server) handleQuit(w http.ResponseWriter, r *http.Request) {
	if !istioNetUtil.IsRequestFromLocalhost(r) {
		http.Error(w, "Only requests from localhost are allowed", http.StatusForbidden)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("OK"))
	log.Infof("handling %s, notifying pilot-agent to exit", quitPath)
	s.disableDrain()
	// Notify the agent to exit.
	s.shutdown(fmt.Errorf("%v called", quitPath))
}

func (s *Server) handleDrain(w http.ResponseWriter, r *http.Request) {
	if !istioNetUtil.IsRequestFromLocalhost(r) {
		http.Error(w, "Only requests from localhost are allowed", http.StatusForbidden)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("OK"))
	log.Infof("handling %s, starting drain", drainPath)
	s.drain()
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

	switch {
	case prober.HTTPGet != nil:
		s.handleAppProbeHTTPGet(w, req, prober, path)
	case prober.TCPSocket != nil:
		s.handleAppProbeTCPSocket(w, prober)
	case prober.GRPC != nil:
		s.handleAppProbeGRPC(w, req, prober)
	}
}

func (s *Server) handleAppProbeHTTPGet(w http.ResponseWriter, req *http.Request, prober *Prober, path string) {
	proberPath := prober.HTTPGet.Path
	if !strings.HasPrefix(proberPath, "/") {
		proberPath = "/" + proberPath
	}
	var url string

	hostPort := net.JoinHostPort(s.appProbersDestination, strconv.Itoa(prober.HTTPGet.Port.IntValue()))
	if prober.HTTPGet.Scheme == apimirror.URISchemeHTTPS {
		url = fmt.Sprintf("https://%s%s", hostPort, proberPath)
	} else {
		url = fmt.Sprintf("http://%s%s", hostPort, proberPath)
	}
	appReq, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		log.Errorf("Failed to create request to probe app %v, original url %v", err, path)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	appReq.Host = req.Host
	if host, port, err := net.SplitHostPort(req.Host); err == nil {
		port, _ := strconv.Atoi(port)
		// the port is same as the status port, then we need to replace the port in the host with the real one
		if port == int(s.statusPort) {
			realPort := strconv.Itoa(prober.HTTPGet.Port.IntValue())
			appReq.Host = net.JoinHostPort(host, realPort)
		}
	}
	// Forward incoming headers to the application.
	for name, values := range req.Header {
		appReq.Header[name] = slices.Clone(values)
		if len(values) > 0 && (name == "Host") {
			// Probe has specific host header override; honor it
			appReq.Host = values[0]
		}
	}

	// get the http client must exist because
	httpClient := s.appProbeClient[path]

	// Send the request.
	response, err := httpClient.Do(appReq)
	if err != nil {
		probes.Errorf("Request to probe app failed: %v, original URL path = %v\napp URL path = %v", err, path, proberPath)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	defer func() {
		// Drain and close the body to let the Transport reuse the connection
		_, _ = io.Copy(io.Discard, response.Body)
		_ = response.Body.Close()
	}()

	if isRedirect(response.StatusCode) { // Redirect
		// In other cases, we return the original status code. For redirects, it is illegal to
		// not have Location header, so we need to switch to just 200.
		w.WriteHeader(http.StatusOK)
		return
	}
	// We only write the status code to the response.
	w.WriteHeader(response.StatusCode)
	// Return the body from probe as well
	b, _ := k8sUtilIo.ReadAtMost(response.Body, maxRespBodyLength)
	_, _ = w.Write(b)
}

func (s *Server) handleAppProbeTCPSocket(w http.ResponseWriter, prober *Prober) {
	timeout := time.Duration(prober.TimeoutSeconds) * time.Second

	d := ProbeDialer()
	d.LocalAddr = s.upstreamLocalAddress
	d.Timeout = timeout

	conn, err := d.Dial("tcp", net.JoinHostPort(s.appProbersDestination, strconv.Itoa(prober.TCPSocket.Port.IntValue())))
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
	} else {
		w.WriteHeader(http.StatusOK)
		err = conn.Close()
		if err != nil {
			probes.Infof("tcp connection is not closed: %v", err)
		}
	}
}

func (s *Server) handleAppProbeGRPC(w http.ResponseWriter, req *http.Request, prober *Prober) {
	timeout := time.Duration(prober.TimeoutSeconds) * time.Second
	// the DialOptions are referenced from https://github.com/kubernetes/kubernetes/blob/v1.23.1/pkg/probe/grpc/grpc.go#L55-L59
	opts := []grpc.DialOption{
		grpc.WithBlock(),
		grpc.WithTransportCredentials(insecure.NewCredentials()), // credentials are currently not supported
		grpc.WithContextDialer(func(ctx context.Context, addr string) (net.Conn, error) {
			d := ProbeDialer()
			d.LocalAddr = s.upstreamLocalAddress
			d.Timeout = timeout
			return d.DialContext(ctx, "tcp", addr)
		}),
	}
	if userAgent := req.Header["User-Agent"]; len(userAgent) > 0 {
		// simulate kubelet
		// please refer to:
		// https://github.com/kubernetes/kubernetes/blob/v1.23.1/pkg/probe/grpc/grpc.go#L56
		// https://github.com/kubernetes/kubernetes/blob/v1.23.1/pkg/probe/http/http.go#L103
		opts = append(opts, grpc.WithUserAgent(userAgent[0]))
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	addr := net.JoinHostPort(s.appProbersDestination, strconv.Itoa(int(prober.GRPC.Port)))
	conn, err := grpc.DialContext(ctx, addr, opts...)
	if err != nil {
		probes.Errorf("Failed to create grpc connection to probe app: %v", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	defer conn.Close()

	var svc string
	if prober.GRPC.Service != nil {
		svc = *prober.GRPC.Service
	}
	grpcClient := grpcHealth.NewHealthClient(conn)
	resp, err := grpcClient.Check(ctx, &grpcHealth.HealthCheckRequest{
		Service: svc,
	})
	// the error handling is referenced from https://github.com/kubernetes/kubernetes/blob/v1.23.1/pkg/probe/grpc/grpc.go#L88-L106
	if err != nil {
		status, ok := grpcStatus.FromError(err)
		if ok {
			switch status.Code() {
			case codes.Unimplemented:
				probes.Errorf("server does not implement the grpc health protocol (grpc.health.v1.Health): %v", err)
			case codes.DeadlineExceeded:
				probes.Errorf("grpc request not finished within timeout: %v", err)
			default:
				probes.Errorf("grpc probe failed: %v", err)
			}
		} else {
			probes.Errorf("grpc probe failed: %v", err)
		}
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	if resp.GetStatus() == grpcHealth.HealthCheckResponse_SERVING {
		w.WriteHeader(http.StatusOK)
		return
	}
	w.WriteHeader(http.StatusInternalServerError)
}

func (s *Server) handleNdsz(w http.ResponseWriter, r *http.Request) {
	if !istioNetUtil.IsRequestFromLocalhost(r) {
		http.Error(w, "Only requests from localhost are allowed", http.StatusForbidden)
		return
	}
	nametable := s.fetchDNS()
	if nametable == nil {
		// See https://golang.org/doc/faq#nil_error for why writeJSONProto cannot handle this
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{}`))
		return
	}
	writeJSONProto(w, nametable)
}

// writeJSONProto writes a protobuf to a json payload, handling content type, marshaling, and errors
func writeJSONProto(w http.ResponseWriter, obj proto.Message) {
	w.Header().Set("Content-Type", "application/json")
	b, err := protomarshal.Marshal(obj)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(err.Error()))
		return
	}
	_, err = w.Write(b)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
	}
}

// notifyExit sends SIGTERM to itself
func notifyExit() {
	p, err := os.FindProcess(os.Getpid())
	if err != nil {
		log.Error(err)
	}
	if err := p.Signal(syscall.SIGTERM); err != nil {
		log.Errorf("failed to send SIGTERM to self: %v", err)
	}
}

var defaultTransport = http.DefaultTransport.(*http.Transport)

// SetTransportDefaults mirrors Kubernetes probe settings
// https://github.com/kubernetes/kubernetes/blob/0153febd9f0098d4b8d0d484927710eaf899ef40/pkg/probe/http/http.go#L52
func setTransportDefaults(t *http.Transport) (*http.Transport, error) {
	if !EnableHTTP2Probing {
		return t, nil
	}
	if t.TLSHandshakeTimeout == 0 {
		t.TLSHandshakeTimeout = defaultTransport.TLSHandshakeTimeout
	}
	if t.IdleConnTimeout == 0 {
		t.IdleConnTimeout = defaultTransport.IdleConnTimeout
	}
	t2, err := http2.ConfigureTransports(t)
	if err != nil {
		return nil, err
	}
	t2.ReadIdleTimeout = time.Duration(30) * time.Second
	t2.PingTimeout = time.Duration(15) * time.Second
	return t, nil
}
