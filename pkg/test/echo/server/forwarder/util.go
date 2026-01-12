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
	"net/url"
	"sync"
	"time"

	"github.com/hashicorp/go-multierror"
	"golang.org/x/net/proxy"

	"istio.io/istio/pkg/hbone"
	"istio.io/istio/pkg/log"
	"istio.io/istio/pkg/test/echo"
	"istio.io/istio/pkg/test/echo/common"
	"istio.io/istio/pkg/test/echo/proto"
)

const (
	hostHeader = "Host"
)

var fwLog = log.RegisterScope("forwarder", "echo clientside")

func writeForwardedHeaders(out *bytes.Buffer, requestID int, header http.Header) {
	for key, values := range header {
		for _, v := range values {
			echo.ForwarderHeaderField.WriteKeyValueForRequest(out, requestID, key, v)
		}
	}
}

type SpecificVersionDialer struct {
	network string
	inner   hbone.Dialer
}

func (s SpecificVersionDialer) Dial(network, addr string) (c net.Conn, err error) {
	return s.DialContext(context.Background(), network, addr)
}

func (s SpecificVersionDialer) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	return s.inner.DialContext(ctx, s.network, address)
}

var _ hbone.Dialer = SpecificVersionDialer{}

func newDialer(cfg *Config) hbone.Dialer {
	// Double HBONE takes higher precedence than single HBONE
	if cfg.Request.DoubleHbone.GetAddress() != "" {
		return hbone.NewDoubleDialer(hbone.Config{
			ProxyAddress: cfg.Request.DoubleHbone.GetAddress(),
			Headers:      cfg.hboneHeaders,
			TLS:          cfg.hboneTLSConfig,
		},
			hbone.Config{
				ProxyAddress: cfg.Request.Hbone.GetAddress(),
				Headers:      cfg.hboneHeaders,
				TLS:          cfg.innerHboneTLSConfig,
			}, cfg.innerHboneTLSConfig)
	}
	if cfg.Request.Hbone.GetAddress() != "" {
		out := hbone.NewDialer(hbone.Config{
			ProxyAddress: cfg.Request.Hbone.GetAddress(),
			Headers:      cfg.hboneHeaders,
			TLS:          cfg.hboneTLSConfig,
		})
		return out
	}
	proxyURL, _ := url.Parse(cfg.Proxy)
	if len(cfg.Proxy) > 0 && proxyURL.Scheme == "socks5" {
		dialer, _ := proxy.SOCKS5("tcp", proxyURL.Host, nil, proxy.Direct)
		return dialer.(hbone.Dialer)
	}
	out := &net.Dialer{
		Timeout: common.ConnectionTimeout,
	}

	if cfg.forceDNSLookup {
		out.Resolver = newResolver(common.ConnectionTimeout, "", "")
	}
	if ipf := cfg.Request.ForceIpFamily; ipf != "" {
		return proxy.FromEnvironmentUsing(SpecificVersionDialer{
			network: ipf,
			inner:   out,
		}).(hbone.Dialer)
	}
	return proxy.FromEnvironmentUsing(out).(hbone.Dialer)
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
		defer throttle.Stop()
	}

	g := e.NewGroup()
	for index := 0; index < cfg.count; index++ {
		workFn := func() error {
			st := time.Now()
			resp, err := doReq(ctx, cfg, index)
			if err != nil {
				fwLog.Debugf("request failed: %v", err)
				return err
			}
			fwLog.Debugf("got resp: %v", resp)

			responsesMu.Lock()
			responses[index] = resp
			responseTimes[index] = time.Since(st)
			responsesMu.Unlock()
			return nil
		}
		if throttle != nil {
			select {
			case <-ctx.Done():
				break
			case <-throttle.C:
			}
		}

		if cfg.PropagateResponse != nil {
			workFn() // nolint: errcheck
		} else {
			g.Go(ctx, workFn)
		}
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
