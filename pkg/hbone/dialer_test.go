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
	"crypto/tls"
	"crypto/x509"
	"net"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"istio.io/istio/security/pkg/pki/util"
)

func newTCPServer(t testing.TB, data string) string {
	n, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("opened listener on %v", n.Addr().String())
	go func() {
		for {
			c, err := n.Accept()
			if err != nil {
				log.Info(err)
				return
			}
			log.Info("accepted connection")
			c.Write([]byte(data))
			c.Close()
		}
	}()
	t.Cleanup(func() {
		n.Close()
	})
	return n.Addr().String()
}

func TestDialerError(t *testing.T) {
	timeout := 500 * time.Millisecond
	d := NewDialer(Config{
		ProxyAddress: "127.0.0.10:1", // Random address that should fail to dial
		Headers: map[string][]string{
			"some-addition-metadata": {"test-value"},
		},
		TLS:     nil, // No TLS for simplification
		Timeout: &timeout,
	})

	_, err := d.Dial("tcp", "fake")
	if err == nil {
		t.Fatal("expected error, got none.")
	}
}

func TestDialer(t *testing.T) {
	timeout := 500 * time.Millisecond
	testAddr := newTCPServer(t, "hello")
	proxy := newHBONEServer(t)
	d := NewDialer(Config{
		ProxyAddress: proxy,
		Headers: map[string][]string{
			"some-addition-metadata": {"test-value"},
		},
		TLS:     nil, // No TLS for simplification
		Timeout: &timeout,
	})
	send := func() {
		client, err := d.Dial("tcp", testAddr)
		if err != nil {
			t.Fatal(err)
		}
		defer client.Close()

		go func() {
			n, err := client.Write([]byte("hello world"))
			log.Infof("wrote %v/%v", n, err)
		}()

		buf := make([]byte, 8)
		n, err := client.Read(buf)
		if err != nil {
			t.Fatalf("err with %v: %v", n, err)
		}
		if string(buf[:n]) != "hello" {
			t.Fatalf("got unexpected buffer: %v", string(buf[:n]))
		}
		t.Logf("Read %v", string(buf[:n]))
	}
	// Make sure we can create multiple connections
	send()
	send()
}

func newHBONEServer(t *testing.T) string {
	return startHBONEServer(t, nil).addr
}

// testServer is an HBONE server that records what it saw on the last CONNECT it handled.
type testServer struct {
	addr   string
	proto  atomic.Value // string, e.g. "HTTP/2.0"
	header atomic.Value // http.Header
}

// startHBONEServer starts an HBONE server, over TLS if tlsConfig is non-nil.
func startHBONEServer(t *testing.T, tlsConfig *tls.Config) *testServer {
	t.Helper()
	s := NewServer()
	ts := &testServer{}
	inner := s.Handler
	s.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ts.proto.Store(r.Proto)
		ts.header.Store(r.Header.Clone())
		inner.ServeHTTP(w, r)
	})
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ts.addr = l.Addr().String()
	serve := s.Serve
	if tlsConfig != nil {
		s.TLSConfig = tlsConfig
		serve = func(l net.Listener) error { return s.ServeTLS(l, "", "") }
	}
	go func() {
		_ = serve(l)
	}()
	t.Cleanup(func() {
		_ = l.Close()
	})
	return ts
}

// newTestCerts returns a server TLS config and the pool needed to verify it. The certificate is
// valid for "localhost" and 127.0.0.1.
func newTestCerts(t *testing.T) (*tls.Config, *x509.CertPool) {
	t.Helper()
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
	return &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS12}, pool
}

// alpnRecorder returns a VerifyConnection hook storing the ALPN protocol the client negotiated.
func alpnRecorder(got *atomic.Value) func(tls.ConnectionState) error {
	return func(cs tls.ConnectionState) error {
		got.Store(cs.NegotiatedProtocol)
		return nil
	}
}

// checkTunnel verifies that a connection dialed through HBONE reaches the echo server.
func checkTunnel(t *testing.T, c net.Conn) {
	t.Helper()
	buf := make([]byte, 8)
	n, err := c.Read(buf)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if got := string(buf[:n]); got != "hello" {
		t.Fatalf("got unexpected buffer: %q", got)
	}
}

// HBONE requires HTTP/2 CONNECT. net/http silently downgrades to HTTP/1.1 unless the transport
// opts in explicitly, so assert the protocol actually used on the wire.
func TestDialerUsesHTTP2(t *testing.T) {
	testAddr := newTCPServer(t, "hello")

	t.Run("h2c", func(t *testing.T) {
		proxy := startHBONEServer(t, nil)
		d := NewDialer(Config{ProxyAddress: proxy.addr})
		c, err := d.Dial("tcp", testAddr)
		if err != nil {
			t.Fatal(err)
		}
		defer c.Close()
		checkTunnel(t, c)
		if got := proxy.proto.Load(); got != "HTTP/2.0" {
			t.Fatalf("server saw %v, want HTTP/2.0", got)
		}
	})

	t.Run("tls", func(t *testing.T) {
		serverTLS, pool := newTestCerts(t)
		proxy := startHBONEServer(t, serverTLS)
		alpn := atomic.Value{}
		d := NewDialer(Config{
			ProxyAddress: proxy.addr,
			TLS: &tls.Config{
				RootCAs:          pool,
				MinVersion:       tls.VersionTLS12,
				VerifyConnection: alpnRecorder(&alpn),
			},
		})
		c, err := d.Dial("tcp", testAddr)
		if err != nil {
			t.Fatal(err)
		}
		defer c.Close()
		checkTunnel(t, c)
		if got := proxy.proto.Load(); got != "HTTP/2.0" {
			t.Fatalf("server saw %v, want HTTP/2.0", got)
		}
		if got := alpn.Load(); got != "h2" {
			t.Fatalf("client negotiated ALPN %v, want h2", got)
		}
	})
}

// The caller's tls.Config must not be mutated by the transport's HTTP/2 setup.
func TestDialerDoesNotMutateTLSConfig(t *testing.T) {
	cfg := &tls.Config{MinVersion: tls.VersionTLS12}
	NewDialer(Config{ProxyAddress: "127.0.0.1:15008", TLS: cfg})
	if cfg.NextProtos != nil {
		t.Fatalf("caller tls.Config was mutated: NextProtos=%v", cfg.NextProtos)
	}
}

func TestDialerHeaders(t *testing.T) {
	testAddr := newTCPServer(t, "hello")
	proxy := startHBONEServer(t, nil)
	d := NewDialer(Config{
		ProxyAddress: proxy.addr,
		Headers:      map[string][]string{"some-addition-metadata": {"test-value"}},
	})
	c, err := d.Dial("tcp", testAddr)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	checkTunnel(t, c)
	hdr, _ := proxy.header.Load().(http.Header)
	if got := hdr.Get("some-addition-metadata"); got != "test-value" {
		t.Fatalf("got header %q, want test-value (headers: %v)", got, hdr)
	}
}
