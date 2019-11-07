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
	"net/http/pprof"
	"sync"

	"istio.io/istio/galley/pkg/server/process"
)

// NewProfiling returns a new profiling component.
func NewProfiling(port uint) process.Component {

	var lis net.Listener
	var server *http.Server

	return process.ComponentFromFns(
		// start
		func() error {
			var err error
			if lis, err = netListen("tcp", fmt.Sprintf(":%v", port)); err != nil {
				err = fmt.Errorf("unable to listen on profiling port %v: %v", port, err)
				return err
			}

			mux := http.NewServeMux()
			mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
				//Redirect ROOT to /debug/pprof/ for convenience
				http.Redirect(w, r, "/debug/pprof/", http.StatusSeeOther)
			})
			mux.HandleFunc("/debug/pprof/", pprof.Index)
			mux.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
			mux.HandleFunc("/debug/pprof/profile", pprof.Profile)
			mux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
			mux.HandleFunc("/debug/pprof/trace", pprof.Trace)

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
					scope.Debugf("Profiling http server done: %v", err)
					return
				}
			}()

			wg.Wait() // Wait until the go-routine starts before returning control
			return nil
		},
		// stop
		func() {
			if server != nil {
				if err := server.Close(); err != nil {
					scope.Errorf("Monitoring server terminated with error: %v", err)
				}
				server = nil
				lis = nil // listener is closed by the server
			}

			if lis != nil {
				if err := lis.Close(); err != nil {
					scope.Errorf("Listener terminated with error: %v", err)
				}
				lis = nil
			}
		})
}
