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

package bootstrap

import (
	"fmt"
	"net"
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	ocprom "go.opencensus.io/exporter/prometheus"
	"go.opencensus.io/stats/view"

	"istio.io/istio/pkg/log"
	"istio.io/istio/pkg/version"
)

type monitor struct {
	monitoringServer *http.Server
	shutdown         chan struct{}
}

const (
	metricsPath = "/metrics"
	versionPath = "/version"
)

func addMonitor(mux *http.ServeMux) error {
	exporter, err := ocprom.NewExporter(ocprom.Options{Registry: prometheus.DefaultRegisterer.(*prometheus.Registry)})
	if err != nil {
		return fmt.Errorf("could not set up prometheus exporter: %v", err)
	}
	view.RegisterExporter(exporter)
	mux.Handle(metricsPath, exporter)

	mux.HandleFunc(versionPath, func(out http.ResponseWriter, req *http.Request) {
		if _, err := out.Write([]byte(version.Info.String())); err != nil {
			log.Errorf("Unable to write version string: %v", err)
		}
	})

	return nil
}

// Deprecated: we shouldn't have 2 http ports. Will be removed after code using
// this port is removed.
func startMonitor(addr string, mux *http.ServeMux) (*monitor, net.Addr, error) {
	m := &monitor{
		shutdown: make(chan struct{}),
	}

	// get the network stuff setup
	var listener net.Listener
	var err error
	if listener, err = net.Listen("tcp", addr); err != nil {
		return nil, nil, fmt.Errorf("unable to listen on socket: %v", err)
	}

	// NOTE: this is a temporary solution to provide bare-bones debug functionality
	// for pilot. a full design / implementation of self-monitoring and reporting
	// is coming. that design will include proper coverage of statusz/healthz type
	// functionality, in addition to how pilot reports its own metrics.
	if err = addMonitor(mux); err != nil {
		return nil, nil, fmt.Errorf("could not establish self-monitoring: %v", err)
	}
	m.monitoringServer = &http.Server{
		Handler: mux,
	}

	version.Info.RecordComponentBuildTag("pilot")

	go func() {
		m.shutdown <- struct{}{}
		_ = m.monitoringServer.Serve(listener)
		m.shutdown <- struct{}{}
	}()

	// This is here to work around (mostly) a race condition in the Serve
	// function. If the Close method is called before or during the execution of
	// Serve, the call may be ignored and Serve never returns.
	<-m.shutdown

	return m, listener.Addr(), nil
}

func (m *monitor) Close() error {
	err := m.monitoringServer.Close()
	<-m.shutdown
	return err
}
