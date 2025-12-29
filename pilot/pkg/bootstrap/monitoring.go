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

package bootstrap

import (
	"fmt"
	"net"
	"net/http"
	"time"

	commonFeatures "istio.io/istio/pkg/features"
	"istio.io/istio/pkg/log"
	"istio.io/istio/pkg/monitoring"
	istioNetUtil "istio.io/istio/pkg/util/net"
	"istio.io/istio/pkg/version"
)

type monitor struct {
	monitoringServer *http.Server
}

const (
	metricsPath = "/metrics"
	versionPath = "/version"
)

var (
	serverStart = time.Now()

	_ = monitoring.NewDerivedGauge(
		"istiod_uptime_seconds",
		"Current istiod server uptime in seconds",
	).ValueFrom(func() float64 {
		return time.Since(serverStart).Seconds()
	})

	versionTag   = monitoring.CreateLabel("version")
	pilotVersion = monitoring.NewGauge(
		"pilot_info",
		"Pilot version and build information.",
	)
)

func addMonitor(exporter http.Handler, mux *http.ServeMux) {
	mux.Handle(metricsPath, metricsMiddleware(exporter))

	mux.HandleFunc(versionPath, func(out http.ResponseWriter, req *http.Request) {
		if _, err := out.Write([]byte(version.Info.String())); err != nil {
			log.Errorf("Unable to write version string: %v", err)
		}
	})
}

func metricsMiddleware(handler http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if commonFeatures.MetricsLocalhostAccessOnly && !istioNetUtil.IsRequestFromLocalhost(r) {
			http.Error(w, "Only requests from localhost are allowed", http.StatusForbidden)
			return
		}
		// Pass control back to the handler
		handler.ServeHTTP(w, r)
	})
}

// Deprecated: we shouldn't have 2 http ports. Will be removed after code using
// this port is removed.
func startMonitor(exporter http.Handler, addr string, mux *http.ServeMux) (*monitor, error) {
	m := &monitor{}

	// get the network stuff setup
	var listener net.Listener
	if addr != "" {
		var err error
		if listener, err = net.Listen("tcp", addr); err != nil {
			return nil, fmt.Errorf("unable to listen on socket: %v", err)
		}
	}

	// NOTE: this is a temporary solution to provide bare-bones debug functionality
	// for pilot. a full design / implementation of self-monitoring and reporting
	// is coming. that design will include proper coverage of statusz/healthz type
	// functionality, in addition to how pilot reports its own metrics.
	addMonitor(exporter, mux)
	if addr != "" {
		m.monitoringServer = &http.Server{
			Addr:        listener.Addr().String(),
			Handler:     mux,
			IdleTimeout: 90 * time.Second, // matches http.DefaultTransport keep-alive timeout
			ReadTimeout: 30 * time.Second,
		}
	}

	version.Info.RecordComponentBuildTag("pilot")
	pilotVersion.With(versionTag.Value(version.Info.String())).Record(1)

	if addr != "" {
		go func() {
			_ = m.monitoringServer.Serve(listener)
		}()
	}

	return m, nil
}

func (m *monitor) Close() error {
	if m.monitoringServer != nil {
		return m.monitoringServer.Close()
	}
	return nil
}

// initMonitor initializes the configuration for the pilot monitoring server.
func (s *Server) initMonitor(addr string) error { // nolint: unparam
	s.addStartFunc("monitoring", func(stop <-chan struct{}) error {
		monitor, err := startMonitor(s.metricsExporter, addr, s.monitoringMux)
		if err != nil {
			return err
		}
		go func() {
			<-stop
			err := monitor.Close()
			log.Debugf("Monitoring server terminated: %v", err)
		}()
		return nil
	})
	return nil
}
