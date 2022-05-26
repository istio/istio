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
	// If Count <= 0, a default will be selected. If To is specified, the value will be set to
	// the numWorkloads * DefaultCallsPerWorkload. Otherwise, defaults to 1.
	Count int

	// Timeout used for each individual request. Must be > 0, otherwise 5 seconds is used.
	Timeout time.Duration

	// NewConnectionPerRequest if true, the forwarder will establish a new connection to the server for
	// each individual request. If false, it will attempt to reuse the same connection for the duration
	// of the forward call. This is ignored for DNS, TCP, and TLS protocols, as well as
	// Headless/StatefulSet deployments.
	NewConnectionPerRequest bool

	// ForceDNSLookup if true, the forwarder will force a DNS lookup for each individual request. This is
	// useful for any situation where DNS is used for load balancing (e.g. headless). This is ignored if
	// NewConnectionPerRequest is false or if the deployment is Headless or StatefulSet.
	ForceDNSLookup bool

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

	// Fill the number of calls to make.
	o.fillCallCount()

	// Fill connection parameters based on scheme and workload type.
	o.fillConnectionParams()

	// Fill in default retry options, if not specified.
	o.fillRetryOptions()

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

func (o *CallOptions) fillCallCount() {
	if o.Count > 0 {
		// Nothing to do.
		return
	}

	o.Count = common.DefaultCount

	// Try setting an appropriate count for the number of workloads.
	newCount := DefaultCallsPerWorkload() * o.numWorkloads()
	if newCount > o.Count {
		o.Count = newCount
	}
}

func (o *CallOptions) numWorkloads() int {
	if o.To == nil {
		return 0
	}
	workloads, err := o.To.Workloads()
	if err != nil {
		return 0
	}
	return len(workloads)
}

func (o *CallOptions) fillConnectionParams() {
	// Overrides connection parameters for scheme.
	switch o.Scheme {
	case scheme.DNS:
		o.NewConnectionPerRequest = true
		o.ForceDNSLookup = true
	case scheme.TCP, scheme.TLS, scheme.WebSocket:
		o.NewConnectionPerRequest = true
	}

	// Override connection parameters for workload type.
	if o.To != nil {
		toCfg := o.To.Config()
		if toCfg.IsHeadless() || toCfg.IsStatefulSet() {
			// Headless uses DNS for load balancing. Force DNS lookup each time so
			// that we get proper load balancing behavior.
			o.NewConnectionPerRequest = true
			o.ForceDNSLookup = true
		}
	}

	// ForceDNSLookup only applies when using new connections per request.
	o.ForceDNSLookup = o.NewConnectionPerRequest && o.ForceDNSLookup
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

func (o *CallOptions) fillRetryOptions() {
	if o.Retry.NoRetry {
		// User specified no-retry, nothing to do.
		return
	}

	// NOTE: last option wins, so order in the list is important!

	// Start by getting the defaults.
	retryOpts := DefaultCallRetryOptions()

	// Don't use converge unless we need it. When sending large batches of requests (for example,
	// when we attempt to reach all clusters), converging will result in sending at least
	// `converge * count` requests. When running multiple requests in parallel, this can contribute
	// to resource (e.g. port) exhaustion in the echo servers. To avoid that problem, we disable
	// converging by default, so long as the count is greater than the default converge value.
	// This, of course, can be overridden if the user supplies their own converge value.
	if o.Count > callConverge {
		retryOpts = append(retryOpts, retry.Converge(1))
	}

	// Now append user-provided options to override the defaults.
	o.Retry.Options = append(retryOpts, o.Retry.Options...)
}
