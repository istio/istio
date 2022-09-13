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
	"bytes"
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/lucas-clemente/quic-go"
	"github.com/lucas-clemente/quic-go/http3"
	"golang.org/x/net/http2"

	"istio.io/istio/pkg/hbone"
	"istio.io/istio/pkg/test/echo"
	"istio.io/istio/pkg/test/echo/common/scheme"
	"istio.io/istio/pkg/test/echo/proto"
)

var _ protocol = &httpProtocol{}

type httpProtocol struct {
	e *executor
}

func newHTTPProtocol(e *executor) *httpProtocol {
	return &httpProtocol{e: e}
}

type httpTransportGetter func() (http.RoundTripper, func(), error)

func (c *httpProtocol) ForwardEcho(ctx context.Context, cfg *Config) (*proto.ForwardEchoResponse, error) {
	var getTransport httpTransportGetter
	var closeSharedTransport func()

	switch {
	case cfg.Request.Http3:
		getTransport, closeSharedTransport = newHTTP3TransportGetter(cfg)
	case cfg.Request.Http2:
		getTransport, closeSharedTransport = newHTTP2TransportGetter(cfg)
	default:
		getTransport, closeSharedTransport = newHTTPTransportGetter(cfg)
	}

	defer closeSharedTransport()

	call := &httpCall{
		httpProtocol: c,
		getTransport: getTransport,
	}

	return doForward(ctx, cfg, c.e, call.makeRequest)
}

func newHTTP3TransportGetter(cfg *Config) (httpTransportGetter, func()) {
	newConn := func() *http3.RoundTripper {
		return &http3.RoundTripper{
			TLSClientConfig: cfg.tlsConfig,
			QuicConfig:      &quic.Config{},
		}
	}
	closeFn := func(conn *http3.RoundTripper) func() {
		return func() {
			_ = conn.Close()
		}
	}
	noCloseFn := func() {}

	if cfg.newConnectionPerRequest {
		// Create a new transport (i.e. connection) for each request.
		return func() (http.RoundTripper, func(), error) {
			conn := newConn()
			return conn, closeFn(conn), nil
		}, noCloseFn
	}

	// Re-use the same transport for all requests. For HTTP3, this should result
	// in multiplexing all requests over the same connection.
	conn := newConn()
	return func() (http.RoundTripper, func(), error) {
		return conn, noCloseFn, nil
	}, closeFn(conn)
}

func newHTTP2TransportGetter(cfg *Config) (httpTransportGetter, func()) {
	newConn := func() *http2.Transport {
		if cfg.scheme == scheme.HTTPS {
			return &http2.Transport{
				TLSClientConfig: cfg.tlsConfig,
				DialTLS: func(network, addr string, tlsConfig *tls.Config) (net.Conn, error) {
					return hbone.TLSDialWithDialer(newDialer(cfg), network, addr, tlsConfig)
				},
			}
		}

		return &http2.Transport{
			// Golang doesn't have first class support for h2c, so we provide some workarounds
			// See https://www.mailgun.com/blog/http-2-cleartext-h2c-client-example-go/
			// So http2.Transport doesn't complain the URL scheme isn't 'https'
			AllowHTTP: true,
			// Pretend we are dialing a TLS endpoint. (Note, we ignore the passed tls.Config)
			DialTLS: func(network, addr string, _ *tls.Config) (net.Conn, error) {
				return newDialer(cfg).Dial(network, addr)
			},
		}
	}
	closeFn := func(conn *http2.Transport) func() {
		return conn.CloseIdleConnections
	}
	noCloseFn := func() {}

	if cfg.newConnectionPerRequest {
		// Create a new transport (i.e. connection) for each request.
		return func() (http.RoundTripper, func(), error) {
			conn := newConn()
			return conn, closeFn(conn), nil
		}, noCloseFn
	}

	// Re-use the same transport for all requests. For HTTP2, this should result
	// in multiplexing all requests over the same connection.
	conn := newConn()
	return func() (http.RoundTripper, func(), error) {
		return conn, noCloseFn, nil
	}, closeFn(conn)
}

