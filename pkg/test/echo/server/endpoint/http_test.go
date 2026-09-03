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

package endpoint

import (
	"bytes"
	"context"
	"crypto/tls"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"
)

type tlsStateConn struct {
	state tls.ConnectionState
}

func (c tlsStateConn) ConnectionState() tls.ConnectionState { return c.state }
func (tlsStateConn) Read([]byte) (int, error)               { return 0, net.ErrClosed }
func (tlsStateConn) Write([]byte) (int, error)              { return 0, net.ErrClosed }
func (tlsStateConn) Close() error                           { return nil }
func (tlsStateConn) LocalAddr() net.Addr                    { return &net.TCPAddr{} }
func (tlsStateConn) RemoteAddr() net.Addr                   { return &net.TCPAddr{} }
func (tlsStateConn) SetDeadline(time.Time) error            { return nil }
func (tlsStateConn) SetReadDeadline(time.Time) error        { return nil }
func (tlsStateConn) SetWriteDeadline(time.Time) error       { return nil }

var _ net.Conn = tlsStateConn{}

func TestAddResponsePayloadALPNFromConn(t *testing.T) {
	h := &httpHandler{}
	tests := []struct {
		name    string
		req     *http.Request
		want    string
		notWant string
	}{
		{
			name: "request TLS is used when set",
			req: &http.Request{
				Proto:      "HTTP/2.0",
				RequestURI: "/",
				RemoteAddr: "127.0.0.1:1",
				TLS:        &tls.ConnectionState{NegotiatedProtocol: "http/1.1", ServerName: "from-request"},
			},
			want: "Alpn=http/1.1\n",
		},
		{
			name: "conn TLS is used when request TLS is nil",
			req: func() *http.Request {
				r := &http.Request{
					Proto:      "HTTP/2.0",
					RequestURI: "/",
					RemoteAddr: "127.0.0.1:1",
				}
				conn := tlsStateConn{state: tls.ConnectionState{NegotiatedProtocol: "h2", ServerName: "from-conn"}}
				return r.WithContext(context.WithValue(context.Background(), ConnContextKey, conn))
			}(),
			want: "Alpn=h2\n",
		},
		{
			name: "no TLS state omits alpn",
			req: &http.Request{
				Proto:      "HTTP/1.1",
				RequestURI: "/",
				RemoteAddr: "127.0.0.1:1",
			},
			notWant: "Alpn=",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var body bytes.Buffer
			h.addResponsePayload(tt.req, &body)
			got := body.String()
			if tt.want != "" && !strings.Contains(got, tt.want) {
				t.Fatalf("payload missing %q:\n%s", tt.want, got)
			}
			if tt.notWant != "" && strings.Contains(got, tt.notWant) {
				t.Fatalf("payload unexpectedly contains %q:\n%s", tt.notWant, got)
			}
		})
	}
}
