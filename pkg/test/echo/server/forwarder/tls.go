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
)

var _ protocol = &tlsProtocol{}

type tlsProtocol struct {
	// conn returns a new connection. This is not just a shared connection as we will
	// not re-use the connection for multiple requests with TCP
	conn func() (*tls.Conn, error)
}

func newTLSProtocol(r *Config) (protocol, error) {
	return &tlsProtocol{
		conn: func() (*tls.Conn, error) {
			address := r.Request.Url[len(r.scheme+"://"):]

			con, err := tls.DialWithDialer(newDialer(), "tcp", address, r.tlsConfig)
			if err != nil {
				return nil, err
			}
			return con, nil
		},
	}, nil
}

func (c *tlsProtocol) makeRequest(ctx context.Context, req *request) (string, error) {
	conn, err := c.conn()
	if err != nil {
		return "", err
	}
	defer func() { _ = conn.Close() }()
	msgBuilder := strings.Builder{}
	msgBuilder.WriteString(fmt.Sprintf("[%d] Url=%s\n", req.RequestID, req.URL))

	// Apply per-request timeout to calculate deadline for reads/writes.
	ctx, cancel := context.WithTimeout(ctx, req.Timeout)
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
	if req.Message != "" {
		message = req.Message
	}
	if _, err := conn.Write([]byte(message + "\n")); err != nil {
		fwLog.Warnf("TCP write failed: %v", err)
		return msgBuilder.String(), err
	}

	cs := conn.ConnectionState()
	msgBuilder.WriteString(fmt.Sprintf("[%d] Cipher=%s\n", req.RequestID, tls.CipherSuiteName(cs.CipherSuite)))
	msgBuilder.WriteString(fmt.Sprintf("[%d] Version=%s\n", req.RequestID, versionName(cs.Version)))
	msgBuilder.WriteString(fmt.Sprintf("[%d] ServerName=%s\n", req.RequestID, cs.ServerName))
	msgBuilder.WriteString(fmt.Sprintf("[%d] Alpn=%s\n", req.RequestID, cs.NegotiatedProtocol))
	for n, i := range cs.PeerCertificates {
		pemBlock := pem.Block{
			Type:  "CERTIFICATE",
			Bytes: i.Raw,
		}
		msgBuilder.WriteString(fmt.Sprintf("[%d body] Response%d=%q\n", req.RequestID, n, string(pem.EncodeToMemory(&pemBlock))))
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
