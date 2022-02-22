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

package traffic

import (
	"time"

	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/echo/check"
	"istio.io/istio/pkg/test/framework/components/echo"
)

const (
	defaultInterval = 1 * time.Second
	defaultTimeout  = 15 * time.Second
)

// Config for a traffic Generator.
type Config struct {
	// Source of the traffic.
	Source echo.Caller

	// Options for generating traffic from the Source to the target.
	Options echo.CallOptions

	// Interval between successive call operations. If not set, defaults to 1 second.
	Interval time.Duration

	// Maximum time to wait for traffic to complete after stopping. If not set, defaults to 15 seconds.
	StopTimeout time.Duration
}

// Generator of traffic between echo instances. Every time interval
// (as defined by Config.Interval), a grpc request is sent to the source pod,
// causing it to send a request to the destination echo server. Results are
// captured for each request for later processing.
type Generator interface {
	// Start sending traffic.
	Start() Generator

	// Stop sending traffic and wait for any in-flight requests to complete.
	// Returns the Result
	Stop() Result
}

// NewGenerator returns a new Generator with the given configuration.
func NewGenerator(t test.Failer, cfg Config) Generator {
	fillInDefaults(&cfg)
	return &generator{
		Config:  cfg,
		t:       t,
		stop:    make(chan struct{}),
		stopped: make(chan struct{}),
	}
}

var _ Generator = &generator{}

type generator struct {
	Config
	t       test.Failer
	result  Result
	stop    chan struct{}
	stopped chan struct{}
}

func (g *generator) Start() Generator {
	go func() {
		t := time.NewTimer(g.Interval)
		for {
			select {
			case <-g.stop:
				t.Stop()
				close(g.stopped)
				return
			case <-t.C:
				g.result.add(g.Source.Call(g.Options))
				t.Reset(g.Interval)
			}
		}
	}()
	return g
}

func (g *generator) Stop() Result {
	// Trigger the generator to stop.
	close(g.stop)

	// Wait for the generator to exit.
	t := time.NewTimer(g.StopTimeout)
	select {
	case <-g.stopped:
		t.Stop()
		if g.result.TotalRequests == 0 {
			g.t.Fatal("no requests completed before stopping the traffic generator")
		}
		return g.result
	case <-t.C:
		g.t.Fatal("timed out waiting for result")
	}
	// Can never happen, but the compiler doesn't know that Fatal terminates
	return Result{}
}

func fillInDefaults(cfg *Config) {
	if cfg.Interval == 0 {
		cfg.Interval = defaultInterval
	}
	if cfg.StopTimeout == 0 {
		cfg.StopTimeout = defaultTimeout
	}
	if cfg.Options.Check == nil {
		cfg.Options.Check = check.OK()
	}
}
