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

// TestStartStop tests basic start, cleanup sequence
func TestStartStop(t *testing.T) {
	current := -1
	ctx, cancel := context.WithCancel(context.Background())
	desired := "config"
	start := func(config interface{}, epoch int, _ <-chan error) error {
		if current != -1 {
			t.Error("Expected epoch not to be set")
		}
		if epoch != 0 {
			t.Error("Expected initial epoch to be 0")
		}
		if config != desired {
			t.Errorf("Start got config %v, want %v", config, desired)
		}
		current = epoch
		return nil
	}
	cleanup := func(epoch int) {
		if current != 0 {
			t.Error("Expected epoch to be set")
		}
		if epoch != 0 {
			t.Error("Expected initial epoch in cleanup to be 0")
		}
		cancel()
	}
	a := NewAgent(TestProxy{start, cleanup, nil}, testRetry)
	go a.Run(ctx)
	a.ScheduleConfigUpdate(desired)
	<-ctx.Done()
}

// TestApplyTwice tests that scheduling the same config does not trigger a restart
func TestApplyTwice(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	desired := "config"
	start := func(config interface{}, epoch int, _ <-chan error) error {
		if epoch == 1 {
			t.Error("Should start only once for same config")
		}
		<-ctx.Done()
		return nil
	}
	cleanup := func(epoch int) {}
	a := NewAgent(TestProxy{start, cleanup, nil}, testRetry)
	go a.Run(ctx)
	a.ScheduleConfigUpdate(desired)
	a.ScheduleConfigUpdate(desired)
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
				a.ScheduleConfigUpdate(good)
				cancel()
			}()
		} else if epoch != 0 {
			t.Errorf("Unexpected epoch %d", epoch)
		}
	}
	retry := testRetry
	retry.MaxRetries = 0
	a = NewAgent(TestProxy{start, cleanup, nil}, retry)
	go a.Run(ctx)
	a.ScheduleConfigUpdate(good)
	a.ScheduleConfigUpdate(bad)
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
		active = active - 1
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
	a := NewAgent(TestProxy{start, cleanup, nil}, retry)
	go a.Run(ctx)
	a.ScheduleConfigUpdate(good1)
	a.ScheduleConfigUpdate(good2)
	a.ScheduleConfigUpdate(bad)
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
		} else {
			t.Errorf("Unexpected epoch %d and retry %d", epoch, retry)
			cancel()
		}
		return nil
	}
	cleanup := func(epoch int) {}
	a := NewAgent(TestProxy{start, cleanup, nil}, testRetry)
	go a.Run(ctx)
	a.ScheduleConfigUpdate("test")
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
	a := NewAgent(TestProxy{start, cleanup, func(_ interface{}) { cancel() }}, retryDelay)
	go a.Run(ctx)
	a.ScheduleConfigUpdate("test")
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
		} else {
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
	a := NewAgent(TestProxy{start, cleanup, nil}, testRetry)
	go a.Run(ctx)
	a.ScheduleConfigUpdate(desired0)
	a.ScheduleConfigUpdate(desired1)
	a.ScheduleConfigUpdate(desired2)
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
	a := NewAgent(TestProxy{start, func(_ int) {}, nil}, testRetry)
	go a.Run(ctx)
	a.ScheduleConfigUpdate(desired)

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
	a := NewAgent(TestProxy{start, func(_ int) {}, nil}, retry)
	go a.Run(ctx)
	a.ScheduleConfigUpdate(0)
	a.ScheduleConfigUpdate(1)
	a.ScheduleConfigUpdate(2)
	<-ctx.Done()
}
