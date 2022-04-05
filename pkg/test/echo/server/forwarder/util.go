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

package forwarder

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/hashicorp/go-multierror"

	"istio.io/istio/pkg/test/echo"
	"istio.io/istio/pkg/test/echo/common"
	"istio.io/istio/pkg/test/echo/proto"
	"istio.io/pkg/log"
)

const (
	hostHeader = "Host"
)

var fwLog = log.RegisterScope("forwarder", "echo clientside", 0)

func writeForwardedHeaders(out *bytes.Buffer, requestID int, header http.Header) {
	for key, values := range header {
		for _, v := range values {
			echo.ForwarderHeaderField.WriteKeyValueForRequest(out, requestID, key, v)
		}
	}
}

func newDialer(forceDNSLookup bool) *net.Dialer {
	out := &net.Dialer{
		Timeout: common.ConnectionTimeout,
	}
	if forceDNSLookup {
		out.Resolver = newResolver(common.ConnectionTimeout, "", "")
	}
	return out
}

func newResolver(timeout time.Duration, protocol, dnsServer string) *net.Resolver {
	return &net.Resolver{
		PreferGo: true,
		Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
			d := net.Dialer{
				Timeout: timeout,
			}
			nt := protocol
			if nt == "" {
				nt = network
			}
			addr := dnsServer
			if addr == "" {
				addr = address
			}
			return d.DialContext(ctx, nt, addr)
		},
	}
}

// doForward sends the requests and collect the responses.
func doForward(ctx context.Context, cfg *Config, e *executor, doReq func(context.Context, *Config, int) (string, error)) (*proto.ForwardEchoResponse, error) {
	if err := cfg.fillDefaults(); err != nil {
		return nil, err
	}

	// make the timeout apply to the entire set of requests
	ctx, cancel := context.WithTimeout(ctx, cfg.timeout)
	defer cancel()

	responses := make([]string, cfg.count)
	responseTimes := make([]time.Duration, cfg.count)
	var responsesMu sync.Mutex

	var throttle *time.Ticker
	qps := int(cfg.Request.Qps)
	if qps > 0 {
		sleepTime := time.Second / time.Duration(qps)
		fwLog.Debugf("Sleeping %v between requests", sleepTime)
		throttle = time.NewTicker(sleepTime)
	}

	g := e.NewGroup()
	for index := 0; index < cfg.count; index++ {
		index := index
		if throttle != nil {
			<-throttle.C
		}

		g.Go(ctx, func() error {
			st := time.Now()
			resp, err := doReq(ctx, cfg, index)
			if err != nil {
				return err
			}

			responsesMu.Lock()
			responses[index] = resp
			responseTimes[index] = time.Since(st)
			responsesMu.Unlock()
			return nil
		})
	}

	// Convert the result of the wait into a channel.
	requestsDone := make(chan *multierror.Error)
	go func() {
		requestsDone <- g.Wait()
	}()

	select {
	case merr := <-requestsDone:
		if err := merr.ErrorOrNil(); err != nil {
			return nil, fmt.Errorf("%d/%d requests had errors; first error: %v", merr.Len(), cfg.count, merr.Errors[0])
		}

		return &proto.ForwardEchoResponse{
			Output: responses,
		}, nil
	case <-ctx.Done():
		responsesMu.Lock()
		defer responsesMu.Unlock()

		var c int
		var tt time.Duration
		for id, res := range responses {
			if res != "" && responseTimes[id] != 0 {
				c++
				tt += responseTimes[id]
			}
		}
		var avgTime time.Duration
		if c > 0 {
			avgTime = tt / time.Duration(c)
		}
		return nil, fmt.Errorf("request set timed out after %v and only %d/%d requests completed (%v avg)",
			cfg.timeout, c, cfg.count, avgTime)
	}
}
