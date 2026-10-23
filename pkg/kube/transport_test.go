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

package kube

import (
	"encoding/pem"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	utilnet "k8s.io/apimachinery/pkg/util/net"
	"k8s.io/client-go/rest"

	"istio.io/istio/pkg/test/util/assert"
)

const testHeaderTimeout = 200 * time.Millisecond

func setTestHeaderTimeout(t *testing.T) {
	old := kubeHeaderTimeout
	kubeHeaderTimeout = testHeaderTimeout
	t.Cleanup(func() { kubeHeaderTimeout = old })
}

// newHeaderTimeoutServer returns an HTTP/2 server and a config for it with SetRestDefaults applied.
// /slow-headers never sends headers. /stream sends headers at once, then streams the body
// for longer than the header timeout.
func newHeaderTimeoutServer(t *testing.T) (*rest.Config, *atomic.Bool) {
	var h2 atomic.Bool
	mux := http.NewServeMux()
	mux.HandleFunc("/slow-headers", func(w http.ResponseWriter, r *http.Request) {
		h2.Store(r.ProtoMajor == 2)
		<-r.Context().Done()
	})
	mux.HandleFunc("/stream", func(w http.ResponseWriter, r *http.Request) {
		h2.Store(r.ProtoMajor == 2)
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		for i := 0; i < 5; i++ {
			select {
			case <-time.After(testHeaderTimeout / 2):
			case <-r.Context().Done():
				return
			}
			fmt.Fprintln(w, i)
			w.(http.Flusher).Flush()
		}
	})
	srv := httptest.NewUnstartedServer(mux)
	srv.EnableHTTP2 = true
	srv.StartTLS()
	t.Cleanup(srv.Close)
	ca := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: srv.Certificate().Raw})
	return SetRestDefaults(&rest.Config{Host: srv.URL, TLSClientConfig: rest.TLSClientConfig{CAData: ca}}), &h2
}

func TestResponseHeaderTimeout(t *testing.T) {
	setTestHeaderTimeout(t)
	cfg, h2 := newHeaderTimeoutServer(t)
	hc, err := rest.HTTPClientFor(cfg)
	assert.NoError(t, err)

	_, err = hc.Get(cfg.Host + "/slow-headers")
	if err == nil || !strings.Contains(err.Error(), headerTimeoutMessage) {
		t.Fatalf("expected a header timeout, got %v", err)
	}
	assert.Equal(t, h2.Load(), true)

	// The timeout stops once headers arrive, so a body that streams past it is not cut.
	resp, err := hc.Get(cfg.Host + "/stream")
	assert.NoError(t, err)
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	assert.NoError(t, err)
	assert.Equal(t, strings.Count(string(body), "\n"), 5)
}

// TestResponseHeaderTimeoutSharedTransports verifies the timeout is never set on a transport
// that other clients share: client-go's cached transports or http.DefaultTransport.
func TestResponseHeaderTimeoutSharedTransports(t *testing.T) {
	setTestHeaderTimeout(t)
	cfg, _ := newHeaderTimeoutServer(t)

	rt, err := rest.TransportFor(cfg)
	assert.NoError(t, err)
	assert.Equal(t, baseTransport(t, rt).ResponseHeaderTimeout, testHeaderTimeout)

	// Same TLS material without SetRestDefaults: client-go's cached transport.
	plain := &rest.Config{Host: cfg.Host, TLSClientConfig: cfg.TLSClientConfig}
	rt, err = rest.TransportFor(plain)
	assert.NoError(t, err)
	assert.Equal(t, baseTransport(t, rt).ResponseHeaderTimeout, time.Duration(0))

	// Without TLS, client-go would otherwise hand out http.DefaultTransport.
	rt, err = rest.TransportFor(SetRestDefaults(&rest.Config{Host: "http://127.0.0.1:1"}))
	assert.NoError(t, err)
	if baseTransport(t, rt) == http.DefaultTransport {
		t.Fatal("got http.DefaultTransport")
	}
	assert.Equal(t, http.DefaultTransport.(*http.Transport).ResponseHeaderTimeout, time.Duration(0))
}

// baseTransport returns the *http.Transport under client-go's wrappers.
func baseTransport(t *testing.T, rt http.RoundTripper) *http.Transport {
	t.Helper()
	for rt != nil {
		switch tt := rt.(type) {
		case *http.Transport:
			return tt
		case utilnet.RoundTripperWrapper:
			rt = tt.WrappedRoundTripper()
		default:
			t.Fatalf("unexpected round tripper %T", rt)
		}
	}
	t.Fatal("no *http.Transport found")
	return nil
}
