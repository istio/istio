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
	"reflect"
	"time"

	wrappers "google.golang.org/protobuf/types/known/wrapperspb"

	"istio.io/istio/pkg/http/headers"
	"istio.io/istio/pkg/test/echo/check"
	"istio.io/istio/pkg/test/echo/common"
	"istio.io/istio/pkg/test/echo/common/scheme"
)

// HTTP settings
type HTTP struct {
	// If true, h2c will be used in HTTP requests
	HTTP2 bool

	// If true, HTTP/3 request over QUIC will be used.
	// It is mandatory to specify TLS settings
	HTTP3 bool

	// Path specifies the URL path for the HTTP(s) request.
	Path string

	// Method to send. Defaults to GET.
	Method string

	// Headers indicates headers that should be sent in the request. Ignored for WebSocket calls.
	// If no Host header is provided, a default will be chosen for the target service endpoint.
	Headers http.Header

	// FollowRedirects will instruct the call to follow 301 redirects. Otherwise, the original 301 response
	// is returned directly.
	FollowRedirects bool

	// HTTProxy used for making ingress echo call via proxy
	HTTPProxy string
}

// TLS settings
type TLS struct {
	// Use the custom certificate to make the call. This is mostly used to make mTLS request directly
	// (without proxy) from naked client to test certificates issued by custom CA instead of the Istio self-signed CA.
	Cert, Key, CaCert string

	// Use the custom certificates file to make the call.
	CertFile, KeyFile, CaCertFile string

	// Skip verify peer's certificate.
	InsecureSkipVerify bool

	Alpn       []string
	ServerName string
}

// TCP settings
type TCP struct {
	// ExpectedResponse asserts this is in the response for TCP requests.
	ExpectedResponse *wrappers.StringValue
}

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

	// Address specifies the host name or IP address to be used on the request. If not provided,
	// an appropriate default is chosen for the target Instance.
	Address string

	// Count indicates the number of exchanges that should be made with the service endpoint.
	// If Count <= 0, defaults to 1.
	Count int

	// Timeout used for each individual request. Must be > 0, otherwise 5 seconds is used.
	Timeout time.Duration

	// HTTP settings.
	HTTP HTTP

	// TCP settings.
	TCP TCP

	// TLS settings.
	TLS TLS

	// Message to be sent.
	Message string

	// Check the server responses. If none is provided, only the number of responses received
	// will be checked.
	Check check.Checker
}

// GetHost returns the best default host for the call. Returns the first host defined from the following
// sources (in order of precedence): Host header, target's DefaultHostHeader, Address, target's FQDN.
func (o CallOptions) GetHost() string {
	// First, use the host header, if specified.
	if h := o.HTTP.Headers["Host"]; len(h) > 0 {
		return o.HTTP.Headers["Host"][0]
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
	if o.TLS.Alpn != nil {
		clone.TLS.Alpn = make([]string, len(o.TLS.Alpn))
		copy(clone.TLS.Alpn, o.TLS.Alpn)
	}
	return clone
}

// FillDefaults fills out any defaults that haven't been explicitly specified.
func (o *CallOptions) FillDefaults() error {
	if o.Target != nil {
		targetPorts := o.Target.Config().Ports
		if o.PortName == "" {
			// Validate the Port value.

			if o.Port == nil {
				return errors.New("callOptions: PortName or Port must be provided")
			}

			// Check the specified port for a match against the Target Instance
			found := false
			for _, port := range targetPorts {
				if reflect.DeepEqual(port, *o.Port) {
					found = true
					break
				}
			}
			if !found {
				return fmt.Errorf("callOptions: Port does not match any Target port")
			}
		} else {
			// Look up the port.
			found := false
			for i, port := range targetPorts {
				if o.PortName == port.Name {
					found = true
					o.Port = &targetPorts[i]
					break
				}
			}
			if !found {
				return fmt.Errorf("callOptions: no port named %s available in Target Instance", o.PortName)
			}
		}
	} else if o.Scheme == scheme.DNS {
		// Just need address
		if o.Address == "" {
			return fmt.Errorf("for DNS, address must be set")
		}
		o.Port = &Port{}
	} else if o.Port == nil || o.Port.ServicePort == 0 || (o.Port.Protocol == "" && o.Scheme == "") || o.Address == "" {
		return fmt.Errorf("if target is not set, then port.servicePort, port.protocol or schema, and address must be set")
	}

	if o.Scheme == "" {
		// No protocol, fill it in.
		var err error
		if o.Scheme, err = o.Port.Scheme(); err != nil {
			return err
		}
	}

	if o.Address == "" {
		// No host specified, use the fully qualified domain name for the service.
		o.Address = o.Target.Config().ClusterLocalFQDN()
	}

	// Initialize the headers and add a default Host header if none provided.
	if o.HTTP.Headers == nil {
		o.HTTP.Headers = make(http.Header)
	} else {
		// Avoid mutating input, which can lead to concurrent writes
		o.HTTP.Headers = o.HTTP.Headers.Clone()
	}

	if h := o.GetHost(); len(h) > 0 {
		o.HTTP.Headers.Set(headers.Host, h)
	}

	if o.Timeout <= 0 {
		o.Timeout = common.DefaultRequestTimeout
	}

	if o.Count <= 0 {
		o.Count = common.DefaultCount
	}

	// If no Check was specified, assume no error.
	if o.Check == nil {
		o.Check = check.None()
	}
	return nil
}
