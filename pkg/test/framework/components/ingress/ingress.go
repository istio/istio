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

package ingress

import (
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"

	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/resource"
)

// CallType defines ingress gateway type
type CallType int

const (
	PlainText CallType = 0
	TLS       CallType = 1
	Mtls      CallType = 2
)

// CallOptions defines options for calling a Endpoint.
type CallOptions struct {
	// Host specifies the host to be used on the request. If not provided, an appropriate
	// default is chosen for the target Instance.
	Host string

	// Path specifies the URL path for the request.
	Path string

	// Headers indicates headers that should be sent in the request. Ignored for WebSocket calls.
	Headers http.Header

	// Timeout used for each individual request. Must be > 0, otherwise 1 minute is used.
	Timeout time.Duration

	// CaCert is inline base64 encoded root certificate that authenticates server certificate provided
	// by ingress gateway.
	CaCert string
	// PrivateKey is inline base64 encoded private key for test client.
	PrivateKey string
	// Cert is inline base64 encoded certificate for test client.
	Cert string

	// Address is the ingress gateway IP and port to call to.
	Address net.TCPAddr

	// CallType specifies what type of call to make (PlainText, TLS, mTLS).
	CallType CallType
}

// sanitize checks and fills fields in CallOptions. Returns error on failures, and nil otherwise.
func (o *CallOptions) sanitize() error {
	if o.Timeout <= 0 {
		o.Timeout = DefaultRequestTimeout
	}
	if !strings.HasPrefix(o.Path, "/") {
		o.Path = "/" + o.Path
	}
	if len(o.Address.IP) == 0 {
		return fmt.Errorf("address is not set")
	}
	return nil
}

// Instance represents a deployed Ingress Gateway instance.
type Instance interface {
	resource.Resource

	// HTTPAddress returns the external HTTP address of the ingress gateway (or the NodePort address,
	// when running under Minikube).
	HTTPAddress() net.TCPAddr
	// HTTPSAddress returns the external HTTPS address of the ingress gateway (or the
	// NodePort address, when running under Minikube).
	HTTPSAddress() net.TCPAddr
	// TCPAddress returns the external TCP address of the ingress gateway (or the NodePort address,
	// when running under Minikube).
	TCPAddress() net.TCPAddr

	//  Call makes a call through ingress.
	Call(options CallOptions) (CallResponse, error)
	CallOrFail(t test.Failer, options CallOptions) CallResponse

	// ProxyStats returns proxy stats, or error if failure happens.
	ProxyStats() (map[string]int, error)
}

type Config struct {
	Istio istio.Instance
	// IngressType specifies the type of ingress gateway.
	IngressType CallType
	// Cluster to be used in a multicluster environment
	Cluster resource.Cluster
}

// CallResponse is the result of a call made through Istio Ingress.
type CallResponse struct {
	// Response status code
	Code int

	// Response body
	Body string
}

// Deploy returns a new instance of echo.
func New(ctx resource.Context, cfg Config) (i Instance, err error) {
	return newKube(ctx, cfg), nil
}

// Deploy returns a new Ingress instance or fails test
func NewOrFail(t test.Failer, ctx resource.Context, cfg Config) Instance {
	t.Helper()
	i, err := New(ctx, cfg)
	if err != nil {
		t.Fatalf("ingress.NewOrFail: %v", err)
	}

	return i
}
