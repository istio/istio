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

package monitoring

import (
	"fmt"
	"net"
	"net/http"

	ocprom "contrib.go.opencensus.io/exporter/prometheus"
	"github.com/prometheus/client_golang/prometheus"
	"go.opencensus.io/stats/view"

	"istio.io/pkg/log"
)

func SetupMonitoring(port int, path string, stop <-chan struct{}) {
	if port <= 0 {
		return
	}
	mux := http.NewServeMux()
	var listener net.Listener
	var err error
	if listener, err = net.Listen("tcp", fmt.Sprintf(":%d", port)); err != nil {
		log.Errorf("unable to listen on socket: %v", err)
		return
	}
	exporter, err := ocprom.NewExporter(ocprom.Options{Registry: prometheus.DefaultRegisterer.(*prometheus.Registry)})
	if err != nil {
		log.Errorf("could not set up prometheus exporter: %v", err)
		return
	}
	view.RegisterExporter(exporter)
	mux.Handle(path, exporter)
	monitoringServer := &http.Server{
		Handler: mux,
	}
	go func() {
		err = monitoringServer.Serve(listener)
		if err != nil {
			log.Errorf("error running monitoring http server: %s", err)
		}
	}()
	go func() {
		<-stop
		err := monitoringServer.Close()
		log.Debugf("monitoring server terminated: %v", err)
	}()
}
