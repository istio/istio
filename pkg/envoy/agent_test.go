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

package envoy

import (
	"context"
	"net"
	"testing"

	"istio.io/istio/pilot/cmd/pilot-agent/status/testserver"
)

var invalidStats = ""

var downstreamCxPostiveAcStats = "http.admin.downstream_cx_active: 2 \n" +
	"http.agent.downstream_cx_active: 0 \n" +
	"http.inbound_0.0.0.0_8080.downstream_cx_active: 0 \n" +
	"listener.0.0.0.0_15001.downstream_cx_active: 0 \n" +
	"listener.0.0.0.0_15006.downstream_cx_active: 0 \n" +
	"listener.0.0.0.0_15021.downstream_cx_active: 0 \n" +
	"listener.0.0.0.0_8443.downstream_cx_active: 6 \n" +
	"listener.0.0.0.0_9093.downstream_cx_active: 8 \n" +
	"listener.10.112.32.70_9043.downstream_cx_active: 1 \n" +
	"listener.10.112.33.230_2181.downstream_cx_active: 0 \n" +
	"listener.10.112.40.186_2181.downstream_cx_active: 1 \n" +
	"listener.10.112.43.239_9043.downstream_cx_active: 2 \n" +
	"listener.10.112.49.68_9043.downstream_cx_active: 1 \n" +
	"listener.admin.downstream_cx_active: 2 \n" +
	"listener.admin.main_thread.downstream_cx_active: 2"

var downstreamCxZeroAcStats = "http.admin.downstream_cx_active: 2 \n" +
	"http.agent.downstream_cx_active: 0 \n" +
	"http.inbound_0.0.0.0_8080.downstream_cx_active: 0 \n" +
	"listener.0.0.0.0_15001.downstream_cx_active: 0 \n" +
	"listener.0.0.0.0_15006.downstream_cx_active: 0 \n" +
	"listener.0.0.0.0_15021.downstream_cx_active: 1 \n" +
	"listener.0.0.0.0_8443.downstream_cx_active: 0 \n" +
	"listener.0.0.0.0_9093.downstream_cx_active: 0 \n" +
	"listener.10.112.32.70_9043.downstream_cx_active: 0 \n" +
	"listener.10.112.33.230_2181.downstream_cx_active: 0 \n" +
	"listener.10.112.40.186_2181.downstream_cx_active: 0 \n" +
	"listener.10.112.43.239_9043.downstream_cx_active: 0 \n" +
	"listener.10.112.49.68_9043.downstream_cx_active: 0\n" +
	"listener.admin.downstream_cx_active: 2 \n" +
	"listener.admin.main_thread.downstream_cx_active: 2"

// TestProxy sample struct for proxy
type TestProxy struct {
	run          func(<-chan error) error
	cleanup      func()
	blockChannel chan any
}

func (tp TestProxy) Run(stop <-chan error) error {
	if tp.run == nil {
		return nil
	}
	return tp.run(stop)
}

func (tp TestProxy) Drain() error {
	tp.blockChannel <- "unblock"
	return nil
}

func (tp TestProxy) Cleanup() {
	if tp.cleanup != nil {
		tp.cleanup()
	}
}

func (tp TestProxy) UpdateConfig(_ []byte) error {
	return nil
}

// TestStartExit starts a proxy and ensures the agent exits once the proxy exits
func TestStartExit(t *testing.T) {
	ctx := context.Background()
	done := make(chan struct{})
	a := NewAgent(TestProxy{}, 0, 0, "", 0, 0, 0, true)
	go func() {
		a.Run(ctx)
		done <- struct{}{}
	}()
	<-done
}

// TestStartTwiceStop applies three configs and validates that cleanups are called in order
func TestStartStop(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	start := func(_ <-chan error) error {
		return nil
	}
	cleanup := func() {
		cancel()
	}
	a := NewAgent(TestProxy{run: start, cleanup: cleanup}, 0, 0, "", 0, 0, 0, true)
	go func() { a.Run(ctx) }()
	<-ctx.Done()
}

func TestActiveConnections(t *testing.T) {
	cases := []struct {
		name     string
		stats    string
		expected int
	}{
		{
			"invalid stats",
			invalidStats,
			-1,
		},
		{
			"valid active connections",
			downstreamCxPostiveAcStats,
			19,
		},
		{
			"zero active connections",
			downstreamCxZeroAcStats,
			0,
		},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			server := testserver.CreateAndStartServer(tt.stats)
			defer server.Close()

			agent := NewAgent(TestProxy{}, 0, 0, "localhost", server.Listener.Addr().(*net.TCPAddr).Port, 15021, 15009, true)
			if ac, _ := agent.activeProxyConnections(); ac != tt.expected {
				t.Errorf("unexpected active proxy connections. expected: %d got: %d", tt.expected, ac)
			}
		})
	}
}
