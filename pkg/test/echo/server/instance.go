//  Copyright Istio Authors
//
//  Licensed under the Apache License, Version 2.0 (the "License");
//  you may not use this file except in compliance with the License.
//  You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
//  Unless required by applicable law or agreed to in writing, software
//  distributed under the License is distributed on an "AS IS" BASIS,
//  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//  See the License for the specific language governing permissions and
//  limitations under the License.

package server

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"sync/atomic"

	ocprom "contrib.go.opencensus.io/exporter/prometheus"
	"github.com/hashicorp/go-multierror"
	"github.com/prometheus/client_golang/prometheus"
	"go.opencensus.io/stats/view"

	"istio.io/istio/pilot/pkg/util/network"
	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/test/echo/common"
	"istio.io/istio/pkg/test/echo/server/endpoint"
	"istio.io/pkg/log"
)

// Config for an echo server Instance.
type Config struct {
	Ports                 common.PortList
	BindIPPortsMap        map[int]struct{}
	BindLocalhostPortsMap map[int]struct{}
	Metrics               int
	TLSCert               string
	TLSKey                string
	Version               string
	UDSServer             string
	Cluster               string
	Dialer                common.Dialer
	IstioVersion          string
	DisableALPN           bool
}

func (c Config) String() string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("Ports:                 %v\n", c.Ports))
	b.WriteString(fmt.Sprintf("BindIPPortsMap:        %v\n", c.BindIPPortsMap))
	b.WriteString(fmt.Sprintf("BindLocalhostPortsMap: %v\n", c.BindLocalhostPortsMap))
	b.WriteString(fmt.Sprintf("Metrics:               %v\n", c.Metrics))
	b.WriteString(fmt.Sprintf("TLSCert:               %v\n", c.TLSCert))
	b.WriteString(fmt.Sprintf("TLSKey:                %v\n", c.TLSKey))
	b.WriteString(fmt.Sprintf("Version:               %v\n", c.Version))
	b.WriteString(fmt.Sprintf("UDSServer:             %v\n", c.UDSServer))
	b.WriteString(fmt.Sprintf("Cluster:               %v\n", c.Cluster))
	b.WriteString(fmt.Sprintf("IstioVersion:          %v\n", c.IstioVersion))

	return b.String()
}

var (
	serverLog           = log.RegisterScope("server", "echo server", 0)
	_         io.Closer = &Instance{}
)

// Instance of the Echo server.
type Instance struct {
	Config

	endpoints     []endpoint.Instance
	metricsServer *http.Server
	ready         uint32
}

// New creates a new server instance.
func New(config Config) *Instance {
	log.Infof("Creating Server with config:\n%s", config)
	config.Dialer = config.Dialer.FillInDefaults()

	return &Instance{
		Config: config,
	}
}

// Start the server.
func (s *Instance) Start() (err error) {
	defer func() {
		if err != nil {
			_ = s.Close()
		}
	}()

	if err = s.validate(); err != nil {
		return err
	}

	if s.Metrics > 0 {
		go s.startMetricsServer()
	}
	s.endpoints = make([]endpoint.Instance, 0)

	for _, p := range s.Ports {
		ip, err := s.getListenerIP(p)
		if err != nil {
			return err
		}
		for _, ip := range getBindAddresses(ip) {
			ep, err := s.newEndpoint(p, ip, "")
			if err != nil {
				return err
			}
			s.endpoints = append(s.endpoints, ep)
		}
	}

	if len(s.UDSServer) > 0 {
		ep, err := s.newEndpoint(nil, "", s.UDSServer)
		if err != nil {
			return err
		}
		s.endpoints = append(s.endpoints, ep)
	}

	return s.waitUntilReady()
}

