// Copyright 2017 Istio Authors
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

package proxy

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"
)

var (
	testRetry = Retry{
		InitialInterval: time.Millisecond,
		MaxRetries:      10,
	}
)

// TestProxy sample struct for proxy
type TestProxy struct {
	run     func(interface{}, int, <-chan error) error
	cleanup func(int)
	panic   func(interface{})
}

func (tp TestProxy) Run(config interface{}, epoch int, stop <-chan error) error {
	return tp.run(config, epoch, stop)
}

func (tp TestProxy) Cleanup(epoch int) {
	if tp.cleanup != nil {
		tp.cleanup(epoch)
	}
}

func (tp TestProxy) Panic(config interface{}) {
	if tp.panic != nil {
		tp.panic(config)
	}
}

// TestStartDrain tests basic start, termination sequence
//   * Runs with passed config
//   * Terminate is called
//   * Runs with drain config
//   * Aborts all proxies
func TestStartDrain(t *testing.T) {
	wantEpoch := 0
	proxiesStarted, wantProxiesStarted := 0, 2
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
		} else if currentEpoch == 1 {
			if _, ok := config.(DrainConfig); !ok {
				t.Errorf("start expected draining config, got %q", config)
			}
		}
		return nil
	}
	a := NewAgent(TestProxy{start, nil, nil}, testRetry, -10*time.Second)
	go a.Run(ctx)
	a.ConfigCh() <- startConfig
	<-blockChan
	cancel()
	<-blockChan
	<-ctx.Done()

	if proxiesStarted != wantProxiesStarted {
		t.Errorf("expected %v proxies to be started, got %v", wantProxiesStarted, proxiesStarted)
	}
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
	cleanup := func(epoch int) {}
	a := NewAgent(TestProxy{start, cleanup, nil}, testRetry, -10*time.Second)
	go a.Run(ctx)
	a.ConfigCh() <- desired
	applyCount++
	a.ConfigCh() <- desired
	applyCount++
	cancel()
}

// TestApplyThrice applies same config twice but with a bad config between them
func TestApplyThrice(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	good := "good"
	bad := "bad"
	applied := false
	var a Agent
	start := func(config interface{}, epoch int, _ <-chan error) error {
		if config == bad {
			return nil
		}
		if config == good && applied {
			t.Errorf("Config has already been applied")
		}
		applied = true
		<-ctx.Done()
		return nil
	}
	cleanup := func(epoch int) {
		// we should expect to see three epochs only: 0 for good, 1 for bad
		if epoch == 1 {
			go func() {
				a.ConfigCh() <- good
				cancel()
			}()
		} else if epoch != 0 {
			t.Errorf("Unexpected epoch %d", epoch)
		}
	}
	retry := testRetry
	retry.MaxRetries = 0
	a = NewAgent(TestProxy{start, cleanup, nil}, retry, -10*time.Second)
	go a.Run(ctx)
	a.ConfigCh() <- good
	a.ConfigCh() <- bad
	<-ctx.Done()
}

// TestAbort checks that successfully started proxies are aborted on error in the child
func TestAbort(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	good1 := "good1"
	aborted1 := false
	good2 := "good2"
	aborted2 := false
	bad := "bad"
	active := 3
	start := func(config interface{}, epoch int, abort <-chan error) error {
		if config == bad {
			return errors.New(bad)
		}
		select {
		case err := <-abort:
			if config == good1 {
				aborted1 = true
			} else if config == good2 {
				aborted2 = true
			}
			return err
		case <-ctx.Done():
		}
		return nil
	}
	cleanup := func(epoch int) {
		// first 2 with an error, then 0 and 1 with abort
		active--
		if active == 0 {
			if !aborted1 {
				t.Error("Expected first epoch to be aborted")
			}
			if !aborted2 {
				t.Error("Expected second epoch to be aborted")
			}
			cancel()
		}
	}
	retry := testRetry
	retry.InitialInterval = 10 * time.Second
	a := NewAgent(TestProxy{start, cleanup, nil}, retry, 0)
	go a.Run(ctx)
	a.ConfigCh() <- good1
	a.ConfigCh() <- good2
	a.ConfigCh() <- bad
	<-ctx.Done()
}

