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
	"net"
	"sync/atomic"
	"testing"
)

// unroutableProxy is a syntactically valid proxy address that can never be dialed: ".invalid" is
// reserved by RFC 2606 and never resolves. The inner leg of a double HBONE connection is tunneled
// over the outer leg, so its configured proxy address is only ever used to build the request URL.
// If the inner transport regresses to dialing it directly, these tests fail loudly instead of
// silently bypassing the outer tunnel.
const unroutableProxy = "inner-proxy.invalid:15008"

// startDoubleHBONEServer starts a double HBONE server, whose inner server uses tlsConfig if set.
func startDoubleHBONEServer(t *testing.T, tlsConfig *tls.Config) string {
	t.Helper()
	s := NewDoubleHBONEServer(tlsConfig)
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

// hostnameTarget rewrites 127.0.0.1:port to localhost:port. The double HBONE server rejects
// CONNECT requests whose host header is a bare IP.
func hostnameTarget(t *testing.T, addr string) string {
	t.Helper()
	_, port, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatal(err)
	}
	return net.JoinHostPort("localhost", port)
}

func TestDoubleDialerPlaintext(t *testing.T) {
	target := hostnameTarget(t, newTCPServer(t, "hello"))
	proxy := startDoubleHBONEServer(t, nil)

	d := NewDoubleDialer(
		Config{ProxyAddress: proxy},
		Config{ProxyAddress: unroutableProxy},
		nil,
	)
	c, err := d.Dial("tcp", target)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	checkTunnel(t, c)
}

func TestDoubleDialerInnerTLS(t *testing.T) {
	target := hostnameTarget(t, newTCPServer(t, "hello"))
	serverTLS, pool := newTestCerts(t)
	proxy := startDoubleHBONEServer(t, serverTLS)

	alpn := atomic.Value{}
	innerTLS := &tls.Config{
		RootCAs:    pool,
		ServerName: "localhost",
		MinVersion: tls.VersionTLS12,
		// The inner connection is handed to net/http by DialTLSContext, so nothing else can
		// assert on it; record what ALPN actually selected.
		VerifyConnection: alpnRecorder(&alpn),
	}
	d := NewDoubleDialer(
		Config{ProxyAddress: proxy},
		Config{ProxyAddress: unroutableProxy, TLS: innerTLS},
		innerTLS,
	)
	c, err := d.Dial("tcp", target)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	checkTunnel(t, c)
	if got := alpn.Load(); got != "h2" {
		t.Fatalf("inner connection negotiated ALPN %v, want h2", got)
	}
}

// The inner TLS config is supplied by the caller; the dialer must not require it to already carry
// HBONE specifics like an "h2" ALPN or an explicit ServerName.
func TestDoubleDialerInnerTLSDefaults(t *testing.T) {
	target := hostnameTarget(t, newTCPServer(t, "hello"))
	serverTLS, pool := newTestCerts(t)
	proxy := startDoubleHBONEServer(t, serverTLS)

	// No NextProtos and no ServerName: ServerName must be inferred from the proxy address.
	innerTLS := &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12}
	d := NewDoubleDialer(
		Config{ProxyAddress: proxy},
		Config{ProxyAddress: hostnameTarget(t, proxy), TLS: innerTLS},
		innerTLS,
	)
	c, err := d.Dial("tcp", target)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	checkTunnel(t, c)
	if innerTLS.NextProtos != nil || innerTLS.ServerName != "" {
		t.Fatalf("caller tls.Config was mutated: %v %q", innerTLS.NextProtos, innerTLS.ServerName)
	}
}

func TestDoubleDialerOuterTLS(t *testing.T) {
	target := hostnameTarget(t, newTCPServer(t, "hello"))
	serverTLS, pool := newTestCerts(t)

	alpn := atomic.Value{}
	outerTLS := &tls.Config{
		RootCAs:          pool,
		MinVersion:       tls.VersionTLS12,
		VerifyConnection: alpnRecorder(&alpn),
	}
	// Serve the outer leg over TLS too, reusing the same certificate for the inner server.
	s := NewDoubleHBONEServer(serverTLS)
	s.TLSConfig = serverTLS
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		_ = s.ServeTLS(l, "", "")
	}()
	t.Cleanup(func() { _ = l.Close() })

	innerTLS := &tls.Config{RootCAs: pool, ServerName: "localhost", MinVersion: tls.VersionTLS12}
	d := NewDoubleDialer(
		Config{ProxyAddress: l.Addr().String(), TLS: outerTLS},
		Config{ProxyAddress: unroutableProxy, TLS: innerTLS},
		innerTLS,
	)
	c, err := d.Dial("tcp", target)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	checkTunnel(t, c)
	if got := alpn.Load(); got != "h2" {
		t.Fatalf("outer connection negotiated ALPN %v, want h2", got)
	}
}
