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

// Package admin implements the private Envoy administration transport.
package admin

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"time"
)

type Transport string

const (
	TCP        Transport = "TCP"
	UDS        Transport = "UDS"
	Env                  = "ISTIO_ENVOY_ADMIN_TRANSPORT"
	NativeEnv            = "ISTIO_NATIVE_SIDECAR"
	Annotation           = "sidecar.istio.io/adminTransport"
	SocketPath           = "/etc/istio/proxy/admin/admin.sock"
)

func Parse(value string) (Transport, error) {
	switch Transport(value) {
	case TCP, UDS:
		return Transport(value), nil
	default:
		return "", fmt.Errorf("%s must be TCP or UDS, got %q", Env, value)
	}
}

func Resolve(annotations, metadata map[string]string) (Transport, error) {
	if v, ok := annotations[Annotation]; ok {
		return Parse(v)
	}
	if v, ok := metadata[Env]; ok {
		return Parse(v)
	}
	return TCP, nil
}

func FromEnvironment() (Transport, error) {
	if v, ok := os.LookupEnv(Env); ok {
		return Parse(v)
	}
	return TCP, nil
}

func Restricted() bool { t, err := FromEnvironment(); return err == nil && t == UDS }

// NewClient never uses a network proxy or falls back to TCP for a Unix socket.
func NewClient(transport Transport, socket string, timeout time.Duration) (*http.Client, error) {
	if _, err := Parse(string(transport)); err != nil {
		return nil, err
	}
	tr := &http.Transport{}
	if transport == UDS {
		tr.DialContext = func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "unix", socket)
		}
	}
	return &http.Client{Transport: tr, Timeout: timeout,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}, nil
}

func Do(ctx context.Context, method, requestURL, body string, timeout time.Duration) (*bytes.Buffer, error) {
	transport, err := FromEnvironment()
	if err != nil {
		return nil, err
	}
	client, err := NewClient(transport, SocketPath, timeout)
	if err != nil {
		return nil, err
	}
	defer client.CloseIdleConnections()
	req, err := http.NewRequestWithContext(ctx, method, requestURL, strings.NewReader(body))
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("admin returned %s: %s", resp.Status, b)
	}
	return bytes.NewBuffer(b), nil
}
