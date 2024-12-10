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
	"sync"
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
	c, s := net.Pipe()
	err := d.proxyTo(s, d.outerCfg, address)
	if err != nil {
		return nil, err
	}
	return c, nil
}

func (d doubleDialer) Dial(network, address string) (c net.Conn, err error) {
	return d.DialContext(context.Background(), network, address)
}

func (d *doubleDialer) proxyTo(conn io.ReadWriteCloser, req Config, address string) error {
	t0 := time.Now()

	url := "http://" + req.ProxyAddress
	if req.TLS != nil {
		url = "https://" + req.ProxyAddress
	}
	// We could just pass `conn` to `http.NewRequest`, but this has a few issues:
	// * Less visibility into i/o
	// * http will call conn.Close, which will close before we want to (finished writing response).
	pr, pw := io.Pipe()

	r, err := http.NewRequest(http.MethodConnect, url, pr)
	if err != nil {
		return fmt.Errorf("new request: %v", err)
	}
	r.Host = address

	// Initiate CONNECT.
	log.Infof("initiate outer CONNECT to %v via %v", r.Host, url)

	resp, err := d.outerTransport.RoundTrip(r)
	if err != nil {
		return fmt.Errorf("round trip: %v", err)
	}
	var remoteID, innerID string
	if resp.TLS != nil && len(resp.TLS.PeerCertificates) > 0 {
		ids := resp.TLS.PeerCertificates[0].DNSNames
		if len(ids) > 0 {
			remoteID = ids[0]
		}
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("round trip failed: %v", resp.Status)
	}
	log.WithLabels("host", r.Host, "remote", remoteID).Info("Outer CONNECT established")

	log.Infof("initiate inner CONNECT to %v via inner tunnel", r.Host)

	ipr, ipw := io.Pipe()
	innerReq, err := http.NewRequest(http.MethodConnect, url, ipr)
	if err != nil {
		return fmt.Errorf("new inner request: %v", err)
	}

	pc := &pipeConn{
		name:   "inner CONNECT",
		writer: pw,
		reader: resp.Body,
	}
	innerReq.Host = address
	var innerTransport *http2.Transport
	if d.innerTLSConfig != nil {
		log.Infof("using TLS on inner connection")
		innerTransport = &http2.Transport{
			TLSClientConfig: d.innerTLSConfig,
			DialTLSContext: func(ctx context.Context, network, addr string, tlsCfg *tls.Config) (net.Conn, error) {
				log.Infof("Sending TLS connection with config %#v", tlsCfg)
				return pc, nil
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

	wg := sync.WaitGroup{}
	wg.Add(1)
	// NOTE: We don't close the connection here because that will affect the outer connection from the HBONE client.
	// Instead, we close the conn further down once we've finished proxying.
	go func() {
		// handle upstream (inner hbone server) <-- downstream (app)
		copyBuffered(ipw, conn, log.WithLabels("name", "body to pipe"))
		wg.Done()
	}()

	innerResp, innerErr := innerTransport.RoundTrip(innerReq)
	if innerErr != nil {
		return fmt.Errorf("inner round trip: %v", innerErr)
	}
	log.Info("inner round trip complete")

	if innerResp.TLS != nil {
		log.Infof("inner TLS does exist")
		if len(innerResp.TLS.PeerCertificates) > 0 {
			ids := innerResp.TLS.PeerCertificates[0].DNSNames
			if len(ids) > 0 {
				innerID = ids[0]
			}
		}
	}
	if innerResp.StatusCode != http.StatusOK {
		return fmt.Errorf("round trip failed: %v", innerResp.Status)
	}
	log.WithLabels("host", r.Host, "remote", innerID).Info("Inner CONNECT established")

	go func() {
		defer conn.Close()
		defer resp.Body.Close()

		wg.Add(1)
		go func() {
			// handle upstream (hbone server) --> downstream (app)
			copyBuffered(conn, innerResp.Body, log.WithLabels("name", "body to conn"))
			wg.Done()
		}()

		wg.Wait()
		log.Infof("stream closed in %v", time.Since(t0))
	}()

	return nil
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
