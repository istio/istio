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

package forwarder

import (
	"crypto/tls"
	"crypto/x509"
	"net"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"istio.io/istio/pkg/hbone"
	"istio.io/istio/pkg/test/echo/common/scheme"
	"istio.io/istio/pkg/test/echo/proto"
	"istio.io/istio/security/pkg/pki/util"
)

func serve(t *testing.T, s *http.Server) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		_ = s.Serve(l)
	}()
	t.Cleanup(func() {
		_ = l.Close()
	})
	return l.Addr().String()
}

// newH2CServer starts a cleartext HTTP/2 server recording the protocol of the last request.
func newH2CServer(t *testing.T, proto *atomic.Value) string {
	t.Helper()
	protocols := new(http.Protocols)
	protocols.SetHTTP1(true)
	protocols.SetUnencryptedHTTP2(true)
	return serve(t, &http.Server{
		Protocols: protocols,
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			proto.Store(r.Proto)
			w.WriteHeader(http.StatusOK)
		}),
	})
}

// newRecordingHBONEProxy starts an HBONE proxy recording the host of the last CONNECT it served.
func newRecordingHBONEProxy(t *testing.T, connectHost *atomic.Value) string {
	t.Helper()
	s := hbone.NewServer()
	inner := s.Handler
	s.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodConnect {
			connectHost.Store(r.Host)
		}
		inner.ServeHTTP(w, r)
	})
	return serve(t, s)
}

// The h2c transport must route through the configured dialer. net/http only calls DialTLSContext
// for https:// URLs, so wiring the dialer there silently bypasses HBONE (and socks5, UDS, ...)
// while still appearing to work whenever the target is directly reachable.
func TestHTTP2TransportUsesDialer(t *testing.T) {
	var serverProto, connectHost atomic.Value
	target := newH2CServer(t, &serverProto)
	proxy := newRecordingHBONEProxy(t, &connectHost)

	cfg := &Config{
		scheme:  scheme.HTTP,
		Request: &proto.ForwardEchoRequest{Hbone: &proto.HBONE{Address: proxy}},
	}
	getter, closeFn := newHTTP2TransportGetter(cfg)
	defer closeFn()
	rt, done, err := getter()
	if err != nil {
		t.Fatal(err)
	}
	defer done()

	req, err := http.NewRequest(http.MethodGet, "http://"+target, nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := rt.RoundTrip(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("got status %v, want 200", resp.Status)
	}
	if got := serverProto.Load(); got != "HTTP/2.0" {
		t.Fatalf("server saw %v, want HTTP/2.0", got)
	}
	if got := connectHost.Load(); got != target {
		t.Fatalf("HBONE proxy saw CONNECT to %v, want %v", got, target)
	}
}

// The HTTPS variant must also actually speak HTTP/2: net/http disables HTTP/2 whenever a custom
// TLS config or dialer is set, and it will happily use HTTP/1.1 framing on a connection whose
// ALPN negotiated "h2".
func TestHTTP2TransportHTTPS(t *testing.T) {
	certPem, keyPem, err := util.GenCertKeyFromOptions(util.CertOptions{
		Host:         "localhost,127.0.0.1",
		TTL:          time.Hour,
		IsSelfSigned: true,
		IsServer:     true,
		Org:          "istio.io",
		ECSigAlg:     util.EcdsaSigAlg,
	})
	if err != nil {
		t.Fatal(err)
	}
	cert, err := tls.X509KeyPair(certPem, keyPem)
	if err != nil {
		t.Fatal(err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(certPem) {
		t.Fatal("failed to build cert pool")
	}

	var serverProto atomic.Value
	protocols := new(http.Protocols)
	protocols.SetHTTP1(true)
	protocols.SetHTTP2(true)
	s := &http.Server{
		Protocols: protocols,
		TLSConfig: &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS12},
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			serverProto.Store(r.Proto)
			w.WriteHeader(http.StatusOK)
		}),
	}
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		_ = s.ServeTLS(l, "", "")
	}()
	t.Cleanup(func() { _ = l.Close() })

	clientTLS := &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12}
	cfg := &Config{
		scheme:    scheme.HTTPS,
		Request:   &proto.ForwardEchoRequest{},
		tlsConfig: clientTLS,
	}
	getter, closeFn := newHTTP2TransportGetter(cfg)
	defer closeFn()
	rt, done, err := getter()
	if err != nil {
		t.Fatal(err)
	}
	defer done()

	req, err := http.NewRequest(http.MethodGet, "https://"+l.Addr().String(), nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := rt.RoundTrip(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("got status %v, want 200", resp.Status)
	}
	if got := serverProto.Load(); got != "HTTP/2.0" {
		t.Fatalf("server saw %v, want HTTP/2.0", got)
	}
	if clientTLS.NextProtos != nil {
		t.Fatalf("caller tls.Config was mutated: NextProtos=%v", clientTLS.NextProtos)
	}
}
