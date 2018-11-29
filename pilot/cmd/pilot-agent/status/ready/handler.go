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

package ready

import (
	"net/http"
	"sync"

	"istio.io/istio/pkg/log"
)

const (
	path = "/healthz/ready"
)

// RegisterHTTPHandlerFunc for the HTTP handler function for the readiness probe.
func RegisterHTTPHandlerFunc(handler *http.ServeMux, proxyAdminPort uint16, applicationPorts []uint16) {
	probe := &Probe{
		ProxyAdminPort:   proxyAdminPort,
		ApplicationPorts: applicationPorts,
	}

	mutex := sync.Mutex{}
	lastProbeSuccessful := false

	handler.HandleFunc(path, func(w http.ResponseWriter, _ *http.Request) {
		err := probe.Check()

		mutex.Lock()
		defer mutex.Unlock()

		if err != nil {
			w.WriteHeader(http.StatusServiceUnavailable)

			log.Infof("Envoy proxy is NOT ready: %s", err.Error())
			lastProbeSuccessful = false
		} else {
			w.WriteHeader(http.StatusOK)

			if !lastProbeSuccessful {
				log.Info("Envoy proxy is ready")
			}
			lastProbeSuccessful = true
		}
	})
}
