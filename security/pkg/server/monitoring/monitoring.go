// Copyright 2017 Istio Authors
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

package monitoring

import (
	"fmt"
	"net"
	"net/http"
	"net/http/pprof"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	ocprom "go.opencensus.io/exporter/prometheus"
	"go.opencensus.io/stats/view"

	"istio.io/istio/pkg/log"
	"istio.io/istio/pkg/version"
)

// Monitor is the server that exposes Prometheus metrics about Citadel.
type Monitor struct {
	monitoringServer *http.Server
	port             int
	closed           chan bool
}

const (
	metricsPath = "/metrics"
	versionPath = "/version"
)

// NewMonitor creates a monitor.
func NewMonitor(port int, enableProfiling bool) (*Monitor, error) {
	m := &Monitor{
		port:   port,
		closed: make(chan bool),
	}

	mux := http.NewServeMux()

	exporter, err := ocprom.NewExporter(ocprom.Options{Registry: prometheus.DefaultRegisterer.(*prometheus.Registry)})
	if err != nil {
		return nil, fmt.Errorf("could not set up prometheus exporter: %v", err)
	}
	view.RegisterExporter(exporter)
	mux.Handle(metricsPath, exporter)

	mux.HandleFunc(versionPath, func(out http.ResponseWriter, req *http.Request) {
		if _, err := out.Write([]byte(version.Info.String())); err != nil {
			log.Errorf("Unable to write version string: %v", err)
		}
	})

	if enableProfiling {
		mux.HandleFunc("/debug/pprof/", pprof.Index)
		mux.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
		mux.HandleFunc("/debug/pprof/profile", pprof.Profile)
		mux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
		mux.HandleFunc("/debug/pprof/trace", pprof.Trace)
	}

	m.monitoringServer = &http.Server{
		Handler: mux,
	}

	return m, nil
}

// Start starts the monitor server.
func (m *Monitor) Start(errCh chan<- error) {
	// get the network stuff setup
	listener, lErr := net.Listen("tcp", fmt.Sprintf(":%d", m.port))
	if lErr != nil {
		errCh <- fmt.Errorf("unable to listen on socket: %v", lErr)
		return
	}

	log.Info("Monitor server started.")
	err := m.monitoringServer.Serve(listener)
	errCh <- fmt.Errorf("monitor server error: %v", err)
	close(m.closed)
}

// Close stops the Monitoring server for Citadel.
func (m *Monitor) Close() error {
	var err error

	// This works around a race condition between Serve() and Close() functions.
	// If Close() is called before Serve(), Serve() never returns.
	// m.closed channel is used by Serve() to indicate that is has processed the Close signal
	// and exited the function. Until Serve() exists, Close() periodically issues monitoringServer.Close().

L:
	for {
		err = m.monitoringServer.Close()
		select {
		case <-m.closed:
			break L
		default:
			time.Sleep(20 * time.Millisecond)
		}
	}
	return err
}