func getBindAddresses(ip string) []string {
	if ip != "" && ip != "localhost" {
		return []string{ip}
	}
	// Binding to "localhost" will only bind to a single address (v4 or v6). We want both, so we need
	// to be explicit
	v4, v6 := false, false
	// Obtain all the IPs from the node
	ipAddrs, ok := network.GetPrivateIPs(context.Background())
	if !ok {
		return []string{ip}
	}
	for _, ip := range ipAddrs {
		addr := net.ParseIP(ip)
		if addr == nil {
			// Should not happen
			continue
		}
		if addr.To4() != nil {
			v4 = true
		} else {
			v6 = true
		}
	}
	addrs := []string{}
	if v4 {
		if ip == "localhost" {
			addrs = append(addrs, "127.0.0.1")
		} else {
			addrs = append(addrs, "0.0.0.0")
		}
	}
	if v6 {
		if ip == "localhost" {
			addrs = append(addrs, "::1")
		} else {
			addrs = append(addrs, "::")
		}
	}
	return addrs
}

// Close implements the application.Application interface
func (s *Instance) Close() (err error) {
	for _, s := range s.endpoints {
		if s != nil {
			err = multierror.Append(err, s.Close())
		}
	}
	return
}

func (s *Instance) getListenerIP(port *common.Port) (string, error) {
	// Not configured on this port, set to empty which will lead to wildcard bind
	// Not 0.0.0.0 in case we want IPv6
	if port == nil {
		return "", nil
	}
	if _, f := s.BindLocalhostPortsMap[port.Port]; f {
		return "localhost", nil
	}
	if _, f := s.BindIPPortsMap[port.Port]; !f {
		return "", nil
	}
	if ip, f := os.LookupEnv("INSTANCE_IP"); f {
		return ip, nil
	}
	return "", fmt.Errorf("--bind-ip set but INSTANCE_IP undefined")
}

func (s *Instance) newEndpoint(port *common.Port, listenerIP string, udsServer string) (endpoint.Instance, error) {
	return endpoint.New(endpoint.Config{
		Port:          port,
		UDSServer:     udsServer,
		IsServerReady: s.isReady,
		Version:       s.Version,
		Cluster:       s.Cluster,
		TLSCert:       s.TLSCert,
		TLSKey:        s.TLSKey,
		Dialer:        s.Dialer,
		ListenerIP:    listenerIP,
		DisableALPN:   s.DisableALPN,
		IstioVersion:  s.IstioVersion,
	})
}

func (s *Instance) isReady() bool {
	return atomic.LoadUint32(&s.ready) == 1
}

func (s *Instance) waitUntilReady() error {
	wg := &sync.WaitGroup{}

	onEndpointReady := func() {
		wg.Done()
	}

	// Start the servers, updating port numbers as necessary.
	for _, ep := range s.endpoints {
		wg.Add(1)
		if err := ep.Start(onEndpointReady); err != nil {
			return err
		}
	}

	// Wait for all the servers to start.
	wg.Wait()

	// Indicate that the server is now ready.
	atomic.StoreUint32(&s.ready, 1)

	log.Info("Echo server is now ready")
	return nil
}

func (s *Instance) validate() error {
	for _, port := range s.Ports {
		switch port.Protocol {
		case protocol.TCP:
		case protocol.HTTP:
		case protocol.HTTPS:
		case protocol.HTTP2:
		case protocol.GRPC:
		default:
			return fmt.Errorf("protocol %v not currently supported", port.Protocol)
		}
	}
	return nil
}

func (s *Instance) startMetricsServer() {
	mux := http.NewServeMux()

	exporter, err := ocprom.NewExporter(ocprom.Options{Registry: prometheus.DefaultRegisterer.(*prometheus.Registry)})
	if err != nil {
		log.Errorf("could not set up prometheus exporter: %v", err)
		return
	}
	view.RegisterExporter(exporter)
	mux.Handle("/metrics", LogRequests(exporter))
	s.metricsServer = &http.Server{
		Handler: mux,
	}
	if err := http.ListenAndServe(fmt.Sprintf(":%d", s.Metrics), mux); err != nil {
		log.Errorf("metrics terminated with err: %v", err)
	}
}

func LogRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			serverLog.WithLabels(
				"remoteAddr", r.RemoteAddr, "method", r.Method, "url", r.URL, "host", r.Host, "headers", r.Header,
			).Infof("Metrics Request")
			next.ServeHTTP(w, r)
		},
	)
}
