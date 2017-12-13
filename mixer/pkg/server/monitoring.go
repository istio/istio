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

	"github.com/golang/glog"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"istio.io/istio/mixer/pkg/version"
)

type monitor struct {
	monitoringServer *http.Server
	shutdown         chan struct{}
}

const (
	metricsPath = "/metrics"
	versionPath = "/version"
)

func startMonitor(port uint16) (*monitor, error) {
	m := &monitor{
		shutdown: make(chan struct{}),
	}

	// get the network stuff setup
	var listener net.Listener
	var err error
	if listener, err = net.Listen("tcp", fmt.Sprintf(":%d", port)); err != nil {
		return nil, fmt.Errorf("unable to listen on socket: %v", err)
	}

	// NOTE: this is a temporary solution to provide bare-bones debug functionality
	// for mixer. a full design / implementation of self-monitoring and reporting
	// is coming. that design will include proper coverage of statusz/healthz type
	// functionality, in addition to how mixer reports its own metrics.
	mux := http.NewServeMux()
	mux.Handle(metricsPath, promhttp.Handler())
	mux.HandleFunc(versionPath, func(out http.ResponseWriter, req *http.Request) {
		if _, err := out.Write([]byte(version.Info.String())); err != nil {
			glog.Errorf("Unable to write version string: %v", err)
		}
	})

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

func (m *monitor) Close() error {
	_ = m.monitoringServer.Close()
	<-m.shutdown
	return nil
}
