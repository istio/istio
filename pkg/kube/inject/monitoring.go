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

package inject

import (
	"fmt"
	"net"
	"net/http"

	ocprom "contrib.go.opencensus.io/exporter/prometheus"
	"github.com/prometheus/client_golang/prometheus"
	"go.opencensus.io/stats/view"

	"istio.io/pkg/monitoring"
)

const (
	metricsPath = "/metrics"
)

type monitor struct {
	monitoringServer *http.Server
	exporter         *ocprom.Exporter
	shutdown         chan struct{}
}

var (
	totalInjections = monitoring.NewSum(
		"sidecar_injection_requests_total",
		"Total number of sidecar injection requests.",
	)

	totalSuccessfulInjections = monitoring.NewSum(
		"sidecar_injection_success_total",
		"Total number of successful sidecar injection requests.",
	)

	totalFailedInjections = monitoring.NewSum(
		"sidecar_injection_failure_total",
		"Total number of failed sidecar injection requests.",
	)

	totalSkippedInjections = monitoring.NewSum(
		"sidecar_injection_skip_total",
		"Total number of skipped sidecar injection requests.",
	)
)

func init() {
	monitoring.MustRegister(
		totalInjections,
		totalSuccessfulInjections,
		totalFailedInjections,
		totalSkippedInjections,
	)
}

func startMonitor(mux *http.ServeMux, port int) (*monitor, error) {
	m := &monitor{
		shutdown: make(chan struct{}),
	}

	// get the network stuff setup
	var listener net.Listener
	var err error
	var exporter *ocprom.Exporter
	if listener, err = net.Listen("tcp", fmt.Sprintf(":%d", port)); err != nil {
		return nil, fmt.Errorf("unable to listen on socket: %v", err)
	}

	// NOTE: this is a temporary solution to provide bare-bones debug functionality
	// for pilot. a full design / implementation of self-monitoring and reporting
	// is coming. that design will include proper coverage of statusz/healthz type
	// functionality, in addition to how pilot reports its own metrics.
	if exporter, err = addMonitor(mux); err != nil {
		return nil, fmt.Errorf("could not establish self-monitoring: %v", err)
	}
	m.exporter = exporter
	m.monitoringServer = &http.Server{
		Handler: mux,
	}

	go func() {
		m.shutdown <- struct{}{}
		_ = m.monitoringServer.Serve(listener)
		m.shutdown <- struct{}{}
	}()

	// This is here to work around (mostly) a race condition in the Serve
	// function. If the Close method is called before or during the execution of
	// Serve, the call may be ignored and Serve never returns.
	<-m.shutdown

	return m, nil
}

func addMonitor(mux *http.ServeMux) (*ocprom.Exporter, error) {
	exporter, err := ocprom.NewExporter(ocprom.Options{Registry: prometheus.DefaultRegisterer.(*prometheus.Registry)})
	if err != nil {
		return nil, fmt.Errorf("could not set up prometheus exporter: %v", err)
	}
	view.RegisterExporter(exporter)
	mux.Handle(metricsPath, exporter)

	return exporter, nil
}
