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
	"context"
	"io"
	"net/http"
	"time"

	"github.com/golang/sync/errgroup"

	"istio.io/istio/pkg/test/echo/common"
	"istio.io/istio/pkg/test/echo/proto"
	"istio.io/pkg/log"
)

var _ io.Closer = &Instance{}

// Config for a forwarder Instance.
type Config struct {
	Request *proto.ForwardEchoRequest
	UDS     string
	TLSCert string
	Dialer  common.Dialer
}

func (c Config) fillInDefaults() Config {
	c.Dialer = c.Dialer.FillInDefaults()
	common.FillInDefaults(c.Request)
	return c
}

// Instance processes a single proto.ForwardEchoRequest, sending individual echo requests to the destination URL.
type Instance struct {
	p       protocol
	url     string
	timeout time.Duration
	count   int
	qps     int
	header  http.Header
	message string
}

// New creates a new forwarder Instance.
func New(cfg Config) (*Instance, error) {
	cfg = cfg.fillInDefaults()

	p, err := newProtocol(cfg)
	if err != nil {
		return nil, err
	}

	return &Instance{
		p:       p,
		url:     cfg.Request.Url,
		timeout: common.GetTimeout(cfg.Request),
		count:   common.GetCount(cfg.Request),
		qps:     int(cfg.Request.Qps),
		header:  common.GetHeaders(cfg.Request),
		message: cfg.Request.Message,
	}, nil
}

// Run the forwarder and collect the responses.
func (i *Instance) Run(ctx context.Context) (*proto.ForwardEchoResponse, error) {
	g, _ := errgroup.WithContext(context.Background())
	responses := make([]string, i.count)

	var throttle *time.Ticker

	if i.qps > 0 {
		sleepTime := time.Second / time.Duration(i.qps)
		log.Debugf("Sleeping %v between requests", sleepTime)
		throttle = time.NewTicker(sleepTime)
	}

	for reqIndex := 0; reqIndex < i.count; reqIndex++ {
		r := request{
			RequestID: reqIndex,
			URL:       i.url,
			Message:   i.message,
			Header:    i.header,
			Timeout:   i.timeout,
		}

		if throttle != nil {
			<-throttle.C
		}

		// TODO(nmittler): Refactor this to limit the number of go routines.
		g.Go(func() error {
			resp, err := i.p.makeRequest(ctx, &r)
			if err != nil {
				return err
			}
			responses[r.RequestID] = resp
			return nil
		})
	}

	if err := g.Wait(); err != nil {
		return nil, err
	}

	return &proto.ForwardEchoResponse{
		Output: responses,
	}, nil
}

func (i *Instance) Close() error {
	if i != nil && i.p != nil {
		return i.p.Close()
	}
	return nil
}
