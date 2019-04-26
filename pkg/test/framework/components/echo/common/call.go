// Copyright 2019 Istio Authors
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
	"errors"
	"fmt"
	"net"
	"net/url"
	"reflect"
	"strconv"

	"istio.io/istio/pilot/pkg/model"
	appEcho "istio.io/istio/pkg/test/application/echo"
	"istio.io/istio/pkg/test/application/echo/proto"
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

func CallEcho(client *appEcho.Client, opts *echo.CallOptions, outboundPortSelector OutboundPortSelectorFunc) (appEcho.ParsedResponses, error) {
	if err := fillInCallOptions(opts); err != nil {
		return nil, err
	}

	port, err := outboundPortSelector(opts.Port.ServicePort)
	if err != nil {
		return nil, err
	}

	// Forward a request from 'this' service to the destination service.
	targetURL := &url.URL{
		Scheme: string(opts.Protocol),
		Host:   net.JoinHostPort(opts.Host, strconv.Itoa(port)),
		Path:   opts.Path,
	}
	targetService := opts.Target.Config().Service

	protoHeaders := []*proto.Header{
		{
			Key:   "Host",
			Value: targetService,
		},
	}
	// Add headers in opts.Headers, e.g., authorization header, etc.
	// If host header is set, it will override targetService.
	for k := range opts.Headers {
		protoHeaders = append(protoHeaders, &proto.Header{Key: k, Value: opts.Headers.Get(k)})
	}

	req := &proto.ForwardEchoRequest{
		Url:     targetURL.String(),
		Count:   int32(opts.Count),
		Headers: protoHeaders,
	}

	resp, err := client.ForwardEcho(req)
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
			if reflect.DeepEqual(port, opts.Port) {
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

	if opts.Protocol == "" {
		// No protocol, fill it in.
		var err error
		if opts.Protocol, err = protocolForPort(opts.Port); err != nil {
			return err
		}
	}

	if opts.Host == "" {
		// No host specified, use the fully qualified domain name for the service.
		opts.Host = opts.Target.Config().FQDN()
	}

	if opts.Count <= 0 {
		opts.Count = 1
	}

	return nil
}

func protocolForPort(port *echo.Port) (echo.CallProtocol, error) {
	switch port.Protocol {
	case model.ProtocolGRPC, model.ProtocolGRPCWeb, model.ProtocolHTTP2:
		return echo.GRPC, nil
	case model.ProtocolHTTP, model.ProtocolTCP:
		return echo.HTTP, nil
	case model.ProtocolHTTPS, model.ProtocolTLS:
		return echo.HTTPS, nil
	default:
		return "", fmt.Errorf("failed creating call for port %s: unsupported protocol %s",
			port.Name, port.Protocol)
	}
}
