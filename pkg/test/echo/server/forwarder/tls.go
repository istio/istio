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
	"context"
	"crypto/tls"
	"encoding/pem"
	"fmt"
	"strings"
	"time"

	"istio.io/istio/pkg/test/echo"
	"istio.io/istio/pkg/test/echo/proto"
)

var _ protocol = &tlsProtocol{}

type tlsProtocol struct {
	e *executor
}

func newTLSProtocol(e *executor) protocol {
	return &tlsProtocol{e: e}
}

func (c *tlsProtocol) ForwardEcho(ctx context.Context, cfg *Config) (*proto.ForwardEchoResponse, error) {
	return doForward(ctx, cfg, c.e, c.makeRequest)
}

func (c *tlsProtocol) makeRequest(ctx context.Context, cfg *Config, requestID int) (string, error) {
	conn, err := newTLSConnection(cfg)
	if err != nil {
		return "", err
	}
	defer func() { _ = conn.Close() }()
	msgBuilder := strings.Builder{}
	echo.ForwarderURLField.WriteForRequest(&msgBuilder, requestID, cfg.Request.Url)

	// Apply per-request timeout to calculate deadline for reads/writes.
	ctx, cancel := context.WithTimeout(ctx, cfg.timeout)
	defer cancel()

	// Apply the deadline to the connection.
	deadline, _ := ctx.Deadline()
	if err := conn.SetWriteDeadline(deadline); err != nil {
		return msgBuilder.String(), err
	}
	if err := conn.SetReadDeadline(deadline); err != nil {
		return msgBuilder.String(), err
	}

	if err := conn.HandshakeContext(ctx); err != nil {
		return "", err
	}
	// Make sure the client writes something to the buffer
	message := "HelloWorld"
	if cfg.Request.Message != "" {
		message = cfg.Request.Message
	}

	start := time.Now()
	if _, err := conn.Write([]byte(message + "\n")); err != nil {
		fwLog.Warnf("TCP write failed: %v", err)
		return msgBuilder.String(), err
	}

	cs := conn.ConnectionState()
	echo.LatencyField.WriteForRequest(&msgBuilder, requestID, fmt.Sprintf("%v", time.Since(start)))
	echo.CipherField.WriteForRequest(&msgBuilder, requestID, tls.CipherSuiteName(cs.CipherSuite))
	echo.TLSVersionField.WriteForRequest(&msgBuilder, requestID, versionName(cs.Version))
	echo.TLSServerName.WriteForRequest(&msgBuilder, requestID, cs.ServerName)
	echo.AlpnField.WriteForRequest(&msgBuilder, requestID, cs.NegotiatedProtocol)
	for n, i := range cs.PeerCertificates {
		pemBlock := pem.Block{
			Type:  "CERTIFICATE",
			Bytes: i.Raw,
		}
		echo.WriteBodyLine(&msgBuilder, requestID, fmt.Sprintf("Response%d=%q", n, string(pem.EncodeToMemory(&pemBlock))))
	}

	msg := msgBuilder.String()
	return msg, nil
}

func versionName(v uint16) string {
	switch v {
	case tls.VersionTLS10:
		return "1.0"
	case tls.VersionTLS11:
		return "1.1"
	case tls.VersionTLS12:
		return "1.2"
	case tls.VersionTLS13:
		return "1.3"
	default:
		return fmt.Sprintf("unknown-%v", v)
	}
}

func (c *tlsProtocol) Close() error {
	return nil
}

func newTLSConnection(cfg *Config) (*tls.Conn, error) {
	address := cfg.Request.Url[len(cfg.scheme+"://"):]

	con, err := tls.DialWithDialer(newDialer(cfg.forceDNSLookup), "tcp", address, cfg.tlsConfig)
	if err != nil {
		return nil, err
	}
	return con, nil
}
