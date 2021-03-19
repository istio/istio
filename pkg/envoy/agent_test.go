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
	"testing"
	"time"
)

// TestProxy sample struct for proxy
type TestProxy struct {
	run          func(int, <-chan error) error
	cleanup      func(int)
	blockChannel chan interface{}
}

func (tp TestProxy) Run(epoch int, stop <-chan error) error {
	if tp.run == nil {
		return nil
	}
	return tp.run(epoch, stop)
}

func (tp TestProxy) Drain() error {
	tp.blockChannel <- "unblock"
	return nil
}

func (tp TestProxy) Cleanup(epoch int) {
	if tp.cleanup != nil {
		tp.cleanup(epoch)
	}
}

// TestStartExit starts a proxy and ensures the agent exits once the proxy exits
func TestStartExit(t *testing.T) {
	ctx := context.Background()
	done := make(chan struct{})
	a := NewAgent(TestProxy{}, 0)
	go func() {
		_ = a.Run(ctx)
		done <- struct{}{}
	}()
	<-done
}

// TestStartDrain tests basic start, termination sequence
//   * Runs with passed config
//   * Terminate is called
//   * Runs with drain config
//   * Aborts all proxies
func TestStartDrain(t *testing.T) {
	wantEpoch := 0
	proxiesStarted, wantProxiesStarted := 0, 1
	blockChan := make(chan interface{})
	ctx, cancel := context.WithCancel(context.Background())
	start := func(currentEpoch int, _ <-chan error) error {
		proxiesStarted++
		if currentEpoch != wantEpoch {
			t.Errorf("start wanted epoch %v, got %v", wantEpoch, currentEpoch)
		}
		wantEpoch = currentEpoch + 1
		blockChan <- "unblock"
		if currentEpoch == 0 {
			<-ctx.Done()
			time.Sleep(time.Second * 2) // ensure initial proxy doesn't terminate too quickly
		}
		return nil
	}
	a := NewAgent(TestProxy{run: start, blockChannel: blockChan}, -10*time.Second)
	go func() { _ = a.Run(ctx) }()
	<-blockChan
	cancel()
	<-blockChan
	<-ctx.Done()

	if proxiesStarted != wantProxiesStarted {
		t.Errorf("expected %v proxies to be started, got %v", wantProxiesStarted, proxiesStarted)
	}
}

// TestStartTwiceStop applies three configs and validates that cleanups are called in order
func TestStartStop(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	start := func(epoch int, _ <-chan error) error {
		return nil
	}
	cleanup := func(epoch int) {
		if epoch == 0 {
			cancel()
		}
	}
	a := NewAgent(TestProxy{run: start, cleanup: cleanup}, 0)
	go func() { _ = a.Run(ctx) }()
	<-ctx.Done()
}

// TestRecovery tests that recovery is applied once
func TestRecovery(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	failed := false
	start := func(epoch int, _ <-chan error) error {
		if epoch == 0 && !failed {
			failed = true
			return nil
		}
		if epoch > 0 {
			t.Errorf("Should not reconcile after success")
		}
		<-ctx.Done()
		return nil
	}
	a := NewAgent(TestProxy{run: start}, 0)
	go func() { _ = a.Run(ctx) }()

	// make sure we don't try to reconcile twice
	<-time.After(100 * time.Millisecond)
	cancel()
}
