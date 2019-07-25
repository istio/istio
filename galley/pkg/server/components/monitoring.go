// Copyright 2019 Istio Authors
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

package components

import (
	"fmt"
	"net"
	"net/http"
	"sync"

	ocprom "contrib.go.opencensus.io/exporter/prometheus"
	"github.com/prometheus/client_golang/prometheus"
	"go.opencensus.io/stats/view"

	"istio.io/pkg/version"

	"istio.io/istio/galley/pkg/server/process"
)

const (
	metricsPath = "/metrics"
	versionPath = "/version"
)

var (
	exporterMu sync.Mutex
	exporter   *ocprom.Exporter
)

func setupExporter() (*ocprom.Exporter, error) {
	exporterMu.Lock()
	defer exporterMu.Unlock()
	if exporter == nil {
		registry := prometheus.DefaultRegisterer.(*prometheus.Registry)
		var err error
		if exporter, err = ocprom.NewExporter(ocprom.Options{Registry: registry}); err != nil {
			err = fmt.Errorf("could not set up prometheus exporter: %v", err)
			return nil, err
		}
		view.RegisterExporter(exporter)
	}

	return exporter, nil
}

// NewMonitoring returns a new monitoring component.
func NewMonitoring(port uint) process.Component {

	var lis net.Listener
	var server *http.Server

	return process.ComponentFromFns(
		// start
		func() error {
			var err error
			if lis, err = netListen("tcp", fmt.Sprintf(":%v", port)); err != nil {
				err = fmt.Errorf("unable to listen on monitoring port %v: %v", port, err)
				return err
			}

			exp, err := setupExporter()
			if err != nil {
				return err
			}

			mux := http.NewServeMux()
			mux.Handle(metricsPath, exp)
			mux.HandleFunc(versionPath, func(out http.ResponseWriter, req *http.Request) {
				if _, err := out.Write([]byte(version.Info.String())); err != nil {
					scope.Errorf("Unable to write version string: %v", err)
				}
			})

			version.Info.RecordComponentBuildTag("galley")

			server = &http.Server{
				Handler: mux,
			}

			var wg sync.WaitGroup
			wg.Add(1)

			go func() {
				s := server
				l := lis
				wg.Done()

				if err := s.Serve(l); err != nil {
					scope.Debugf("Monitoring http server done: %v", err)
					return
				}
			}()

			wg.Wait()
			return nil
		},
		// stop
		func() {
			// must be called under lock
			if server != nil {
				if err := server.Close(); err != nil {
					scope.Errorf("Monitoring server terminated with error: %v", err)
				}
				server = nil
				lis = nil // listener is closed by the server
			}

			// listener may not be nil if the initialization failed before the server could be
			// started.
			if lis != nil {
				if err := lis.Close(); err != nil {
					scope.Errorf("Listener terminated with error: %v", err)
				}
				lis = nil
			}
		})
}