// TestStartFail injects an error in 2 tries to start the proxy
func TestStartFail(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	retry := 0
	start := func(config interface{}, epoch int, _ <-chan error) error {
		if epoch == 0 && retry == 0 {
			retry++
			return fmt.Errorf("error on try %d", retry)
		} else if epoch == 0 && retry == 1 {
			retry++
			return fmt.Errorf("error on try %d", retry)
		} else if epoch == 0 && retry == 2 {
			retry++
			cancel()
		} else if _, ok := config.(DrainConfig); !ok { // don't need to validate draining proxy here
			t.Errorf("Unexpected epoch %d and retry %d", epoch, retry)
			cancel()
		}
		return nil
	}
	cleanup := func(epoch int) {}
	a := NewAgent(TestProxy{start, cleanup, nil}, testRetry, 0)
	go a.Run(ctx)
	a.ConfigCh() <- "test"
	<-ctx.Done()
}

// TestExceedBudget drives to permanent failure
func TestExceedBudget(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	retry := 0
	start := func(config interface{}, epoch int, _ <-chan error) error {
		if epoch == 0 && retry == 0 {
			retry++
			return fmt.Errorf("error on try %d", retry)
		} else if epoch == 0 && retry == 1 {
			retry++
			return fmt.Errorf("error on try %d", retry)
		} else {
			t.Errorf("Unexpected epoch %d and retry %d", epoch, retry)
			cancel()
		}
		return nil
	}
	cleanup := func(epoch int) {
		if epoch == 0 && (retry == 0 || retry == 1 || retry == 2) {
		} else {
			t.Errorf("Unexpected epoch %d and retry %d", epoch, retry)
			cancel()
		}
	}
	retryDelay := testRetry
	retryDelay.MaxRetries = 1
	a := NewAgent(TestProxy{start, cleanup, func(_ interface{}) { cancel() }}, retryDelay, 0)
	go a.Run(ctx)
	a.ConfigCh() <- "test"
	<-ctx.Done()
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
		} else if _, ok := config.(DrainConfig); !ok { // don't need to validate draining proxy here
			t.Errorf("Unexpected start %v, epoch %d", config, epoch)
			cancel()
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
			t.Errorf("Unexpected epoch %d in cleanup", epoch)
			cancel()
		}
	}
	a := NewAgent(TestProxy{start, cleanup, nil}, testRetry, 0)
	go a.Run(ctx)
	a.ConfigCh() <- desired0
	a.ConfigCh() <- desired1
	a.ConfigCh() <- desired2
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
	a := NewAgent(TestProxy{start, func(_ int) {}, nil}, testRetry, 0)
	go a.Run(ctx)
	a.ConfigCh() <- desired

	// make sure we don't try to reconcile twice
	<-time.After(100 * time.Millisecond)
	cancel()
}

// TestCascadingAbort plays a scenario that may trigger self-abort deadlock:
//  * start three epochs 0, 1, 2
//  * epoch 2 crashes, triggers abort of 0, 1
//  * epoch 1 crashes before receiving abort, triggers abort of 0
//  * epoch 0 aborts, recovery to config 2 follows
func TestCascadingAbort(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	crash1 := make(chan struct{})
	start := func(config interface{}, epoch int, abort <-chan error) error {
		if config == 2 {
			close(crash1)
			return errors.New("planned crash for 2")
		} else if config == 1 {
			<-crash1
			// ignore abort, crash by itself
			<-time.After(100 * time.Millisecond)
			return errors.New("planned crash for 1")
		} else if config == 0 {
			// abort, a bit later
			<-time.After(300 * time.Millisecond)
			err := <-abort
			cancel()
			return err
		}
		return nil
	}
	retry := testRetry
	retry.InitialInterval = 1 * time.Second
	a := NewAgent(TestProxy{start, func(_ int) {}, nil}, retry, 0)
	go a.Run(ctx)
	a.ConfigCh() <- 0
	a.ConfigCh() <- 1
	a.ConfigCh() <- 2
	<-ctx.Done()
}

// TestLockup plays a scenario that may cause a deadlock
//  * start epoch 0
//  * start epoch 1 (wait till sets desired and current config)
//  * epoch 0 crashes, triggers abort
//  * epoch 1 aborts
//  * progress should be made
func TestLockup(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	crash := make(chan struct{})

	try := 0
	lock := sync.Mutex{}

	start := func(config interface{}, epoch int, abort <-chan error) error {

		lock.Lock()
		if try >= 2 {
			cancel()
			lock.Unlock()
			return nil
		}
		try++
		lock.Unlock()
		switch epoch {
		case 0:
			<-crash

			return errors.New("crash")
		case 1:
			close(crash)
			err := <-abort
			return err
		}
		return nil
	}
	a := NewAgent(TestProxy{start, func(_ int) {}, nil}, testRetry, 0)
	go a.Run(ctx)
	a.ConfigCh() <- 0
	a.ConfigCh() <- 1
	select {
	case <-ctx.Done():
	case <-time.After(1 * time.Second):
		t.Error("liveness check failed")
	}
}
