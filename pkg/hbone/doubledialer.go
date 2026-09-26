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
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"
)

// NewDialer creates a Dialer that proxies connections over HBONE to the configured proxy.
func NewDoubleDialer(outerCfg Config, innerCfg Config, innerTLSConfig *tls.Config) Dialer {
	outerTransport := &http.Transport{
		// Must be DialContext, not DialTLSContext: net/http only uses the latter for https:// URLs.
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			d := net.Dialer{}
			if outerCfg.Timeout != nil {
				d.Timeout = *outerCfg.Timeout
			}
			return d.DialContext(ctx, network, addr)
		},
		Protocols: new(http.Protocols),
	}
	if outerCfg.TLS != nil {
		// Clone: net/http mutates TLSClientConfig.NextProtos when configuring HTTP/2.
		outerTransport.TLSClientConfig = outerCfg.TLS.Clone()
		// Explicit opt-in is required; net/http disables HTTP/2 when a custom TLS config is set.
		outerTransport.Protocols.SetHTTP2(true)
	} else {
		// For h2c
		outerTransport.Protocols.SetUnencryptedHTTP2(true)
	}

	return &doubleDialer{
		outerCfg:       outerCfg,
		innerCfg:       innerCfg,
		innerTLSConfig: innerTLSConfig,
		outerTransport: outerTransport,
	}
}

type doubleDialer struct {
	outerCfg       Config
	innerCfg       Config
	innerTLSConfig *tls.Config
	outerTransport *http.Transport
}

// DialContext connects to `address` via the HBONE proxy.
func (d *doubleDialer) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	if network != "tcp" {
		return net.Dial(network, address)
	}
	c, s := net.Pipe()
	resp, pw, err := hbone(s, address, d.outerCfg, d.outerTransport, false)
	if err != nil {
		return nil, err
	}

	pc := &pipeConn{
		name:   "inner CONNECT",
		writer: pw,
		reader: resp.Body,
	}

	innerTransport := &http.Transport{Protocols: new(http.Protocols)}
	if d.innerTLSConfig != nil {
		log.Infof("using TLS on inner connection")
		innerTransport.TLSClientConfig = d.innerTLSConfig.Clone()
		innerTransport.Protocols.SetHTTP2(true)
		// The inner CONNECT targets an https:// URL, so net/http does use DialTLSContext here.
		innerTransport.DialTLSContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
			// Upgrade the raw connection to a TLS connection. We must advertise "h2" and set SNI
			// ourselves; net/http does not touch the config of a connection we hand it.
			c := tls.Client(pc, H2ClientTLSConfig(d.innerTLSConfig, addr))
			err := c.HandshakeContext(ctx)
			if err != nil {
				pc.Close()
				return nil, err
			}
			return c, nil
		}
	} else {
		// For h2c. The inner CONNECT targets an http:// URL, so this must be DialContext:
		// net/http would never call DialTLSContext and would dial the proxy address directly,
		// bypassing the outer tunnel entirely.
		innerTransport.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
			return pc, nil
		}
		innerTransport.Protocols.SetUnencryptedHTTP2(true)
	}
	// Note: outerCfg is only used to generate the final URL and host header (which is the same for double hbone)
	_, _, err = hbone(s, address, d.innerCfg, innerTransport, true)
	if err != nil {
		return nil, err
	}
	return c, nil
}

func (d doubleDialer) Dial(network, address string) (c net.Conn, err error) {
	return d.DialContext(context.Background(), network, address)
}

type pipeAddr struct {
	name string
}

func (p *pipeAddr) Network() string {
	return "pipe"
}

func (p *pipeAddr) String() string {
	return "pipe " + p.name
}

// implements net.Conn
type pipeConn struct {
	name   string
	reader io.ReadCloser
	writer io.WriteCloser
}

func (p *pipeConn) LocalAddr() net.Addr {
	return &pipeAddr{name: p.name}
}

func (p *pipeConn) RemoteAddr() net.Addr {
	return &pipeAddr{name: p.name}
}

func (p *pipeConn) SetDeadline(t time.Time) error {
	return errors.New("not implemented")
}

func (p *pipeConn) SetReadDeadline(t time.Time) error {
	return errors.New("not implemented")
}

func (p *pipeConn) SetWriteDeadline(t time.Time) error {
	return errors.New("not implemented")
}

func (p *pipeConn) Read(b []byte) (n int, err error) {
	return p.reader.Read(b)
}

func (p *pipeConn) Write(b []byte) (n int, err error) {
	return p.writer.Write(b)
}

func (p *pipeConn) Close() (err error) {
	err = p.reader.Close()
	secondErr := p.writer.Close()
	if err != nil && secondErr != nil {
		return fmt.Errorf("multiple errors: %v, %v", err, secondErr)
	}

	if err != nil {
		return err
	}

	if secondErr != nil {
		return secondErr
	}

	return nil
}
