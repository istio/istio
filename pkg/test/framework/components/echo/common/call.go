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

func callInternal(srcName string, from echo.Caller, opts echo.CallOptions, send sendFunc) (echo.CallResult, error) {
	// Create the proto request.
	req := newForwardRequest(opts)
	sendAndValidate := func() (echo.CallResult, error) {
		responses, err := send(req)

		// Verify the number of responses matches the expected.
		if err == nil && len(responses) != opts.Count {
			err = fmt.Errorf("unexpected number of responses: expected %d, received %d",
				opts.Count, len(responses))
		}

		// Convert to a CallResult.
		result := echo.CallResult{
			From:      from,
			Opts:      opts,
			Responses: responses,
		}

		// Return the results from the validator.
		err = opts.Check(result, err)
		if err != nil {
			err = fmt.Errorf("call failed from %s to %s (using %s): %v",
				srcName, getTargetURL(opts), opts.Scheme, err)
		}

		return result, err
	}

	if opts.Retry.NoRetry {
		// Retry is disabled, just send once.
		t0 := time.Now()
		defer scopes.Framework.Debugf("echo call complete with duration %v", time.Since(t0))
		return sendAndValidate()
	}

	// Retry the call until it succeeds or times out.
	var result echo.CallResult
	var err error
	_, _ = retry.UntilComplete(func() (any, bool, error) {
		result, err = sendAndValidate()
		if err != nil {
			return nil, false, err
		}
		return nil, true, nil
	}, opts.Retry.Options...)

	return result, err
}

type Caller struct {
	f *forwarder.Instance
}

func NewCaller() *Caller {
	return &Caller{
		f: forwarder.New(),
	}
}

func (c *Caller) Close() error {
	return c.f.Close()
}

func (c *Caller) CallEcho(from echo.Caller, opts echo.CallOptions) (echo.CallResult, error) {
	if err := opts.FillDefaults(); err != nil {
		return echo.CallResult{}, err
	}

	send := func(req *proto.ForwardEchoRequest) (echoclient.Responses, error) {
		ctx, cancel := context.WithTimeout(context.Background(), opts.Timeout)
		defer cancel()

		ret, err := c.f.ForwardEcho(ctx, &forwarder.Config{
			Request: req,
			Proxy:   opts.HTTP.HTTPProxy,
		})
		if err != nil {
			return nil, err
		}
		resp := echoclient.ParseResponses(req, ret)
		return resp, nil
	}
	return callInternal("TestRunner", from, opts, send)
}

func newForwardRequest(opts echo.CallOptions) *proto.ForwardEchoRequest {
	return &proto.ForwardEchoRequest{
		Url:                     getTargetURL(opts),
		Count:                   int32(opts.Count),
		Headers:                 common.HTTPToProtoHeaders(opts.HTTP.Headers),
		TimeoutMicros:           common.DurationToMicros(opts.Timeout),
		Message:                 opts.Message,
		ExpectedResponse:        opts.TCP.ExpectedResponse,
		Http2:                   opts.HTTP.HTTP2,
		Http3:                   opts.HTTP.HTTP3,
		Method:                  opts.HTTP.Method,
		ServerFirst:             opts.Port.ServerFirst,
		Cert:                    opts.TLS.Cert,
		Key:                     opts.TLS.Key,
		CaCert:                  opts.TLS.CaCert,
		CertFile:                opts.TLS.CertFile,
		KeyFile:                 opts.TLS.KeyFile,
		CaCertFile:              opts.TLS.CaCertFile,
		InsecureSkipVerify:      opts.TLS.InsecureSkipVerify,
		Alpn:                    getProtoALPN(opts.TLS.Alpn),
		FollowRedirects:         opts.HTTP.FollowRedirects,
		ServerName:              opts.TLS.ServerName,
		NewConnectionPerRequest: opts.NewConnectionPerRequest,
		ForceDNSLookup:          opts.ForceDNSLookup,
	}
}

func getProtoALPN(alpn []string) *proto.Alpn {
	if alpn != nil {
		return &proto.Alpn{
			Value: alpn,
		}
	}
	return nil
}

// EchoClientProvider provides dynamic creation of Echo clients. This allows retries to potentially make
// use of different (ready) workloads for forward requests.
type EchoClientProvider func() (*echoclient.Client, error)

func ForwardEcho(srcName string, from echo.Caller, opts echo.CallOptions, clientProvider EchoClientProvider) (echo.CallResult, error) {
	if err := opts.FillDefaults(); err != nil {
		return echo.CallResult{}, err
	}

	res, err := callInternal(srcName, from, opts, func(req *proto.ForwardEchoRequest) (echoclient.Responses, error) {
		c, err := clientProvider()
		if err != nil {
			return nil, err
		}
		return c.ForwardEcho(context.Background(), req)
	})
	if err != nil {
		return echo.CallResult{}, fmt.Errorf("failed calling %s->'%s': %v",
			srcName,
			getTargetURL(opts),
			err)
	}
	return res, nil
}

func getTargetURL(opts echo.CallOptions) string {
	port := opts.Port.ServicePort
	addressAndPort := net.JoinHostPort(opts.Address, strconv.Itoa(port))
	// Forward a request from 'this' service to the destination service.
	switch opts.Scheme {
	case scheme.DNS:
		return fmt.Sprintf("%s://%s", string(opts.Scheme), opts.Address)
	case scheme.TCP, scheme.GRPC:
		return fmt.Sprintf("%s://%s", string(opts.Scheme), addressAndPort)
	case scheme.XDS:
		return fmt.Sprintf("%s:///%s", string(opts.Scheme), addressAndPort)
	default:
		return fmt.Sprintf("%s://%s%s", string(opts.Scheme), addressAndPort, opts.HTTP.Path)
	}
}
