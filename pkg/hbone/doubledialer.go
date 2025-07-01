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
	"time"

	"golang.org/x/net/http2"
)

// NewDialer creates a Dialer that proxies connections over HBONE to the configured proxy.
func NewDoubleDialer(outerCfg Config, innerTLSConfig *tls.Config) Dialer {
	var outerTransport *http2.Transport

	if outerCfg.TLS != nil {
		outerTransport = &http2.Transport{
			TLSClientConfig: outerCfg.TLS,
		}
	} else {
		outerTransport = &http2.Transport{
			// For h2c
			AllowHTTP: true,
			DialTLSContext: func(ctx context.Context, network, addr string, tlsCfg *tls.Config) (net.Conn, error) {
				d := net.Dialer{}
				if outerCfg.Timeout != nil {
					d.Timeout = *outerCfg.Timeout
				}
				return d.Dial(network, addr)
			},
		}
	}

	return &doubleDialer{
		outerCfg:       outerCfg,
		innerTLSConfig: innerTLSConfig,
		outerTransport: outerTransport,
	}
}

type doubleDialer struct {
	outerCfg       Config
	innerTLSConfig *tls.Config
	outerTransport *http2.Transport
}

// DialContext connects to `address` via the HBONE proxy.
func (d *doubleDialer) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	if network != "tcp" {
		return net.Dial(network, address)
	}
	// TODO: use context
	log.Infof("jaellio in the doubleDialer %v + %v", address, d.outerCfg)
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

	var innerTransport *http2.Transport
	if d.innerTLSConfig != nil {
		log.Infof("using TLS on inner connection")
		innerTransport = &http2.Transport{
			TLSClientConfig: d.innerTLSConfig,
			DialTLSContext: func(ctx context.Context, network, addr string, tlsCfg *tls.Config) (net.Conn, error) {
				tlsCfg.ServerName = resp.Request.Host
				// Upgrade the raw connection to a TLS connection.
				c := tls.Client(pc, tlsCfg)
				err := c.HandshakeContext(ctx)
				if err != nil {
					pc.Close()
					return nil, err
				}
				return c, nil
			},
		}
	} else {
		innerTransport = &http2.Transport{
			// For h2c
			AllowHTTP: true,
			DialTLSContext: func(ctx context.Context, network, addr string, tlsCfg *tls.Config) (net.Conn, error) {
				return pc, nil
			},
		}
	}
	// Note: outerCfg is only used to generate the final URL and host header (which is the same for double hbone)
	_, _, err = hbone(s, address, d.outerCfg, innerTransport, true)
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
