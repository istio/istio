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
	"fmt"
	"net/http"
	"time"

	"istio.io/istio/pkg/test/echo/client"
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

	// HostHeader specifies the "Host" header to be used on a request. This will override the Host
	// field if set, in which case Host will be used for DNS resolution while HostHeader will be set
	// as a header.
	HostHeader string

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

	// Method to send. Defaults to HTTP. Only relevant for HTTP.
	Method string

	// Use the custom certificate to make the call. This is mostly used to make mTLS request directly
	// (without proxy) from naked client to test certificates issued by custom CA instead of the Istio self-signed CA.
	Cert, Key, CaCert string

	// Validators is a list of validators for server responses. If empty, only the number of responses received
	// will be verified.
	Validators Validators
}

// Validator validates that the given responses are expected.
type Validator func(client.ParsedResponses) error

type Validators []Validator

// NewValidators creates an empty Validators array.
func NewValidators() Validators {
	return make(Validators, 0)
}

// Validate executes all validators in order, exiting on the first error encountered.
func (all Validators) Validate(responses client.ParsedResponses) error {
	for _, v := range all {
		if err := v(responses); err != nil {
			return err
		}
	}
	return nil
}

// WithOK returns a copy of this Validators with ValidateOK added.
func (all Validators) WithOK() Validators {
	return all.With(ValidateOK)
}

// WithCount returns a copy of this Validators with validation for the given
// expected response count.
func (all Validators) WithCount(expectedCount int) Validators {
	return all.With(func(responses client.ParsedResponses) error {
		if len(responses) != expectedCount {
			return fmt.Errorf("unexpected number of responses: expected %d, received %d",
				expectedCount, len(responses))
		}
		return nil
	})
}

// With returns a copy of this Validators with the given validator added.
func (all Validators) With(v Validator) Validators {
	return append(append(NewValidators(), all...), v)
}

var _ Validator = ValidateOK

// ValidateOK is a Validator that calls CheckOK on the given responses.
func ValidateOK(responses client.ParsedResponses) error {
	return responses.CheckOK()
}
