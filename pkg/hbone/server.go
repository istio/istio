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
	"crypto/tls"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/cbeuw/connutil"
	"golang.org/x/net/http2"

	"istio.io/istio/pkg/h2c"
)

func NewDoubleHBONEServer(tlsConfig *tls.Config) *http.Server {
	return newServer(func(t *tls.Config) func(http.ResponseWriter, *http.Request) bool {
		return func(w http.ResponseWriter, r *http.Request) bool {
			return handleDoubleConnect(w, r, t)
		}
	}(tlsConfig))
}

func NewServer() *http.Server {
	return newServer(handleConnect)
}

func newServer(handleFunc func(http.ResponseWriter, *http.Request) bool) *http.Server {
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
			if handleFunc(w, r) {
				return
			}
		} else {
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	})
	return &http.Server{
		Handler: h2c.NewHandler(handler, h2Server),
	}
}

func handleDoubleConnect(w http.ResponseWriter, r *http.Request, tlsConfig *tls.Config) bool {
	t0 := time.Now()
	log.WithLabels("host", r.Host, "source", r.RemoteAddr).Info("Received Double CONNECT")
	// Send headers back immediately so we can start getting the body
	w.(http.Flusher).Flush()

	// TODO: Remove this check; this is Istio specific context
	host, _, err := net.SplitHostPort(r.Host)
	if err != nil {
		log.Errorf("invalid host header: %v", r.Host)
		w.WriteHeader(http.StatusBadRequest)
		return true
	}
	if net.ParseIP(host) != nil {
		// If the host header is an IP address, this is invalid; we only support hostnames in double hbone
		// TODO: Evaluate if we still need this constraint later
		log.Errorf("invalid host header: %v. Must be a hostname", r.Host)
		w.WriteHeader(http.StatusBadRequest)
		return true
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	innerServer := newServer(handleConnect)
	if tlsConfig != nil {
		innerServer.TLSConfig = tlsConfig
	} else {
		log.Info("Using plaintext for inner HBONE server")
	}

	dialer, listener := connutil.DialerListener(128)
	go func() {
		err := innerServer.ServeTLS(listener, "", "")
		if err != nil {
			log.Errorf("failed to start intermediate http server: %v", err)
		}
	}()

	defer func() {
		if innerServer.Shutdown(ctx) != nil {
			log.Errorf("failed to shutdown inner server: %v", err)
		}
	}()
	log.Info("Started inner HBONE server and piping data to it")

	clientConn, err := dialer.Dial("", "")
	if err != nil {
		log.Errorf("failed to dial inner server: %v", err)
		return true
	}

	w.WriteHeader(http.StatusOK)
	wg := sync.WaitGroup{}
	wg.Add(1)
	go func() {
		// downstream (hbone client) <-- upstream (single hbone server)
		copyBuffered(w, clientConn, log.WithLabels("name", "pipe to outer client"))
		err := r.Body.Close()
		if err != nil {
			log.Infof("connection to hbone client is not closed: %v", err)
		}
		wg.Done()
	}()

	// downstream (hbone client) --> upstream (app)
	copyBuffered(clientConn, r.Body, log.WithLabels("name", "body to inner server"))
	wg.Wait()
	log.Infof("connection closed in %v", time.Since(t0))
	return false
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
		err = r.Body.Close()
		if err != nil {
			log.Infof("connection to hbone client is not closed: %v", err)
		}
		wg.Done()
	}()

	// downstream (hbone client) --> upstream (app)
	copyBuffered(dst, r.Body, log.WithLabels("name", "body to dst"))
	wg.Wait()
	log.Infof("connection closed in %v", time.Since(t0))
	return false
}
