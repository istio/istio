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
	"context"
	"fmt"
	"net"
	"net/http"
	"sync/atomic"

	"istio.io/istio/cni/pkg/constants"
	"istio.io/istio/pkg/monitoring"
	"istio.io/istio/pkg/network"
)

type Probes struct {
	InstallReady *atomic.Value
	WatchReady   *atomic.Value
}

func StartServer(ctx context.Context, port int) (Probes, error) {
	probes := Probes{}
	if port <= 0 {
		return probes, nil
	}
	exporter, err := monitoring.RegisterPrometheusExporter(nil, nil)
	if err != nil {
		log.Errorf("could not set up prometheus exporter: %v", err)
		return probes, err
	}
	mux := http.NewServeMux()
	listener, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		log.Errorf("unable to listen on socket: %v. "+
			"Hint: the istio-cni node agent typically runs in the hostNetwork. "+""+
			"Port conflicts may be caused by other processes on the host utilizing the same port.", err)
		return probes, err
	}
	mux.Handle("/metrics", exporter)
	server := &http.Server{
		Handler: mux,
	}
	go func() {
		if err = server.Serve(listener); network.IsUnexpectedListenerError(err) {
			log.Errorf("error running http server: %s", err)
		}
	}()
	context.AfterFunc(ctx, func() {
		err := server.Close()
		log.Debugf("monitoring server terminated: %v", err)
	})
	router := http.NewServeMux()
	probes.InstallReady = &atomic.Value{}
	probes.InstallReady.Store(false)
	probes.WatchReady = &atomic.Value{}
	probes.WatchReady.Store(false)

	router.HandleFunc(constants.LivenessEndpoint, healthz)
	router.HandleFunc(constants.ReadinessEndpoint, readyz(probes.InstallReady, probes.WatchReady))

	return probes, nil
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
