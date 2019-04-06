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

	appEcho "istio.io/istio/pkg/test/application/echo"
	"istio.io/istio/pkg/test/application/echo/proto"
	"istio.io/istio/pkg/test/framework/components/echo"
)

func CallEcho(client *appEcho.Client, opts echo.CallOptions) (appEcho.ParsedResponses, error) {
	if err := fillInCallOptions(&opts); err != nil {
		return nil, err
	}

	// Forward a request from 'this' service to the destination service.
	targetURL := makeURL(opts)
	targetService := opts.Target.Config().Service

	var headers []*proto.Header
	headers = append(headers, &proto.Header{Key: "Host", Value: targetService})
	for key, values := range opts.Headers {
		for _, value := range values {
			headers = append(headers, &proto.Header{Key: key, Value: value})
		}
	}

	req := &proto.ForwardEchoRequest{
		Url:     targetURL.String(),
		Count:   int32(opts.Count),
		Headers: headers,
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

	if opts.Host == "" {
		// No host specified, use the fully qualified domain name for the service.
		opts.Host = opts.Target.Config().FQDN()
	}

	if opts.Count <= 0 {
		opts.Count = 1
	}

	return nil
}

func makeURL(opts echo.CallOptions) *url.URL {
	protocol := string(normalizeProtocol(opts.Protocol))
	if opts.Secure {
		protocol += "s"
	}

	return &url.URL{
		Scheme: protocol,
		Host:   net.JoinHostPort(opts.Host, strconv.Itoa(opts.Port.ServicePort)),
		Path:   opts.Path,
	}
}

func normalizeProtocol(p echo.CallProtocol) echo.CallProtocol {
	switch p {
	case echo.HTTP, echo.GRPC, echo.WebSocket:
		return p
	default:
		return echo.HTTP
	}
}
