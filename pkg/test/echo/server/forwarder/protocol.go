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

// An example implementation of a client.

package forwarder

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/asn1"
	"fmt"
	"io/ioutil"
	"net"
	"net/http"
	"net/url"
	"time"

	"github.com/gorilla/websocket"
	"github.com/lucas-clemente/quic-go"
	"github.com/lucas-clemente/quic-go/http3"
	"golang.org/x/net/http2"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"

	"istio.io/istio/pkg/test/echo/common"
	"istio.io/istio/pkg/test/echo/common/scheme"
	"istio.io/istio/pkg/test/echo/proto"
)

type request struct {
	URL         string
	Header      http.Header
	RequestID   int
	Message     string
	Timeout     time.Duration
	ServerFirst bool
	Method      string
}

type protocol interface {
	makeRequest(ctx context.Context, req *request) (string, error)
	Close() error
}

func newProtocol(cfg Config) (protocol, error) {
	var httpDialContext func(ctx context.Context, network, addr string) (net.Conn, error)
	var wsDialContext func(network, addr string) (net.Conn, error)
	if len(cfg.UDS) > 0 {
		httpDialContext = func(_ context.Context, _, _ string) (net.Conn, error) {
			return net.Dial("unix", cfg.UDS)
		}

		wsDialContext = func(_, _ string) (net.Conn, error) {
			return net.Dial("unix", cfg.UDS)
		}
	}

	rawURL := cfg.Request.Url
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("failed parsing request URL %s: %v", cfg.Request.Url, err)
	}

	timeout := common.GetTimeout(cfg.Request)
	headers := common.GetHeaders(cfg.Request)

	var getClientCertificate func(info *tls.CertificateRequestInfo) (*tls.Certificate, error)
	if cfg.Request.KeyFile != "" && cfg.Request.CertFile != "" {
		certData, err := ioutil.ReadFile(cfg.Request.CertFile)
		if err != nil {
			return nil, fmt.Errorf("failed to load client certificate: %v", err)
		}
		cfg.Request.Cert = string(certData)
		keyData, err := ioutil.ReadFile(cfg.Request.KeyFile)
		if err != nil {
			return nil, fmt.Errorf("failed to load client certificate key: %v", err)
		}
		cfg.Request.Key = string(keyData)
	}
	if cfg.Request.Cert != "" && cfg.Request.Key != "" {
		cert, err := tls.X509KeyPair([]byte(cfg.Request.Cert), []byte(cfg.Request.Key))
		if err != nil {
			return nil, fmt.Errorf("failed to parse x509 key pair: %v", err)
		}

		for _, c := range cert.Certificate {
			cert, err := x509.ParseCertificate(c)
			if err != nil {
				fwLog.Errorf("Failed to parse client certificate: %v", err)
			}
			fwLog.Debugf("Using client certificate [%s] issued by %s", cert.SerialNumber, cert.Issuer)
			for _, uri := range cert.URIs {
				fwLog.Debugf("  URI SAN: %s", uri)
			}
		}
		// nolint: unparam
		getClientCertificate = func(info *tls.CertificateRequestInfo) (*tls.Certificate, error) {
			fwLog.Debugf("Peer asking for client certificate")
			for i, ca := range info.AcceptableCAs {
				x := &pkix.RDNSequence{}
				if _, err := asn1.Unmarshal(ca, x); err != nil {
					fwLog.Errorf("Failed to decode AcceptableCA[%d]: %v", i, err)
				} else {
					name := &pkix.Name{}
					name.FillFromRDNSequence(x)
					fwLog.Debugf("  AcceptableCA[%d]: %s", i, name)
				}
			}

			return &cert, nil
		}
	}
	tlsConfig := &tls.Config{
		GetClientCertificate: getClientCertificate,
		NextProtos:           cfg.Request.GetAlpn().GetValue(),
	}
	if cfg.Request.CaCertFile != "" {
		certData, err := ioutil.ReadFile(cfg.Request.CaCertFile)
		if err != nil {
			return nil, fmt.Errorf("failed to load client certificate: %v", err)
		}
		cfg.Request.CaCert = string(certData)
	}
	if cfg.Request.InsecureSkipVerify || cfg.Request.CaCert == "" {
		tlsConfig.InsecureSkipVerify = true
	} else if cfg.Request.CaCert != "" {
		certPool := x509.NewCertPool()
		if !certPool.AppendCertsFromPEM([]byte(cfg.Request.CaCert)) {
			return nil, fmt.Errorf("failed to create cert pool")
		}
		tlsConfig.RootCAs = certPool
	}

	// Disable redirects
	redirectFn := func(req *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	}
	if cfg.Request.FollowRedirects {
		redirectFn = nil
	}
	switch scheme.Instance(u.Scheme) {
	case scheme.HTTP, scheme.HTTPS:
		if cfg.Request.Alpn == nil {
			tlsConfig.NextProtos = []string{"http/1.1"}
		}
		proto := &httpProtocol{
			client: &http.Client{
				CheckRedirect: redirectFn,
				Transport: &http.Transport{
					// We are creating a Transport on each ForwardEcho request. Transport is what holds connections,
					// so this means every ForwardEcho request will create a new connection. Without setting an idle timeout,
					// we would never close these connections.
					IdleConnTimeout: time.Second,
					TLSClientConfig: tlsConfig,
					DialContext:     httpDialContext,
					Proxy:           http.ProxyFromEnvironment,
				},
				Timeout: timeout,
			},
			do: cfg.Dialer.HTTP,
		}
		if cfg.Request.Http3 && scheme.Instance(u.Scheme) == scheme.HTTP {
			return nil, fmt.Errorf("http3 requires HTTPS")
		} else if cfg.Request.Http3 {
			proto.client.Transport = &http3.RoundTripper{
				TLSClientConfig: tlsConfig,
				QuicConfig:      &quic.Config{},
			}
		} else if cfg.Request.Http2 && scheme.Instance(u.Scheme) == scheme.HTTPS {
			if cfg.Request.Alpn == nil {
				tlsConfig.NextProtos = []string{"h2"}
			}
			proto.client.Transport = &http2.Transport{
				TLSClientConfig: tlsConfig,
				DialTLS: func(network, addr string, cfg *tls.Config) (net.Conn, error) {
					return tls.Dial(network, addr, cfg)
				},
			}
		} else if cfg.Request.Http2 {
			proto.client.Transport = &http2.Transport{
				// Golang doesn't have first class support for h2c, so we provide some workarounds
				// See https://www.mailgun.com/blog/http-2-cleartext-h2c-client-example-go/
				// So http2.Transport doesn't complain the URL scheme isn't 'https'
				AllowHTTP: true,
				// Pretend we are dialing a TLS endpoint. (Note, we ignore the passed tls.Config)
				DialTLS: func(network, addr string, cfg *tls.Config) (net.Conn, error) {
					return net.Dial(network, addr)
				},
			}
		}
		return proto, nil
	case scheme.GRPC:
		// grpc-go sets incorrect authority header
		authority := headers.Get(hostHeader)

		// transport security
		security := grpc.WithInsecure()
		if getClientCertificate != nil {
			security = grpc.WithTransportCredentials(credentials.NewTLS(tlsConfig))
		}

		// Strip off the scheme from the address.
		address := rawURL[len(u.Scheme+"://"):]

		// Connect to the GRPC server.
		ctx, cancel := context.WithTimeout(context.Background(), common.ConnectionTimeout)
		defer cancel()
		grpcConn, err := cfg.Dialer.GRPC(ctx,
			address,
			security,
			grpc.WithAuthority(authority))
		if err != nil {
			return nil, err
		}
		return &grpcProtocol{
			conn:   grpcConn,
			client: proto.NewEchoTestServiceClient(grpcConn),
		}, nil
	case scheme.WebSocket:
		dialer := &websocket.Dialer{
			TLSClientConfig:  tlsConfig,
			NetDial:          wsDialContext,
			HandshakeTimeout: timeout,
		}
		return &websocketProtocol{
			dialer: dialer,
		}, nil
	case scheme.DNS:
		return &dnsProtocol{}, nil
	case scheme.TCP:
		return &tcpProtocol{
			conn: func() (net.Conn, error) {
				dialer := net.Dialer{
					Timeout: timeout,
				}
				address := rawURL[len(u.Scheme+"://"):]

				ctx, cancel := context.WithTimeout(context.Background(), common.ConnectionTimeout)
				defer cancel()

				if getClientCertificate == nil {
					return cfg.Dialer.TCP(dialer, ctx, address)
				}
				return tls.Dial("tcp", address, tlsConfig)
			},
		}, nil
	}

	return nil, fmt.Errorf("unrecognized protocol %q", u.String())
}
