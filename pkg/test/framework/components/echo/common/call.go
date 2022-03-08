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
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	echoclient "istio.io/istio/pkg/test/echo"
	"istio.io/istio/pkg/test/echo/common"
	"istio.io/istio/pkg/test/echo/common/scheme"
	"istio.io/istio/pkg/test/echo/proto"
	"istio.io/istio/pkg/test/echo/server/forwarder"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/scopes"
	"istio.io/istio/pkg/test/util/retry"
)

type sendFunc func(req *proto.ForwardEchoRequest) (echoclient.Responses, error)

func callInternal(srcName string, opts *echo.CallOptions, send sendFunc) (echoclient.Responses, error) {
	if err := opts.FillDefaults(); err != nil {
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
	case scheme.XDS:
		targetURL = fmt.Sprintf("%s:///%s", string(opts.Scheme), addressAndPort)
	default:
		targetURL = fmt.Sprintf("%s://%s%s", string(opts.Scheme), addressAndPort, opts.HTTP.Path)
	}

	// Copy all the headers.
	protoHeaders := common.HTTPToProtoHeaders(opts.HTTP.Headers)

	req := &proto.ForwardEchoRequest{
		Url:                targetURL,
		Count:              int32(opts.Count),
		Headers:            protoHeaders,
		TimeoutMicros:      common.DurationToMicros(opts.Timeout),
		Message:            opts.Message,
		ExpectedResponse:   opts.TCP.ExpectedResponse,
		Http2:              opts.HTTP.HTTP2,
		Http3:              opts.HTTP.HTTP3,
		Method:             opts.HTTP.Method,
		ServerFirst:        opts.Port.ServerFirst,
		Cert:               opts.TLS.Cert,
		Key:                opts.TLS.Key,
		CaCert:             opts.TLS.CaCert,
		CertFile:           opts.TLS.CertFile,
		KeyFile:            opts.TLS.KeyFile,
		CaCertFile:         opts.TLS.CaCertFile,
		InsecureSkipVerify: opts.TLS.InsecureSkipVerify,
		FollowRedirects:    opts.HTTP.FollowRedirects,
		ServerName:         opts.TLS.ServerName,
	}
	if opts.TLS.Alpn != nil {
		req.Alpn = &proto.Alpn{
			Value: opts.TLS.Alpn,
		}
	}

	var responses echoclient.Responses
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
		return opts.Check(responses, err)
	}

	formatError := func(err error) error {
		if err != nil {
			return fmt.Errorf("call failed from %s to %s (using %s): %v", srcName, targetURL, opts.Scheme, err)
		}
		return nil
	}

	if !opts.Retry.NoRetry {
		// Add defaults retry options to the beginning, since last option encountered wins.
		err := retry.UntilSuccess(sendAndValidate, opts.Retry.Options...)
		return responses, formatError(err)
	}

	t0 := time.Now()
	// Retry not enabled for this call.
	err := sendAndValidate()
	scopes.Framework.Debugf("echo call complete with duration %v", time.Since(t0))
	return responses, formatError(err)
}

func CallEcho(opts *echo.CallOptions) (echoclient.Responses, error) {
	send := func(req *proto.ForwardEchoRequest) (echoclient.Responses, error) {
		instance, err := forwarder.New(forwarder.Config{
			Request: req,
			Proxy:   opts.HTTP.HTTPProxy,
		})
		if err != nil {
			return nil, err
		}
		defer instance.Close()
		ctx, cancel := context.WithTimeout(context.Background(), opts.Timeout)
		defer cancel()
		ret, err := instance.Run(ctx)
		if err != nil {
			return nil, err
		}
		resp := echoclient.ParseResponses(req, ret)
		return resp, nil
	}
	return callInternal("TestRunner", opts, send)
}

// EchoClientProvider provides dynamic creation of Echo clients. This allows retries to potentially make
// use of different (ready) workloads for forward requests.
type EchoClientProvider func() (*echoclient.Client, error)

func ForwardEcho(srcName string, clientProvider EchoClientProvider, opts *echo.CallOptions) (echoclient.Responses, error) {
	res, err := callInternal(srcName, opts, func(req *proto.ForwardEchoRequest) (echoclient.Responses, error) {
		c, err := clientProvider()
		if err != nil {
			return nil, err
		}
		return c.ForwardEcho(context.Background(), req)
	})
	if err != nil {
		return nil, fmt.Errorf("failed calling %s->'%s://%s:%d/%s': %v",
			srcName,
			strings.ToLower(string(opts.Port.Protocol)),
			opts.Address,
			opts.Port.ServicePort,
			opts.HTTP.Path,
			err)
	}
	return res, nil
}
