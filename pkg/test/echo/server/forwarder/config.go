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
	"crypto/x509/pkix"
	"encoding/asn1"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"istio.io/istio/pkg/test/echo/common"
	"istio.io/istio/pkg/test/echo/common/scheme"
	"istio.io/istio/pkg/test/echo/proto"
)

type Config struct {
	Request *proto.ForwardEchoRequest
	UDS     string
	// XDSTestBootstrap, for gRPC forwarders, is used to set the bootstrap without using a global one defined in the env
	XDSTestBootstrap []byte
	// Http proxy used for connection
	Proxy string

	// Filled in values.
	scheme                  scheme.Instance
	tlsConfig               *tls.Config
	getClientCertificate    func(info *tls.CertificateRequestInfo) (*tls.Certificate, error)
	checkRedirect           func(req *http.Request, via []*http.Request) error
	proxyURL                func(*http.Request) (*url.URL, error)
	timeout                 time.Duration
	count                   int
	headers                 http.Header
	newConnectionPerRequest bool
	forceDNSLookup          bool
	hostHeader              string
	urlHost                 string
	urlPath                 string
	method                  string
	secure                  bool
}

func (c *Config) fillDefaults() error {
	c.checkRedirect = checkRedirectFunc(c.Request)
	c.timeout = common.GetTimeout(c.Request)
	c.count = common.GetCount(c.Request)
	c.headers = common.GetHeaders(c.Request)

	// Extract the host from the headers and then remove it.
	c.hostHeader = c.headers.Get(hostHeader)
	c.headers.Del(hostHeader)

	c.urlHost, c.urlPath = splitPath(c.Request.Url)

	c.method = c.Request.Method
	if c.method == "" {
		c.method = "GET"
	}

	if i := strings.IndexByte(c.Request.Url, ':'); i > 0 {
		c.scheme = scheme.Instance(strings.ToLower(c.Request.Url[0:i]))
	} else {
		return fmt.Errorf("missing protocol scheme in the request URL: %s", c.Request.Url)
	}

	var err error
	c.getClientCertificate, err = getClientCertificateFunc(c.Request)
	if err != nil {
		return err
	}
	c.secure = c.getClientCertificate != nil

	c.tlsConfig, err = newTLSConfig(c)
	if err != nil {
		return err
	}

	// Parse the proxy if specified.
	if len(c.Proxy) > 0 {
		proxyURL, err := url.Parse(c.Proxy)
		if err != nil {
			return err
		}

		c.proxyURL = http.ProxyURL(proxyURL)
	}

	// Configure reuseConnection and forceDNSLookup as appropriate.
	switch c.scheme {
	case scheme.DNS:
		c.newConnectionPerRequest = true
		c.forceDNSLookup = true
	case scheme.TCP, scheme.TLS, scheme.WebSocket, scheme.HTTPS:
		c.newConnectionPerRequest = true
		c.forceDNSLookup = c.Request.ForceDNSLookup
	default:
		c.newConnectionPerRequest = c.Request.NewConnectionPerRequest
		c.forceDNSLookup = c.newConnectionPerRequest && c.Request.ForceDNSLookup
	}

	return nil
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

func getClientCertificateFunc(r *proto.ForwardEchoRequest) (func(info *tls.CertificateRequestInfo) (*tls.Certificate, error), error) {
	if r.KeyFile != "" && r.CertFile != "" {
		certData, err := os.ReadFile(r.CertFile)
		if err != nil {
			return nil, fmt.Errorf("failed to load client certificate: %v", err)
		}
		r.Cert = string(certData)
		keyData, err := os.ReadFile(r.KeyFile)
		if err != nil {
			return nil, fmt.Errorf("failed to load client certificate key: %v", err)
		}
		r.Key = string(keyData)
	}

	if r.Cert != "" && r.Key != "" {
		cert, err := tls.X509KeyPair([]byte(r.Cert), []byte(r.Key))
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
		return func(info *tls.CertificateRequestInfo) (*tls.Certificate, error) {
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
		}, nil
	}

	return nil, nil
}

func newTLSConfig(c *Config) (*tls.Config, error) {
	r := c.Request
	tlsConfig := &tls.Config{
		GetClientCertificate: c.getClientCertificate,
		NextProtos:           r.GetAlpn().GetValue(),
		ServerName:           r.ServerName,
	}
	if r.CaCertFile != "" {
		certData, err := os.ReadFile(r.CaCertFile)
		if err != nil {
			return nil, fmt.Errorf("failed to load client certificate: %v", err)
		}
		r.CaCert = string(certData)
	}
	if r.InsecureSkipVerify || r.CaCert == "" {
		tlsConfig.InsecureSkipVerify = true
	} else if r.CaCert != "" {
		certPool := x509.NewCertPool()
		if !certPool.AppendCertsFromPEM([]byte(r.CaCert)) {
			return nil, fmt.Errorf("failed to create cert pool")
		}
		tlsConfig.RootCAs = certPool
	}

	setALPNForHTTP := func() {
		if r.Alpn == nil {
			switch {
			case r.Http3:
				// Do nothing.
			case r.Http2:
				tlsConfig.NextProtos = []string{"h2"}
			default:
				tlsConfig.NextProtos = []string{"http/1.1"}
			}
		}
	}

	// Per-protocol setup.
	switch c.scheme {
	case scheme.HTTPS:
		// Set SNI value to be same as the request Host
		// For use with SNI routing tests
		if tlsConfig.ServerName == "" {
			tlsConfig.ServerName = c.hostHeader
		}
		setALPNForHTTP()
	case scheme.HTTP:
		if r.Http3 {
			return nil, fmt.Errorf("http3 requires HTTPS")
		}
		setALPNForHTTP()
	}
	return tlsConfig, nil
}

func checkRedirectFunc(req *proto.ForwardEchoRequest) func(req *http.Request, via []*http.Request) error {
	if req.FollowRedirects {
		return nil
	}

	return func(req *http.Request, via []*http.Request) error {
		// Disable redirects
		return http.ErrUseLastResponse
	}
}