func newHTTPTransportGetter(cfg *Config) (httpTransportGetter, func()) {
	newConn := func() *http.Transport {
		dialContext := func(ctx context.Context, network, addr string) (net.Conn, error) {
			return newDialer(cfg).DialContext(ctx, network, addr)
		}
		if len(cfg.UDS) > 0 {
			dialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
				return newDialer(cfg).DialContext(ctx, "unix", cfg.UDS)
			}
		}
		out := &http.Transport{
			// No connection pooling.
			DisableKeepAlives: true,
			TLSClientConfig:   cfg.tlsConfig,
			DialContext:       dialContext,
			Proxy:             http.ProxyFromEnvironment,
		}

		// Set the proxy in the transport, if specified.
		out.Proxy = cfg.proxyURL
		return out
	}
	noCloseFn := func() {}

	// Always create a new HTTP transport for each request, since HTTP can't multiplex over
	// a single connection.
	return func() (http.RoundTripper, func(), error) {
		conn := newConn()
		return conn, noCloseFn, nil
	}, noCloseFn
}

type httpCall struct {
	*httpProtocol
	getTransport httpTransportGetter
}

func (c *httpCall) makeRequest(ctx context.Context, cfg *Config, requestID int) (string, error) {
	start := time.Now()

	r := cfg.Request
	var outBuffer bytes.Buffer
	echo.ForwarderURLField.WriteForRequest(&outBuffer, requestID, r.Url)

	// Set the per-request timeout.
	ctx, cancel := context.WithTimeout(ctx, cfg.timeout)
	defer cancel()

	httpReq, err := http.NewRequestWithContext(ctx, cfg.method, cfg.urlHost, nil)
	if err != nil {
		return outBuffer.String(), err
	}

	// Use raw path, we don't want golang normalizing anything since we use this for testing purposes
	httpReq.URL.Opaque = cfg.urlPath

	// Use the host header as the host.
	httpReq.Host = cfg.hostHeader

	// Copy the headers.
	httpReq.Header = cfg.headers.Clone()
	writeForwardedHeaders(&outBuffer, requestID, cfg.headers)

	// Get the transport.
	transport, closeTransport, err := c.getTransport()
	if err != nil {
		return outBuffer.String(), err
	}
	defer closeTransport()

	// Create a new HTTP client.
	client := &http.Client{
		CheckRedirect: cfg.checkRedirect,
		Timeout:       cfg.timeout,
		Transport:     transport,
	}

	// Make the request.
	httpResp, err := client.Do(httpReq)
	if err != nil {
		return outBuffer.String(), err
	}

	echo.LatencyField.WriteForRequest(&outBuffer, requestID, fmt.Sprintf("%v", time.Since(start)))
	echo.ActiveRequestsField.WriteForRequest(&outBuffer, requestID, fmt.Sprintf("%d", c.e.ActiveRequests()))

	// Process the response.
	err = processHTTPResponse(requestID, httpResp, &outBuffer)

	// Extract the output string.
	return outBuffer.String(), err
}

func processHTTPResponse(requestID int, httpResp *http.Response, outBuffer *bytes.Buffer) error {
	// Make sure we close the body before exiting.
	defer func() {
		if err := httpResp.Body.Close(); err != nil {
			echo.WriteError(outBuffer, requestID, err)
		}
	}()

	echo.StatusCodeField.WriteForRequest(outBuffer, requestID, strconv.Itoa(httpResp.StatusCode))

	// Read the entire body.
	data, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return err
	}

	// Write the response headers to the output buffer.
	var keys []string
	for k := range httpResp.Header {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, key := range keys {
		values := httpResp.Header[key]
		for _, value := range values {
			echo.ResponseHeaderField.WriteKeyValueForRequest(outBuffer, requestID, key, value)
		}
	}

	// Write the lines of the body to the output buffer.
	for _, line := range strings.Split(string(data), "\n") {
		if line != "" {
			echo.WriteBodyLine(outBuffer, requestID, line)
		}
	}
	return nil
}

func (c *httpProtocol) Close() error {
	return nil
}
