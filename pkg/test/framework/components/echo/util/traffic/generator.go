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
	"errors"
	"fmt"
	"time"

	"istio.io/istio/pkg/test/framework/components/echo"
)

const (
	defaultInterval = 1 * time.Second
)

// Config for a traffic Generator.
type Config struct {
	// Source of the traffic.
	Source echo.Instance

	// Options for generating traffic from the Source to the target.
	Options echo.CallOptions

	// Interval between successive call operations. If not set, defaults to 1 second.
	Interval time.Duration
}

// Generator of traffic between echo instances. Every time interval
// (as defined by Config.Interval), a grpc request is sent to the source pod,
// causing it to send a request to the destination echo server. Results are
// captured for each request for later processing.
type Generator interface {
	// Run the Generator and start sending traffic.
	Run(stop chan struct{})

	// WaitForResult waits for a short while for in-flight requests to complete.
	// It is expected that the stop channel will be closed in order for this function
	// to return successfully. Returns the Result, or an error if the wait timed out.
	WaitForResult(timeout time.Duration) (Result, error)
}

// NewGenerator returns a new Generator with the given configuration.
func NewGenerator(cfg Config) Generator {
	fillInDefaults(&cfg)
	return &generator{
		Config:  cfg,
		stopped: make(chan struct{}),
	}
}

var _ Generator = &generator{}

type generator struct {
	Config
	result  Result
	stopped chan struct{}
}

func (g *generator) Run(stop chan struct{}) {
	for {
		t := time.NewTimer(g.Interval)
		select {
		case <-stop:
			t.Stop()
			close(g.stopped)
			return
		case <-t.C:
			g.result.add(g.Source.Call(g.Options))
			t.Reset(g.Interval)
		}
	}
}

func (g *generator) WaitForResult(timeout time.Duration) (Result, error) {
	t := time.NewTimer(timeout)
	select {
	case <-g.stopped:
		t.Stop()
		if g.result.TotalRequests == 0 {
			return Result{}, errors.New("no requests completed before stopping the traffic generator")
		}
		return g.result, nil
	case <-t.C:
		return Result{}, fmt.Errorf("timed out waiting for result")
	}
}

func fillInDefaults(cfg *Config) {
	if cfg.Interval == 0 {
		cfg.Interval = defaultInterval
	}

	if cfg.Options.Validator == nil {
		cfg.Options.Validator = echo.ExpectOK()
	}
}
