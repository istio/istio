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
	"strings"
	"time"

	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/test/echo/client"
	"istio.io/istio/pkg/test/echo/common"
	"istio.io/istio/pkg/test/echo/common/scheme"
	"istio.io/istio/pkg/test/echo/proto"
	"istio.io/istio/pkg/test/echo/server/forwarder"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/scopes"
	"istio.io/istio/pkg/test/util/retry"
)

type sendFunc func(req *proto.ForwardEchoRequest) (client.ParsedResponses, error)

func callInternal(srcName string, opts *echo.CallOptions, send sendFunc,
	doRetry bool, retryOptions ...retry.Option) (client.ParsedResponses, error) {
	if err := fillInCallOptions(opts); err != nil {
		return nil, err
	}

	var targetURL string
	port := opts.Port.ServicePort
	addressAndPort := net.JoinHostPort(opts.Address, strconv.Itoa(port))
	// Forward a request from 'this' service to the destination service.
	switch opts.Scheme {
	case scheme.DNS:
		targetURL = fmt.Sprintf("%s://%s", string(opts.Scheme), opts.Address)
	case scheme.TCP:
		targetURL = fmt.Sprintf("%s://%s", string(opts.Scheme), addressAndPort)
	default:
		targetURL = fmt.Sprintf("%s://%s%s", string(opts.Scheme), addressAndPort, opts.Path)
	}

	// Copy all the headers.
	protoHeaders := make([]*proto.Header, 0, len(opts.Headers))
	for k := range opts.Headers {
		protoHeaders = append(protoHeaders, &proto.Header{Key: k, Value: opts.Headers.Get(k)})
	}

	req := &proto.ForwardEchoRequest{
		Url:                targetURL,
		Count:              int32(opts.Count),
		Headers:            protoHeaders,
		TimeoutMicros:      common.DurationToMicros(opts.Timeout),
		Message:            opts.Message,
		Http2:              opts.HTTP2,
		Method:             opts.Method,
		ServerFirst:        opts.Port.ServerFirst,
		Cert:               opts.Cert,
		Key:                opts.Key,
		CaCert:             opts.CaCert,
		CertFile:           opts.CertFile,
		KeyFile:            opts.KeyFile,
		CaCertFile:         opts.CaCertFile,
		InsecureSkipVerify: opts.InsecureSkipVerify,
		FollowRedirects:    opts.FollowRedirects,
	}

	var responses client.ParsedResponses
	sendAndValidate := func() error {
		var err error
		responses, err = send(req)

		// Verify the number of responses matches the expected.
		if err == nil {
			if len(responses) != opts.Count {
				err = fmt.Errorf("unexpected number of responses: expected %d, received %d",
					opts.Count, len(responses))
			}
		}

		// Return the results from the validator.
		return opts.Validator.Validate(responses, err)
	}

	formatError := func(err error) error {
		if err != nil {
			return fmt.Errorf("call failed from %s to %s (using %s): %v", srcName, targetURL, opts.Scheme, err)
		}
		return nil
	}

	if doRetry {
		// Add defaults retry options to the beginning, since last option encountered wins.
		retryOptions = append(append([]retry.Option{}, echo.DefaultCallRetryOptions()...), retryOptions...)
		err := retry.UntilSuccess(sendAndValidate, retryOptions...)
		return responses, formatError(err)
	}

	t0 := time.Now()
	// Retry not enabled for this call.
	err := sendAndValidate()
	scopes.Framework.Debugf("echo call complete with duration %v", time.Since(t0))
	return responses, formatError(err)
}

func CallEcho(opts *echo.CallOptions, retry bool, retryOptions ...retry.Option) (client.ParsedResponses, error) {
	send := func(req *proto.ForwardEchoRequest) (client.ParsedResponses, error) {
		instance, err := forwarder.New(forwarder.Config{
			Request: req,
		})
		if err != nil {
			return nil, err
		}
		defer instance.Close()

		ret, err := instance.Run(context.Background())
		if err != nil {
			return nil, err
		}
		resp := client.ParseForwardedResponse(req, ret)
		return resp, nil
	}
	return callInternal("TestRunner", opts, send, retry, retryOptions...)
}

// EchoClientProvider provides dynamic creation of Echo clients. This allows retries to potentially make
// use of different (ready) workloads for forward requests.
type EchoClientProvider func() (*client.Instance, error)

func ForwardEcho(srcName string, clientProvider EchoClientProvider, opts *echo.CallOptions,
	retry bool, retryOptions ...retry.Option) (client.ParsedResponses, error) {
	res, err := callInternal(srcName, opts, func(req *proto.ForwardEchoRequest) (client.ParsedResponses, error) {
		c, err := clientProvider()
		if err != nil {
			return nil, err
		}
		return c.ForwardEcho(context.Background(), req)
	}, retry, retryOptions...)
	if err != nil {
		if opts.Port != nil {
			err = fmt.Errorf("failed calling %s->'%s://%s:%d/%s': %v",
				srcName,
				strings.ToLower(string(opts.Port.Protocol)),
				opts.Address,
				opts.Port.ServicePort,
				opts.Path,
				err)
		}
		return nil, err
	}
	return res, nil
}

func fillInCallOptions(opts *echo.CallOptions) error {
	if opts.Target != nil {
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
	} else if opts.Scheme == scheme.DNS {
		// Just need address
		if opts.Address == "" {
			return fmt.Errorf("for DNS, address must be set")
		}
		opts.Port = &echo.Port{}
	} else if opts.Port == nil || opts.Port.ServicePort == 0 || (opts.Port.Protocol == "" && opts.Scheme == "") || opts.Address == "" {
		return fmt.Errorf("if target is not set, then port.servicePort, port.protocol or schema, and address must be set")
	}

	if opts.Scheme == "" {
		// No protocol, fill it in.
		var err error
		if opts.Scheme, err = schemeForPort(opts.Port); err != nil {
			return err
		}
	}

	if opts.Address == "" {
		// No host specified, use the fully qualified domain name for the service.
		opts.Address = opts.Target.Config().FQDN()
	}

	// Initialize the headers and add a default Host header if none provided.
	if opts.Headers == nil {
		opts.Headers = make(http.Header)
	}
	if h := opts.Headers["Host"]; len(h) == 0 && opts.Target != nil {
		// No host specified, use the hostname for the service.
		opts.Headers["Host"] = []string{opts.Target.Config().HostHeader()}
	}

	if opts.Timeout <= 0 {
		opts.Timeout = common.DefaultRequestTimeout
	}

	if opts.Count <= 0 {
		opts.Count = common.DefaultCount
	}

	// This is a quick and dirty way of getting the identity validator if the validator was not set.
	opts.Validator = echo.And(opts.Validator)
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
