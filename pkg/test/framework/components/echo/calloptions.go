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

	wrappers "google.golang.org/protobuf/types/known/wrapperspb"

	"istio.io/istio/pkg/http/headers"
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/echo/common"
	"istio.io/istio/pkg/test/echo/common/scheme"
	"istio.io/istio/pkg/test/util/retry"
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

// Retry settings
type Retry struct {
	// NoRetry if true, no retry will be attempted.
	NoRetry bool

	// Options to be used when retrying. If not specified, defaults will be used.
	Options []retry.Option
}

// TCP settings
type TCP struct {
	// ExpectedResponse asserts this is in the response for TCP requests.
	ExpectedResponse *wrappers.StringValue
}

// Target of a call.
type Target interface {
	Configurable
	WorkloadContainer

	// Instances in this target.
	Instances() Instances
}

// CallOptions defines options for calling a Endpoint.
type CallOptions struct {
	// To is the Target to be called.
	To Target

	// ToWorkload will call a specific workload in this instance, rather than the Service.
	// If there are multiple workloads in the Instance, the first is used.
	// Can be used with `ToWorkload: to.WithWorkloads(someWl)` to send to a specific workload.
	// When using the Port field, the ServicePort should be used.
	ToWorkload Instance

	// Port to be used for the call. Ignored if Scheme == DNS. If the Port.ServicePort is set,
	// either Port.Protocol or Scheme must also be set. If Port.ServicePort is not set,
	// the port is looked up in To by either Port.Name or Port.Protocol.
	Port Port

	// Scheme to be used when making the call. If not provided, the Scheme will be selected
	// based on the Port.Protocol.
	Scheme scheme.Instance

	// Address specifies the host name or IP address to be used on the request. If not provided,
	// an appropriate default is chosen for To.
	Address string

	// Count indicates the number of exchanges that should be made with the service endpoint.
	// If Count <= 0, defaults to 1.
	Count int

	// Timeout used for each individual request. Must be > 0, otherwise 5 seconds is used.
	Timeout time.Duration

	// Retry options for the call.
	Retry Retry

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
	Check Checker
}

// GetHost returns the best default host for the call. Returns the first host defined from the following
// sources (in order of precedence): Host header, target's DefaultHostHeader, Address, target's FQDN.
func (o CallOptions) GetHost() string {
	// First, use the host header, if specified.
	if h := o.HTTP.Headers.Get(headers.Host); len(h) > 0 {
		return h
	}

	// Next use the target's default, if specified.
	if o.To != nil && len(o.To.Config().DefaultHostHeader) > 0 {
		return o.To.Config().DefaultHostHeader
	}

	// Next, if the Address was manually specified use it as the Host.
	if len(o.Address) > 0 {
		return o.Address
	}

	// Finally, use the target's FQDN.
	if o.To != nil {
		return o.To.Config().ClusterLocalFQDN()
	}

	return ""
}

func (o CallOptions) DeepCopy() CallOptions {
	clone := o
	if o.TLS.Alpn != nil {
		clone.TLS.Alpn = make([]string, len(o.TLS.Alpn))
		copy(clone.TLS.Alpn, o.TLS.Alpn)
	}
	return clone
}

// FillDefaults fills out any defaults that haven't been explicitly specified.
func (o *CallOptions) FillDefaults() error {
	// Fill in the address if not set.
	if err := o.fillAddress(); err != nil {
		return err
	}

	// Fill in the port if not set or the service port is missing.
	if err := o.fillPort(); err != nil {
		return err
	}

	// Fill in the scheme if not set, using the port information.
	if err := o.fillScheme(); err != nil {
		return err
	}

	// Fill in HTTP headers
	o.fillHeaders()

	if o.Timeout <= 0 {
		o.Timeout = common.DefaultRequestTimeout
	}

	if o.Count <= 0 {
		o.Count = common.DefaultCount
	}

	// Add any user-specified options after the default options (last option wins for each type of option).
	o.Retry.Options = append(append([]retry.Option{}, DefaultCallRetryOptions()...), o.Retry.Options...)

	// If no Check was specified, assume no error.
	if o.Check == nil {
		o.Check = NoChecker()
	}
	return nil
}

// FillDefaultsOrFail calls FillDefaults and fails if an error occurs.
func (o *CallOptions) FillDefaultsOrFail(t test.Failer) {
	t.Helper()
	if err := o.FillDefaults(); err != nil {
		t.Fatal(err)
	}
}

func (o *CallOptions) fillAddress() error {
	if o.Address == "" {
		if o.To != nil {
			// No host specified, use the fully qualified domain name for the service.
			o.Address = o.To.Config().ClusterLocalFQDN()
			return nil
		}
		if o.ToWorkload != nil {
			wl, err := o.ToWorkload.Workloads()
			if err != nil {
				return err
			}
			o.Address = wl[0].Address()
			return nil
		}

		return errors.New("if address is not set, then To must be set")
	}
	return nil
}

func (o *CallOptions) fillPort() error {
	if o.Scheme == scheme.DNS {
		// Port is not used for DNS.
		return nil
	}

	if o.Port.ServicePort > 0 {
		if o.Port.Protocol == "" && o.Scheme == "" {
			return errors.New("callOptions: servicePort specified, but no protocol or scheme was set")
		}

		// The service port was set explicitly. Nothing else to do.
		return nil
	}

	if o.To != nil {
		return o.fillPort2(o.To)
	} else if o.ToWorkload != nil {
		err := o.fillPort2(o.ToWorkload)
		if err != nil {
			return err
		}
		// Set the ServicePort to workload port since we are not reaching it through the Service
		p := o.Port
		p.ServicePort = p.WorkloadPort
		o.Port = p
	}

	if o.Port.ServicePort <= 0 || (o.Port.Protocol == "" && o.Scheme == "") || o.Address == "" {
		return fmt.Errorf("if target is not set, then port.servicePort, port.protocol or schema, and address must be set")
	}

	return nil
}

func (o *CallOptions) fillPort2(target Target) error {
	servicePorts := target.Config().Ports.GetServicePorts()

	if o.Port.Name != "" {
		// Look up the port by name.
		p, found := servicePorts.ForName(o.Port.Name)
		if !found {
			return fmt.Errorf("callOptions: no port named %s available in To Instance", o.Port.Name)
		}
		o.Port = p
		return nil
	}

	if o.Port.Protocol != "" {
		// Look up the port by protocol.
		p, found := servicePorts.ForProtocol(o.Port.Protocol)
		if !found {
			return fmt.Errorf("callOptions: no port for protocol %s available in To Instance", o.Port.Protocol)
		}
		o.Port = p
		return nil
	}

	if o.Port.ServicePort != 0 {
		// We just have a single port number, populate the rest of the fields
		p, found := servicePorts.ForServicePort(o.Port.ServicePort)
		if !found {
			return fmt.Errorf("callOptions: no port %d available in To Instance", o.Port.ServicePort)
		}
		o.Port = p
		return nil
	}
	return nil
}

func (o *CallOptions) fillScheme() error {
	if o.Scheme == "" {
		// No protocol, fill it in.
		var err error
		if o.Scheme, err = o.Port.Scheme(); err != nil {
			return err
		}
	}
	return nil
}

func (o *CallOptions) fillHeaders() {
	if o.ToWorkload != nil {
		return
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
}
