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
	"errors"
	"fmt"
	"net/http"
	"time"

	"istio.io/istio/pkg/test/echo/client"
	"istio.io/istio/pkg/test/echo/common/scheme"
	"istio.io/istio/pkg/test/framework/components/cluster"
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

	// Validator for server responses. If no validator is provided, only the number of responses received
	// will be verified.
	Validator Validator
}

// Validator validates that the given responses are expected.
type Validator interface {
	// Validate performs the validation check for this Validator.
	Validate(client.ParsedResponses, error) error
}

type validators []Validator

var _ Validator = validators{}

// Validate executes all validators in order, exiting on the first error encountered.
func (all validators) Validate(inResp client.ParsedResponses, err error) error {
	if len(all) == 0 {
		// By default, just assume no error.
		return expectNoError.Validate(inResp, err)
	}

	for _, v := range all {
		if e := v.Validate(inResp, err); e != nil {
			return e
		}
	}
	return nil
}

func (all validators) And(v Validator) Validator {
	if v == nil {
		return all
	}
	return append(append(validators{}, all...), v)
}

var (
	expectNoError = ValidatorFunc(func(resp client.ParsedResponses, err error) error {
		if err != nil {
			return fmt.Errorf("expected no error, but encountered: %v", err)
		}
		return nil
	})

	expectError = ValidatorFunc(func(resp client.ParsedResponses, err error) error {
		if err == nil {
			return errors.New("expected error, but none occurred")
		}
		return nil
	})

	identityValidator = ValidatorFunc(func(_ client.ParsedResponses, err error) error {
		return err
	})
)

// ExpectError returns a Validator that is completed when an error occurs.
func ExpectError() Validator {
	return expectError
}

// ExpectOK returns a Validator that calls CheckOK on the given responses.
func ExpectOK() Validator {
	return ValidatorFunc(func(resp client.ParsedResponses, err error) error {
		return resp.CheckOK()
	})
}

// ExpectReachedClusters returns a Validator that checks that all provided clusters are reached.
func ExpectReachedClusters(clusters cluster.Clusters) Validator {
	return ValidatorFunc(func(responses client.ParsedResponses, _ error) error {
		return responses.CheckReachedClusters(clusters)
	})
}

// ExpectCluster returns a validator that checks responses for the given cluster ID.
func ExpectCluster(expected string) Validator {
	return ValidatorFunc(func(responses client.ParsedResponses, _ error) error {
		return responses.CheckCluster(expected)
	})
}

// ExpectKey returns a validator that checks a key matches the provided value
func ExpectKey(key, expected string) Validator {
	return ValidatorFunc(func(responses client.ParsedResponses, _ error) error {
		return responses.CheckKey(key, expected)
	})
}

// ExpectHost returns a Validator that checks the responses for the given host header.
func ExpectHost(expected string) Validator {
	return ValidatorFunc(func(responses client.ParsedResponses, _ error) error {
		return responses.CheckHost(expected)
	})
}

// ExpectCode returns a Validator that checks the responses for the given response code.
func ExpectCode(expected string) Validator {
	return ValidatorFunc(func(responses client.ParsedResponses, _ error) error {
		return responses.CheckCode(expected)
	})
}

// ValidatorFunc is a function that serves as a Validator.
type ValidatorFunc func(client.ParsedResponses, error) error

var _ Validator = ValidatorFunc(func(client.ParsedResponses, error) error { return nil })

func (v ValidatorFunc) Validate(resp client.ParsedResponses, err error) error {
	return v(resp, err)
}

// And combines the validators into a chain. If no validators are provided, returns
// the identity validator that just returns the original error.
func And(vs ...Validator) Validator {
	out := make(validators, 0)

	for _, v := range vs {
		if v != nil {
			out = append(out, v)
		}
	}

	if len(out) == 0 {
		return identityValidator
	}

	if len(out) == 1 {
		return out[0]
	}

	return out
}
