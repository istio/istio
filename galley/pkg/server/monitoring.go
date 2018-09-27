// Copyright 2018 Istio Authors
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

	ocprom "go.opencensus.io/exporter/prometheus"
	"istio.io/istio/pkg/log"
	"istio.io/istio/pkg/version"
	"github.com/prometheus/client_golang/prometheus"
	"go.opencensus.io/stats/view"
)

const (
	metricsPath = "/metrics"
	versionPath = "/version"
)

//StartSelfMonitoring start the self monitoring for Galley
func StartSelfMonitoring(stop <-chan struct{}, port uint) {
	lis, err := net.Listen("tcp", fmt.Sprintf(":%v", port))
	if err != nil {
		log.Errorf("Unable to listen on monitoring port %v: %v", port, err)
		return
	}

	mux := http.NewServeMux()

	registry := prometheus.DefaultRegisterer.(*prometheus.Registry)
	exporter, err := ocprom.NewExporter(ocprom.Options{Registry: registry})
	if err != nil {
		log.Errorf("could not set up prometheus exporter: %v", err)
	} else {
		view.RegisterExporter(exporter)
		mux.Handle(metricsPath, exporter)
	}
	mux.HandleFunc(versionPath, func(out http.ResponseWriter, req *http.Request) {
		if _, err := out.Write([]byte(version.Info.String())); err != nil {
			log.Errorf("Unable to write version string: %v", err)
		}
	})

	server := &http.Server{
		Handler: mux,
	}

	go func() {
		if err := server.Serve(lis); err != nil {
			log.Errorf("Monitoring http server failed: %v", err)
			return
		}
	}()

	<-stop
	err = server.Close()
	log.Debugf("Monitoring server terminated: %v", err)
}
