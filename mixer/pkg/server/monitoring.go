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

package server

import (
	"fmt"
	"net"
	"net/http"
	"net/http/pprof"
	"os"
	"time" // "github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/prometheus/client_golang/prometheus"
	ocprom "go.opencensus.io/exporter/prometheus"
	"go.opencensus.io/stats/view"

	"istio.io/istio/pkg/log"
	"istio.io/istio/pkg/version"
)

type monitor struct {
	monitoringServer *http.Server
	// This channel is closed after the server stops serving requests.
	closed chan struct{}
}

const (
	metricsPath = "/metrics"
	versionPath = "/version"
)

func startMonitor(port uint16, enableProfiling bool, lf listenFunc) (*monitor, error) {
	m := &monitor{
		closed: make(chan struct{}),
	}

	// get the network stuff setup
	var listener net.Listener
	var err error
	if listener, err = lf("tcp", fmt.Sprintf(":%d", port)); err != nil {
		return nil, fmt.Errorf("unable to listen on socket: %v", err)
	}

	// NOTE: this is a temporary solution to provide bare-bones debug functionality
	// for mixer. a full design / implementation of self-monitoring and reporting
	// is coming. that design will include proper coverage of statusz/healthz type
	// functionality, in addition to how mixer reports its own metrics.
	mux := http.NewServeMux()

	registry := prometheus.NewRegistry()
	registry.MustRegister(prometheus.NewProcessCollector(os.Getpid(), ""))
	registry.MustRegister(prometheus.NewGoCollector())

	exporter, err := ocprom.NewExporter(ocprom.Options{Registry: registry})
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

	go func() {
		_ = m.monitoringServer.Serve(listener)
		close(m.closed)
	}()

	return m, nil
}

func (m *monitor) Close() error {
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
