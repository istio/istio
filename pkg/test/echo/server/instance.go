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
	"fmt"
	"io"
	"net/http"
	"sync"
	"sync/atomic"

	ocprom "contrib.go.opencensus.io/exporter/prometheus"
	"go.opencensus.io/stats/view"

	"github.com/hashicorp/go-multierror"
	"github.com/prometheus/client_golang/prometheus"

	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/test/echo/common"
	"istio.io/istio/pkg/test/echo/server/endpoint"
	"istio.io/pkg/log"
)

// Config for an echo server Instance.
type Config struct {
	Ports     common.PortList
	Metrics   int
	TLSCert   string
	TLSKey    string
	Version   string
	UDSServer string
	Cluster   string
	Dialer    common.Dialer
}

var _ io.Closer = &Instance{}

// Instance of the Echo server.
type Instance struct {
	Config

	endpoints     []endpoint.Instance
	metricsServer *http.Server
	ready         uint32
}

// New creates a new server instance.
func New(config Config) *Instance {
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
		ep, err := s.newEndpoint(p, "")
		if err != nil {
			return err
		}
		s.endpoints = append(s.endpoints, ep)
	}

	if len(s.UDSServer) > 0 {
		ep, err := s.newEndpoint(nil, s.UDSServer)
		if err != nil {
			return err
		}
		s.endpoints = append(s.endpoints, ep)
	}

	return s.waitUntilReady()
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

func (s *Instance) newEndpoint(port *common.Port, udsServer string) (endpoint.Instance, error) {
	return endpoint.New(endpoint.Config{
		Port:          port,
		UDSServer:     udsServer,
		IsServerReady: s.isReady,
		Version:       s.Version,
		Cluster:       s.Cluster,
		TLSCert:       s.TLSCert,
		TLSKey:        s.TLSKey,
		Dialer:        s.Dialer,
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
	mux.Handle("/metrics", exporter)
	s.metricsServer = &http.Server{
		Handler: mux,
	}
	if err := http.ListenAndServe(fmt.Sprintf(":%d", s.Metrics), mux); err != nil {
		log.Errorf("metrics terminated with err: %v", err)
	}
}
