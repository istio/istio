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

package echo

import (
	"net/http"
	"time"

	wrappers "google.golang.org/protobuf/types/known/wrapperspb"

	"istio.io/istio/pkg/test/echo/check"
	"istio.io/istio/pkg/test/echo/common/scheme"
)

// CallOptions defines options for calling a Endpoint.
type CallOptions struct {
	// Target instance of the call. Required.
	Target Instance

	// Port on the target Instance. Either Port or PortName must be specified.
	Port *Port

	// PortName of the port on the target Instance. Either Port or PortName must be specified.
	PortName string

	// Scheme to be used when making the call. If not provided, an appropriate default for the
	// port will be used (if feasible).
	Scheme scheme.Instance

	// If true, h2c will be used in HTTP requests
	HTTP2 bool

	// If true, HTTP/3 request over QUIC will be used.
	// It is mandatory to specify TLS settings
	HTTP3 bool

	// Address specifies the host name or IP address to be used on the request. If not provided,
	// an appropriate default is chosen for the target Instance.
	Address string

	// Path specifies the URL path for the HTTP(s) request.
	Path string

	// Count indicates the number of exchanges that should be made with the service endpoint.
	// If Count <= 0, defaults to 1.
	Count int

	// Headers indicates headers that should be sent in the request. Ignored for WebSocket calls.
	// If no Host header is provided, a default will be chosen for the target service endpoint.
	Headers http.Header

	// Timeout used for each individual request. Must be > 0, otherwise 5 seconds is used.
	Timeout time.Duration

	// Message to be sent if this is a GRPC request
	Message string

	// ExpectedResponse asserts this is in the response for TCP requests.
	ExpectedResponse *wrappers.StringValue

	// Method to send. Defaults to HTTP. Only relevant for HTTP.
	Method string

	// Use the custom certificate to make the call. This is mostly used to make mTLS request directly
	// (without proxy) from naked client to test certificates issued by custom CA instead of the Istio self-signed CA.
	Cert, Key, CaCert string

	// Use the custom certificates file to make the call.
	CertFile, KeyFile, CaCertFile string

	// Skip verify peer's certificate.
	InsecureSkipVerify bool

	// FollowRedirects will instruct the call to follow 301 redirects. Otherwise, the original 301 response
	// is returned directly.
	FollowRedirects bool

	// Check the server responses. If none is provided, only the number of responses received
	// will be checked.
	Check check.Checker

	// HTTProxy used for making ingress echo call via proxy
	HTTPProxy string

	Alpn       []string
	ServerName string
}

// GetHost returns the best default host for the call. Returns the first host defined from the following
// sources (in order of precedence): Host header, target's DefaultHostHeader, Address, target's FQDN.
func (o CallOptions) GetHost() string {
	// First, use the host header, if specified.
	if h := o.Headers["Host"]; len(h) > 0 {
		return o.Headers["Host"][0]
	}

	// Next use the target's default, if specified.
	if o.Target != nil && len(o.Target.Config().DefaultHostHeader) > 0 {
		return o.Target.Config().DefaultHostHeader
	}

	// Next, if the Address was manually specified use it as the Host.
	if len(o.Address) > 0 {
		return o.Address
	}

	// Finally, use the target's FQDN.
	if o.Target != nil {
		return o.Target.Config().ClusterLocalFQDN()
	}

	return ""
}

func (o CallOptions) DeepCopy() CallOptions {
	clone := o
	if o.Port != nil {
		dc := *o.Port
		clone.Port = &dc
	}
	if o.Alpn != nil {
		clone.Alpn = make([]string, len(o.Alpn))
		copy(clone.Alpn, o.Alpn)
	}
	return clone
}
