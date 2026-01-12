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

package nodeagent

import (
	"net/http"
	"sync/atomic"

	"istio.io/istio/cni/pkg/constants"
)

// StartHealthServer initializes and starts a web server that exposes liveness and readiness endpoints at port 8000.
func StartHealthServer() (installReady *atomic.Value, watchReady *atomic.Value) {
	router := http.NewServeMux()
	installReady, watchReady = initRouter(router)

	go func() {
		_ = http.ListenAndServe(":"+constants.ReadinessPort, router)
	}()

	return installReady, watchReady
}

func initRouter(router *http.ServeMux) (installReady *atomic.Value, watchReady *atomic.Value) {
	installReady = &atomic.Value{}
	watchReady = &atomic.Value{}
	installReady.Store(false)
	watchReady.Store(false)

	router.HandleFunc(constants.LivenessEndpoint, healthz)
	router.HandleFunc(constants.ReadinessEndpoint, readyz(installReady, watchReady))

	return installReady, watchReady
}

func healthz(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
}

func readyz(installReady, watchReady *atomic.Value) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		if !installReady.Load().(bool) || !watchReady.Load().(bool) {
			http.Error(w, http.StatusText(http.StatusServiceUnavailable), http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	}
}
