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

package common

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"reflect"
	"strconv"

	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/test/echo/client"
	"istio.io/istio/pkg/test/echo/common"
	"istio.io/istio/pkg/test/echo/common/scheme"
	"istio.io/istio/pkg/test/echo/proto"
	"istio.io/istio/pkg/test/framework/components/echo"
)

var (
	// IdentityOutboundPortSelector is an OutboundPortSelectorFunc that always returns the original service port.
	IdentityOutboundPortSelector OutboundPortSelectorFunc = func(servicePort int) (int, error) {
		return servicePort, nil
	}
)

// OutboundPortSelectorFunc is a function that selects the appropriate outbound port for sending
// requests to a target service.
type OutboundPortSelectorFunc func(servicePort int) (int, error)

func CallEcho(c *client.Instance, opts *echo.CallOptions, outboundPortSelector OutboundPortSelectorFunc) (client.ParsedResponses, error) {
	if err := fillInCallOptions(opts); err != nil {
		return nil, err
	}

	port, err := outboundPortSelector(opts.Port.ServicePort)
	if err != nil {
		return nil, err
	}

	// Forward a request from 'this' service to the destination service.
	targetHost := net.JoinHostPort(opts.Host, strconv.Itoa(port))
	var targetURL string
	if opts.Scheme != scheme.TCP {
		targetURL = fmt.Sprintf("%s://%s%s", string(opts.Scheme), targetHost, opts.Path)
	} else {
		targetURL = fmt.Sprintf("%s://%s", string(opts.Scheme), targetHost)
	}
	protoHeaders := []*proto.Header{
		{
			Key:   "Host",
			Value: targetHost,
		},
	}
	// Add headers in opts.Headers, e.g., authorization header, etc.
	// If host header is set, it will override targetService.
	for k := range opts.Headers {
		protoHeaders = append(protoHeaders, &proto.Header{Key: k, Value: opts.Headers.Get(k)})
	}

	req := &proto.ForwardEchoRequest{
		Url:           targetURL,
		Count:         int32(opts.Count),
		Headers:       protoHeaders,
		TimeoutMicros: common.DurationToMicros(opts.Timeout),
		Message:       opts.Message,
		Http2:         opts.HTTP2,
		Cert:          opts.Cert,
		Key:           opts.Key,
	}

	resp, err := c.ForwardEcho(context.Background(), req)
	if err != nil {
		return nil, err
	}

	if len(resp) != opts.Count {
		return nil, fmt.Errorf("unexpected number of responses: expected %d, received %d", opts.Count, len(resp))
	}
	return resp, err
}

func fillInCallOptions(opts *echo.CallOptions) error {
	if opts.Target == nil {
		return errors.New("callOptions: missing Target")
	}

	targetPorts := opts.Target.Config().Ports
	if opts.PortName == "" {
		// Validate the Port value.

		if opts.Port == nil {
			return errors.New("callOptions: PortName or Port must be provided")
		}

		// Check the specified port for a match against the Target Instance
		found := false
		for _, port := range targetPorts {
			if reflect.DeepEqual(port, *opts.Port) {
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
		for _, port := range targetPorts {
			if opts.PortName == port.Name {
				found = true
				opts.Port = &port
				break
			}
		}
		if !found {
			return fmt.Errorf("callOptions: no port named %s available in Target Instance", opts.PortName)
		}
	}

	if opts.Scheme == "" {
		// No protocol, fill it in.
		var err error
		if opts.Scheme, err = schemeForPort(opts.Port); err != nil {
			return err
		}
	}

	if opts.Headers == nil {
		opts.Headers = make(http.Header)
	}

	if opts.Host == "" {
		// No host specified, use the fully qualified domain name for the service.
		opts.Host = opts.Target.Config().FQDN()
	}

	if opts.Timeout <= 0 {
		opts.Timeout = common.DefaultRequestTimeout
	}

	if opts.Count <= 0 {
		opts.Count = common.DefaultCount
	}

	return nil
}

func schemeForPort(port *echo.Port) (scheme.Instance, error) {
	switch port.Protocol {
	case protocol.GRPC, protocol.GRPCWeb, protocol.HTTP2:
		return scheme.GRPC, nil
	case protocol.HTTP:
		return scheme.HTTP, nil
	case protocol.HTTPS:
		return scheme.HTTPS, nil
	case protocol.TCP:
		return scheme.TCP, nil
	default:
		return "", fmt.Errorf("failed creating call for port %s: unsupported protocol %s",
			port.Name, port.Protocol)
	}
}
