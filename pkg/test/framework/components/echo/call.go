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

	// Host specifies the host to be used on the request. If not provided, an appropriate
	// default is chosen for the target Instance.
	Host string

	// Path specifies the URL path for the HTTP(s) request.
	Path string

	// Count indicates the number of exchanges that should be made with the service endpoint.
	// If Count <= 0, defaults to 1.
	Count int

	// Headers indicates headers that should be sent in the request. Ignored for WebSocket calls.
	Headers http.Header

	// Timeout used for each individual request. Must be > 0, otherwise 30 seconds is used.
	Timeout time.Duration

	// Message to be sent if this is a GRPC request
	Message string

	// Use the custom certificate to make the call. This is mostly used to make mTLS request directly
	// (without proxy) from naked client to test certificates issued by custom CA instead of the Istio self-signed CA.
	Cert, Key string
}
