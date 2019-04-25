/*
Copyright The Helm Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package main

import (
	"net/http"

	"github.com/prometheus/client_golang/prometheus/promhttp"
)

func readinessProbe(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
}

func livenessProbe(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
}

func newProbesMux() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("/readiness", readinessProbe)
	mux.HandleFunc("/liveness", livenessProbe)
	return mux
}

func addPrometheusHandler(mux *http.ServeMux) {
	// Register HTTP handler for the global Prometheus registry.
	mux.Handle("/metrics", promhttp.Handler())
}
