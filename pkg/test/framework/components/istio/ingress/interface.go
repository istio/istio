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

	"istio.io/istio/pkg/test"
)

// CallType defines ingress gateway type
type CallType int

const (
	PlainText CallType = iota
	TLS
	Mtls
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

// Sanitize checks and fills fields in CallOptions. Returns error on failures, and nil otherwise.
func (o *CallOptions) Sanitize() error {
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
	// HTTPAddress returns the external HTTP (80) address of the ingress gateway ((or the NodePort address,
	//	// when in an environment that doesn't support LoadBalancer).
	HTTPAddress() net.TCPAddr
	// HTTPSAddress returns the external HTTPS (443) address of the ingress gateway (or the NodePort address,
	//	// when in an environment that doesn't support LoadBalancer).
	HTTPSAddress() net.TCPAddr
	// TCPAddress returns the external TCP (31400) address of the ingress gateway (or the NodePort address,
	// when in an environment that doesn't support LoadBalancer).
	TCPAddress() net.TCPAddr
	// DiscoveryAddress returns the external XDS (!5012) address on the ingress gateway (or the NodePort address,
	// when in an evnironment that doesn't support LoadBalancer).
	DiscoveryAddress() net.TCPAddr

	// Call makes a call through ingress.
	Call(options CallOptions) (CallResponse, error)
	CallOrFail(t test.Failer, options CallOptions) CallResponse

	// ProxyStats returns proxy stats, or error if failure happens.
	ProxyStats() (map[string]int, error)

	// CloseClients closes all clients the ingress has created. The object can still be used with new clients.
	CloseClients()

	// PodID returns the name of the ingress gateway pod of index i. Returns error if failed to get the pod
	// or the index is out of boundary.
	PodID(i int) (string, error)
}

// CallResponse is the result of a call made through Istio Instance.
type CallResponse struct {
	// Response status code
	Code int

	// Response body
	Body string
}
