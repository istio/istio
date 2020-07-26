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

package server

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/pprof"
	"time"

	ocprom "contrib.go.opencensus.io/exporter/prometheus"
	"github.com/prometheus/client_golang/prometheus"
	"go.opencensus.io/stats/view"
	"google.golang.org/grpc/stats"

	"istio.io/pkg/log"
	"istio.io/pkg/version"
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
	registry.MustRegister(prometheus.NewProcessCollector(prometheus.ProcessCollectorOpts{}))
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

	version.Info.RecordComponentBuildTag("mixer")

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

type multiStatsHandler struct {
	handlers []stats.Handler
}

// HandleRPC processes the RPC stats.
func (m *multiStatsHandler) HandleRPC(ctx context.Context, rs stats.RPCStats) {
	for _, h := range m.handlers {
		h.HandleRPC(ctx, rs)
	}
}

// TagRPC can attach some information to the given context.
func (m *multiStatsHandler) TagRPC(ctx context.Context, rti *stats.RPCTagInfo) context.Context {
	c := ctx
	for _, h := range m.handlers {
		c = h.TagRPC(c, rti)
	}
	return c
}

// TagConn can attach some information to the given context.
func (m *multiStatsHandler) TagConn(ctx context.Context, cti *stats.ConnTagInfo) context.Context {
	c := ctx
	for _, h := range m.handlers {
		c = h.TagConn(c, cti)
	}
	return c
}

// HandleConn processes the Conn stats.
func (m *multiStatsHandler) HandleConn(ctx context.Context, cs stats.ConnStats) {
	for _, h := range m.handlers {
		h.HandleConn(ctx, cs)
	}
}

func newMultiStatsHandler(handlers ...stats.Handler) stats.Handler {
	return &multiStatsHandler{handlers: handlers}
}
