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
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	. "github.com/onsi/gomega"
)

// TestProxy sample struct for proxy
type TestProxy struct {
	run          func(interface{}, int, <-chan error) error
	cleanup      func(int)
	live         func() bool
	blockChannel chan interface{}
}

func (tp TestProxy) Run(config interface{}, epoch int, stop <-chan error) error {
	if tp.run == nil {
		return nil
	}
	return tp.run(config, epoch, stop)
}

func (tp TestProxy) IsLive() bool {
	if tp.live == nil {
		return true
	}
	return tp.live()
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
	a.Restart("config")
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
	startConfig := "start config"
	start := func(config interface{}, currentEpoch int, _ <-chan error) error {
		t.Logf("Start called with config: %v", config)
		proxiesStarted++
		if currentEpoch != wantEpoch {
			t.Errorf("start wanted epoch %v, got %v", wantEpoch, currentEpoch)
		}
		wantEpoch = currentEpoch + 1
		blockChan <- "unblock"
		if currentEpoch == 0 {
			<-ctx.Done()
			if config != startConfig {
				t.Errorf("start wanted config %q, got %q", startConfig, config)
			}
			time.Sleep(time.Second * 2) // ensure initial proxy doesn't terminate too quickly
		}
		return nil
	}
	a := NewAgent(TestProxy{run: start, blockChannel: blockChan}, -10*time.Second)
	go func() { _ = a.Run(ctx) }()
	a.Restart(startConfig)
	<-blockChan
	cancel()
	<-blockChan
	<-ctx.Done()

	if proxiesStarted != wantProxiesStarted {
		t.Errorf("expected %v proxies to be started, got %v", wantProxiesStarted, proxiesStarted)
	}
}

// TestWaitForLive tests that a hot restart will not occur until after a previous epoch goes live.
func TestWaitForLive(t *testing.T) {
	g := NewGomegaWithT(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// The delay for the first epoch (i.e. epoch 0) to go "live"
	delay := time.Second * 2
	live := uint32(0)
	var expectedEpoch1StartTime time.Time
	var epoch1StartTime time.Time

	wg := sync.WaitGroup{}
	wg.Add(2)

	start := func(config interface{}, epoch int, _ <-chan error) error {
		switch epoch {
		case 0:
			// Epoch 0 has started, get the expected time that epoch 1 should start including the "live" delay.
			expectedEpoch1StartTime = time.Now().Add(delay)
			wg.Done()

			// Add a short delay before reporting live.
			time.Sleep(delay)
			atomic.StoreUint32(&live, 1)
		case 1:
			// Epoch 1 has started, record the time.
			epoch1StartTime = time.Now()
			wg.Done()
		}
		<-ctx.Done()
		return nil
	}
	isLive := func() bool {
		return atomic.LoadUint32(&live) > 0
	}
	a := NewAgent(TestProxy{run: start, live: isLive}, -10*time.Second)
	go func() { _ = a.Run(ctx) }()

	// Start the first epoch.
	a.Restart("config1")

	// Start the second epoch, should wait until the first goes live.
	a.Restart("config2")

	// Wait for both to start.
	wg.Wait()

	// An error threshold used for the time comparison. Should (hopefully) be enough to avoid flakes.
	errThreshold := 1 * time.Second

	// Verify that the second epoch is delayed until epoch 0 was live.
	g.Expect(epoch1StartTime).Should(BeTemporally("~", expectedEpoch1StartTime, errThreshold))
}

func TestExitDuringWaitForLive(t *testing.T) {
	g := NewGomegaWithT(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	epoch0Exit := make(chan error)
	epoch1Started := make(chan struct{}, 1)
	start := func(config interface{}, epoch int, _ <-chan error) error {
		switch epoch {
		case 0:
			// The first epoch just waits for the exit error.
			return <-epoch0Exit
		case 1:
			// Indicate that the second epoch was started.
			close(epoch1Started)
		}
		<-ctx.Done()
		return nil
	}
	neverLive := func() bool {
		// Never go live.
		return false
	}
	a := NewAgent(TestProxy{run: start, live: neverLive}, -10*time.Second)
	go func() { _ = a.Run(ctx) }()

	// Start the first epoch.
	a.Restart("config1")

	// Immediately start the second epoch. This will block until the first one exits, since it will never go live.
	go a.Restart("config2")

	// Trigger the first epoch to exit
	var epoch1StartTime time.Time
	expectedEpoch1StartTime := time.Now()
	epoch0Exit <- errors.New("fake")

	select {
	case <-epoch1Started:
		// Started
		epoch1StartTime = time.Now()
		break
	case <-time.After(5 * time.Second):
		t.Fatalf("timed out waiting for epoch 1 to start")
	}

	// An error threshold used for the time comparison. Should (hopefully) be enough to avoid flakes.
	errThreshold := 1 * time.Second

	// Verify that the second epoch is delayed until epoch 0 was live.
	g.Expect(epoch1StartTime).Should(BeTemporally("~", expectedEpoch1StartTime, errThreshold))
}

// TestApplyTwice tests that scheduling the same config does not trigger a restart
func TestApplyTwice(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	desired := "config"
	applyCount := 0
	start := func(config interface{}, epoch int, _ <-chan error) error {
		if epoch == 1 && applyCount < 2 {
			t.Error("Should start only once for same config")
		}
		<-ctx.Done()
		return nil
	}
	a := NewAgent(TestProxy{run: start}, -10*time.Second)
	go func() { _ = a.Run(ctx) }()
	a.Restart(desired)
	applyCount++
	a.Restart(desired)
	applyCount++
	cancel()
}

// TestStartTwiceStop applies three configs and validates that cleanups are called in order
func TestStartTwiceStop(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	stop0 := make(chan struct{})
	stop1 := make(chan struct{})
	desired0 := "config0"
	desired1 := "config1"
	desired2 := "config2"
	start := func(config interface{}, epoch int, _ <-chan error) error {
		if config == desired0 && epoch == 0 {
			<-stop0
		} else if config == desired1 && epoch == 1 {
			<-stop1
		} else if config == desired2 && epoch == 2 {
			close(stop1)
			<-ctx.Done()
		}
		return nil
	}
	finished0, finished1, finished2 := false, false, false
	cleanup := func(epoch int) {
		// epoch 1 finishes before epoch 0
		if epoch == 1 {
			finished1 = true
			if finished0 || finished2 {
				t.Errorf("Expected epoch 1 to be first to finish")
			}
			close(stop0)
		} else if epoch == 0 {
			finished0 = true
			if !finished1 || finished2 {
				t.Errorf("Expected epoch 0 to be second to finish")
			}
			cancel()
		} else if epoch == 2 {
			finished2 = true
			if !finished0 || !finished1 {
				t.Errorf("Expected epoch 2 to be last to finish")
			}
		} else {
			// epoch 3 is the drain epoch
			if epoch != 3 {
				t.Errorf("Unexpected epoch %d in cleanup", epoch)
			}
			cancel()
		}
	}
	a := NewAgent(TestProxy{run: start, cleanup: cleanup}, 0)
	go func() { _ = a.Run(ctx) }()
	a.Restart(desired0)
	a.Restart(desired1)
	a.Restart(desired2)
	<-ctx.Done()
}

// TestRecovery tests that recovery is applied once
func TestRecovery(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	desired := "config"
	failed := false
	start := func(config interface{}, epoch int, _ <-chan error) error {
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
	a.Restart(desired)

	// make sure we don't try to reconcile twice
	<-time.After(100 * time.Millisecond)
	cancel()
}
