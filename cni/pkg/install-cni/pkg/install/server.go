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

package install

import (
	"net/http"
	"sync/atomic"

	"istio.io/istio/cni/pkg/install-cni/pkg/constants"
)

// StartServer initializes and starts a web server that exposes liveness and readiness endpoints at port 8000.
func StartServer() *atomic.Value {
	router := http.NewServeMux()
	isReady := initRouter(router)

	go func() {
		_ = http.ListenAndServe(":"+constants.Port, router)
	}()

	return isReady
}

// Sets isReady to true.
func SetReady(isReady *atomic.Value) {
	isReady.Store(true)
}

// Sets isReady to false.
func SetNotReady(isReady *atomic.Value) {
	isReady.Store(false)
}

func initRouter(router *http.ServeMux) *atomic.Value {
	isReady := &atomic.Value{}
	isReady.Store(false)

	router.HandleFunc(constants.LivenessEndpoint, healthz)
	router.HandleFunc(constants.ReadinessEndpoint, readyz(isReady))

	return isReady
}

func healthz(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
}

func readyz(isReady *atomic.Value) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		if isReady == nil || !isReady.Load().(bool) {
			http.Error(w, http.StatusText(http.StatusServiceUnavailable), http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	}
}
