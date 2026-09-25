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
	"fmt"
	"io"
	"net"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/proxy"

	istiolog "istio.io/istio/pkg/log"
	"istio.io/istio/security/pkg/pki/util"
)

var log = istiolog.RegisterScope("hbone", "")

// Config defines the configuration for a given dialer. All fields other than ProxyAddress are optional
type Config struct {
	// ProxyAddress defines the address of the HBONE proxy we are connecting to
	ProxyAddress string
	Headers      http.Header
	TLS          *tls.Config
	Timeout      *time.Duration
}

type Dialer interface {
	proxy.Dialer
	proxy.ContextDialer
}

// NewDialer creates a Dialer that proxies connections over HBONE to the configured proxy.
func NewDialer(cfg Config) Dialer {
	transport := &http.Transport{
		// Note: this must be DialContext, not DialTLSContext. net/http only consults
		// DialTLSContext for https:// URLs, so an h2c transport would silently ignore it.
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			d := net.Dialer{}
			if cfg.Timeout != nil {
				d.Timeout = *cfg.Timeout
			}
			return d.DialContext(ctx, network, addr)
		},
		Protocols: new(http.Protocols),
	}
	if cfg.TLS != nil {
		// Clone: net/http mutates TLSClientConfig.NextProtos when configuring HTTP/2.
		transport.TLSClientConfig = cfg.TLS.Clone()
		// HBONE is always HTTP/2. This must be set explicitly, as net/http disables HTTP/2 by
		// default whenever a custom TLS config or dialer is set.
		transport.Protocols.SetHTTP2(true)
	} else {
		// For h2c
		transport.Protocols.SetUnencryptedHTTP2(true)
	}
	return &dialer{
		cfg:       cfg,
		transport: transport,
	}
}

// H2ClientTLSConfig returns a copy of cfg suitable for an HTTP/2 client connection to addr.
// net/http applies this fixup itself for connections it dials, but not for ones handed to it
// by a DialTLSContext. Without it ALPN never selects "h2" and net/http silently falls back to
// HTTP/1.1 framing, which HBONE peers do not accept.
func H2ClientTLSConfig(cfg *tls.Config, addr string) *tls.Config {
	out := cfg.Clone()
	if !slices.Contains(out.NextProtos, "h2") {
		out.NextProtos = append([]string{"h2"}, out.NextProtos...)
	}
	if out.ServerName == "" {
		host, _, err := net.SplitHostPort(addr)
		if err != nil {
			host = addr
		}
		out.ServerName = host
	}
	return out
}

type dialer struct {
	cfg       Config
	transport *http.Transport
}

// DialContext connects to `address` via the HBONE proxy.
func (d *dialer) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	if network != "tcp" {
		return net.Dial(network, address)
	}
	// TODO: use context
	c, s := net.Pipe()
	_, _, err := hbone(s, address, d.cfg, d.transport, true)
	if err != nil {
		return nil, err
	}
	return c, nil
}

func (d dialer) Dial(network, address string) (c net.Conn, err error) {
	return d.DialContext(context.Background(), network, address)
}

func hbone(conn io.ReadWriteCloser, address string, req Config, transport *http.Transport, shouldCopy bool) (*http.Response, io.WriteCloser, error) {
	t0 := time.Now()

	url := "http://" + req.ProxyAddress
	if req.TLS != nil {
		url = "https://" + req.ProxyAddress
	}

	pr, pw := io.Pipe()
	r, err := http.NewRequest(http.MethodConnect, url, pr)
	if err != nil {
		return nil, nil, fmt.Errorf("new request: %v", err)
	}
	r.Host = address
	for k, vs := range req.Headers {
		for _, v := range vs {
			r.Header.Add(k, v)
		}
	}
	// Initiate CONNECT.
	log.Infof("initiate CONNECT to %v via %v", r.Host, url)

	wg := sync.WaitGroup{}

	resp, err := transport.RoundTrip(r)
	if err != nil {
		return nil, nil, fmt.Errorf("round trip: %v", err)
	}
	var remoteID string
	if resp.TLS != nil && len(resp.TLS.PeerCertificates) > 0 {
		ids, _ := util.ExtractIDs(resp.TLS.PeerCertificates[0].Extensions)
		if len(ids) > 0 {
			remoteID = ids[0]
		}
	}

	if resp.StatusCode != http.StatusOK {
		return nil, nil, fmt.Errorf("round trip failed: %v", resp.Status)
	}

	if shouldCopy {
		wg.Add(1)

		go func() {
			defer wg.Done()
			// Copy from conn into the pipe, which will then be sent as part of the request
			// handle upstream (hbone server) <-- downstream (app)
			copyBuffered(pw, conn, log.WithLabels("name", "conn to pipe"))
		}()
	}
	log.WithLabels("host", r.Host, "remote", remoteID).Info("CONNECT established")

	if shouldCopy {
		go func() {
			defer conn.Close()
			defer resp.Body.Close()

			wg.Add(1)
			go func() {
				// handle upstream (hbone server) --> downstream (app)
				copyBuffered(conn, resp.Body, log.WithLabels("name", "body to conn"))
				wg.Done()
			}()

			wg.Wait()
			log.Infof("stream closed in %v", time.Since(t0))
		}()
	}

	return resp, pw, nil
}

// TLSDialWithDialer is an implementation of tls.DialWithDialer that accepts a generic Dialer
func TLSDialWithDialer(dialer Dialer, network, addr string, config *tls.Config) (*tls.Conn, error) {
	return tlsDial(context.Background(), dialer, network, addr, config)
}

func tlsDial(ctx context.Context, netDialer Dialer, network, addr string, config *tls.Config) (*tls.Conn, error) {
	rawConn, err := netDialer.DialContext(ctx, network, addr)
	if err != nil {
		return nil, err
	}

	colonPos := strings.LastIndex(addr, ":")
	if colonPos == -1 {
		colonPos = len(addr)
	}
	hostname := addr[:colonPos]

	if config == nil {
		config = &tls.Config{MinVersion: tls.VersionTLS12}
	}
	// If no ServerName is set, infer the ServerName
	// from the hostname we're connecting to.
	if config.ServerName == "" {
		// Make a copy to avoid polluting argument or default.
		c := config.Clone()
		c.ServerName = hostname
		config = c
	}

	conn := tls.Client(rawConn, config)
	if err := conn.HandshakeContext(ctx); err != nil {
		_ = rawConn.Close()
		return nil, err
	}
	return conn, nil
}
