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
	"net/url"
	"sort"
	"strings"

	"github.com/lucas-clemente/quic-go"
	"github.com/lucas-clemente/quic-go/http3"
	"golang.org/x/net/http2"

	"istio.io/istio/pkg/test/echo"
	"istio.io/istio/pkg/test/echo/common/scheme"
)

var _ protocol = &httpProtocol{}

type httpProtocol struct {
	*Config
}

func newHTTPProtocol(r *Config) (*httpProtocol, error) {
	// Per-protocol setup.
	switch {
	case r.Request.Http3:
		if r.scheme == scheme.HTTP {
			return nil, fmt.Errorf("http3 requires HTTPS")
		}
	case r.Request.Http2:
		if r.Request.Alpn == nil {
			r.tlsConfig.NextProtos = []string{"h2"}
		}
	default:
		if r.Request.Alpn == nil {
			r.tlsConfig.NextProtos = []string{"http/1.1"}
		}
	}

	return &httpProtocol{
		Config: r,
	}, nil
}

func splitPath(raw string) (url, path string) {
	schemeSep := "://"
	schemeBegin := strings.Index(raw, schemeSep)
	if schemeBegin == -1 {
		return raw, ""
	}
	schemeEnd := schemeBegin + len(schemeSep)
	pathBegin := strings.IndexByte(raw[schemeEnd:], '/')
	if pathBegin == -1 {
		return raw, ""
	}
	return raw[:schemeEnd+pathBegin], raw[schemeEnd+pathBegin:]
}

func (c *httpProtocol) newClient() (*http.Client, error) {
	client := &http.Client{
		CheckRedirect: c.checkRedirect,
		Timeout:       c.timeout,
	}

	switch {
	case c.Request.Http3:
		client.Transport = &http3.RoundTripper{
			TLSClientConfig: c.tlsConfig,
			QuicConfig:      &quic.Config{},
		}
	case c.Request.Http2:
		if c.scheme == scheme.HTTPS {
			client.Transport = &http2.Transport{
				TLSClientConfig: c.tlsConfig,
				DialTLS: func(network, addr string, cfg *tls.Config) (net.Conn, error) {
					return tls.DialWithDialer(newDialer(), network, addr, cfg)
				},
			}
		} else {
			client.Transport = &http2.Transport{
				// Golang doesn't have first class support for h2c, so we provide some workarounds
				// See https://www.mailgun.com/blog/http-2-cleartext-h2c-client-example-go/
				// So http2.Transport doesn't complain the URL scheme isn't 'https'
				AllowHTTP: true,
				// Pretend we are dialing a TLS endpoint. (Note, we ignore the passed tls.Config)
				DialTLS: func(network, addr string, cfg *tls.Config) (net.Conn, error) {
					return newDialer().Dial(network, addr)
				},
			}
		}
	default:
		dialContext := func(ctx context.Context, network, addr string) (net.Conn, error) {
			return newDialer().Dial(network, addr)
		}
		if len(c.UDS) > 0 {
			dialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
				return newDialer().Dial("unix", c.UDS)
			}
		}
		transport := &http.Transport{
			// No connection pooling.
			DisableKeepAlives: true,
			TLSClientConfig:   c.tlsConfig,
			DialContext:       dialContext,
			Proxy:             http.ProxyFromEnvironment,
		}
		client.Transport = transport

		// Set the proxy in the transport, if specified.
		if len(c.Proxy) > 0 {
			proxyURL, err := url.Parse(c.Proxy)
			if err != nil {
				return nil, err
			}
			transport.Proxy = http.ProxyURL(proxyURL)
		}
	}

	return client, nil
}

func (c *httpProtocol) setHost(client *http.Client, r *http.Request, host string) {
	r.Host = host

	if r.URL.Scheme == "https" {
		// Set SNI value to be same as the request Host
		// For use with SNI routing tests
		httpTransport, ok := client.Transport.(*http.Transport)
		if ok && httpTransport.TLSClientConfig.ServerName == "" {
			httpTransport.TLSClientConfig.ServerName = host
			return
		}

		http2Transport, ok := client.Transport.(*http2.Transport)
		if ok && http2Transport.TLSClientConfig.ServerName == "" {
			http2Transport.TLSClientConfig.ServerName = host
			return
		}

		http3Transport, ok := client.Transport.(*http3.RoundTripper)
		if ok && http3Transport.TLSClientConfig.ServerName == "" {
			http3Transport.TLSClientConfig.ServerName = host
			return
		}
	}
}

func (c *httpProtocol) makeRequest(ctx context.Context, req *request) (string, error) {
	method := req.Method
	if method == "" {
		method = "GET"
	}

	// Manually split the path from the URL, the http.NewRequest() will fail to parse paths with invalid encoding that we
	// intentionally used in the test.
	u, p := splitPath(req.URL)
	httpReq, err := http.NewRequest(method, u, nil)
	if err != nil {
		return "", err
	}
	// Use raw path, we don't want golang normalizing anything since we use this for testing purposes
	httpReq.URL.Opaque = p

	// Set the per-request timeout.
	ctx, cancel := context.WithTimeout(ctx, req.Timeout)
	defer cancel()
	httpReq = httpReq.WithContext(ctx)

	var outBuffer bytes.Buffer
	outBuffer.WriteString(fmt.Sprintf("[%d] Url=%s\n", req.RequestID, req.URL))
	host := ""
	writeHeaders(req.RequestID, req.Header, outBuffer, func(key string, value string) {
		if key == hostHeader {
			host = value
		} else {
			// Avoid using .Add() to allow users to pass non-canonical forms
			httpReq.Header[key] = append(httpReq.Header[key], value)
		}
	})

	// Create a new HTTP client.
	client, err := c.newClient()
	if err != nil {
		return outBuffer.String(), err
	}

	c.setHost(client, httpReq, host)

	httpResp, err := client.Do(httpReq)
	if err != nil {
		return outBuffer.String(), err
	}

	outBuffer.WriteString(fmt.Sprintf("[%d] %s=%d\n", req.RequestID, echo.StatusCodeField, httpResp.StatusCode))

	var keys []string
	for k := range httpResp.Header {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, key := range keys {
		values := httpResp.Header[key]
		for _, value := range values {
			outBuffer.WriteString(fmt.Sprintf("[%d] %s=%s:%s\n", req.RequestID, echo.ResponseHeaderField, key, value))
		}
	}

	data, err := io.ReadAll(httpResp.Body)
	defer func() {
		if err = httpResp.Body.Close(); err != nil {
			outBuffer.WriteString(fmt.Sprintf("[%d error] %s\n", req.RequestID, err))
		}
	}()

	if err != nil {
		return outBuffer.String(), err
	}

	for _, line := range strings.Split(string(data), "\n") {
		if line != "" {
			outBuffer.WriteString(fmt.Sprintf("[%d body] %s\n", req.RequestID, line))
		}
	}

	return outBuffer.String(), nil
}

func (c *httpProtocol) Close() error {
	return nil
}
