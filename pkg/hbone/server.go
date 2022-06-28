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

package hbone

import (
	"context"
	"net"
	"net/http"
	"sync"
	"time"

	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"
)

func NewServer() *http.Server {
	// Need to set this to allow timeout on the read header
	h1 := &http.Transport{
		ExpectContinueTimeout: 3 * time.Second,
	}
	h2, _ := http2.ConfigureTransports(h1)
	h2.ReadIdleTimeout = 10 * time.Minute // TODO: much larger to support long-lived connections
	h2.AllowHTTP = true
	h2Server := &http2.Server{}
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodConnect {
			if handleConnect(w, r) {
				return
			}
		} else {
			log.Errorf("non-CONNECT: %v", r.Method)
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	})
	hs := &http.Server{
		Handler: h2c.NewHandler(handler, h2Server),
	}
	return hs
}

func handleConnect(w http.ResponseWriter, r *http.Request) bool {
	t0 := time.Now()
	log.WithLabels("host", r.Host, "source", r.RemoteAddr).Info("Received CONNECT")
	// Send headers back immediately so we can start getting the body
	w.(http.Flusher).Flush()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	dst, err := (&net.Dialer{}).DialContext(ctx, "tcp", r.Host)
	if err != nil {
		w.WriteHeader(http.StatusServiceUnavailable)
		log.Errorf("failed to dial upstream: %v", err)
		return true
	}
	log.Infof("Connected to %v", r.Host)
	w.WriteHeader(http.StatusOK)

	wg := sync.WaitGroup{}
	wg.Add(1)
	go func() {
		// downstream (hbone client) <-- upstream (app)
		copyBuffered(w, dst, log.WithLabels("name", "dst to w"))
		r.Body.Close()
		wg.Done()
	}()
	// downstream (hbone client) --> upstream (app)
	copyBuffered(dst, r.Body, log.WithLabels("name", "body to dst"))
	wg.Wait()
	log.Infof("connection closed in %v", time.Since(t0))
	return false
}
