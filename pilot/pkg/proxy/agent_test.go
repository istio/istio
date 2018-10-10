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
	"istio.io/istio/pkg/log"
	"os/exec"
	"strconv"
	"syscall"
	"io/ioutil"
	"os"
)

var (
	testRetry = Retry{
		InitialInterval: time.Millisecond,
		MaxRetries:      10,
	}
)

// TestProxy sample struct for proxy
type TestProxy struct {
	run     func(interface{}, int, <-chan error) (*exec.Cmd, error)
	cleanup func(ExitStatus)
	panic   func(interface{})
}

func (tp TestProxy) Run(config interface{}, epoch int, stop <-chan error) (*exec.Cmd, error) {
	return tp.run(config, epoch, stop)
}

func (tp TestProxy) Cleanup(status ExitStatus) {
	if tp.cleanup != nil {
		tp.cleanup(status)
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
	start := func(config interface{}, epoch int, _ <-chan error) (*exec.Cmd, error) {
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
		return nil, nil
	}
	cleanup := func(status ExitStatus) {
		if current != 0 {
			t.Error("Expected epoch to be set")
		}
		if status.Epoch != 0 {
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
	start := func(config interface{}, epoch int, _ <-chan error) (*exec.Cmd, error) {
		if epoch == 1 {
			t.Error("Should start only once for same config")
		}
		<-ctx.Done()
		return nil, nil
	}
	cleanup := func(_ ExitStatus) {}
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
	start := func(config interface{}, epoch int, _ <-chan error) (*exec.Cmd, error) {
		if config == bad {
			return nil, nil
		}
		if config == good && applied {
			t.Errorf("Config has already been applied")
		}
		applied = true
		<-ctx.Done()
		return nil, nil
	}
	cleanup := func(status ExitStatus) {
		// we should expect to see three epochs only: 0 for good, 1 for bad
		if status.Epoch == 1 {
			go func() {
				a.ScheduleConfigUpdate(good)
				cancel()
			}()
		} else if status.Epoch != 0 {
			t.Errorf("Unexpected epoch %d", status.Epoch)
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
	start := func(config interface{}, epoch int, abort <-chan error) (*exec.Cmd, error) {
		if config == bad {
			return nil, errors.New(bad)
		}
		select {
		case err := <-abort:
			if config == good1 {
				aborted1 = true
			} else if config == good2 {
				aborted2 = true
			}
			return nil,err
		case <-ctx.Done():
		}
		return nil, nil
	}
	cleanup := func(_ ExitStatus) {
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
	start := func(config interface{}, epoch int, _ <-chan error) (*exec.Cmd, error) {
		if epoch == 0 && retry == 0 {
			retry++
			return nil, fmt.Errorf("error on try %d", retry)
		} else if epoch == 0 && retry == 1 {
			retry++
			return nil, fmt.Errorf("error on try %d", retry)
		} else if epoch == 0 && retry == 2 {
			retry++
			cancel()
		} else {
			t.Errorf("Unexpected epoch %d and retry %d", epoch, retry)
			cancel()
		}
		return nil, nil
	}
	cleanup := func(_ ExitStatus) {}
	a := NewAgent(TestProxy{start, cleanup, nil}, testRetry)
	go a.Run(ctx)
	a.ScheduleConfigUpdate("test")
	<-ctx.Done()
}

// TestExceedBudget drives to permanent failure
func TestExceedBudget(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	retry := 0
	start := func(config interface{}, epoch int, _ <-chan error) (*exec.Cmd, error) {
		if epoch == 0 && retry == 0 {
			retry++
			return nil, fmt.Errorf("error on try %d", retry)
		} else if epoch == 0 && retry == 1 {
			retry++
			return nil, fmt.Errorf("error on try %d", retry)
		} else {
			t.Errorf("Unexpected epoch %d and retry %d", epoch, retry)
			cancel()
		}
		return nil, nil
	}
	cleanup := func(status ExitStatus) {
		if status.Epoch == 0 && (retry == 0 || retry == 1 || retry == 2) {
		} else {
			t.Errorf("Unexpected epoch %d and retry %d", status.Epoch, retry)
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
	start := func(config interface{}, epoch int, _ <-chan error) (*exec.Cmd, error) {
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
		return nil, nil
	}
	finished0, finished1, finished2 := false, false, false
	cleanup := func(status ExitStatus) {
		// epoch 1 finishes before epoch 0
		if status.Epoch == 1 {
			finished1 = true
			if finished0 || finished2 {
				t.Errorf("Expected epoch 1 to be first to finish")
			}
			close(stop0)
		} else if status.Epoch == 0 {
			finished0 = true
			if !finished1 || finished2 {
				t.Errorf("Expected epoch 0 to be second to finish")
			}
			cancel()
		} else if status.Epoch == 2 {
			finished2 = true
			if !finished0 || !finished1 {
				t.Errorf("Expected epoch 2 to be last to finish")
			}
		} else {
			t.Errorf("Unexpected epoch %d in cleanup", status.Epoch)
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
	start := func(config interface{}, epoch int, _ <-chan error) (*exec.Cmd, error) {
		if epoch == 0 && !failed {
			failed = true
			return nil, nil
		}
		if epoch > 0 {
			t.Errorf("Should not reconcile after success")
		}
		<-ctx.Done()
		return nil, nil
	}
	a := NewAgent(TestProxy{start, func(_ ExitStatus) {}, nil}, testRetry)
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
	start := func(config interface{}, epoch int, abort <-chan error) (*exec.Cmd, error) {
		if config == 2 {
			close(crash1)
			return nil, errors.New("planned crash for 2")
		} else if config == 1 {
			<-crash1
			// ignore abort, crash by itself
			<-time.After(100 * time.Millisecond)
			return nil, errors.New("planned crash for 1")
		} else if config == 0 {
			// abort, a bit later
			<-time.After(300 * time.Millisecond)
			err := <-abort
			cancel()
			return nil, err
		}
		return nil, nil
	}
	retry := testRetry
	retry.InitialInterval = 1 * time.Second
	a := NewAgent(TestProxy{start, func(_ ExitStatus) {}, nil}, retry)
	go a.Run(ctx)
	a.ScheduleConfigUpdate(0)
	a.ScheduleConfigUpdate(1)
	a.ScheduleConfigUpdate(2)
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

	start := func(config interface{}, epoch int, abort <-chan error) (*exec.Cmd, error) {

		lock.Lock()
		if try >= 2 {
			cancel()
			lock.Unlock()
			return nil, nil
		}
		try = try + 1
		lock.Unlock()
		switch epoch {
		case 0:
			<-crash

			return nil, errors.New("crash")
		case 1:
			close(crash)
			err := <-abort
			return nil, err
		}
		return nil, nil
	}
	a := NewAgent(TestProxy{start, func(_ ExitStatus) {}, nil}, testRetry)
	go a.Run(ctx)
	a.ScheduleConfigUpdate(0)
	a.ScheduleConfigUpdate(1)
	select {
	case <-ctx.Done():
	case <-time.After(1 * time.Second):
		t.Error("liveness check failed")
	}
}

func generateTempScript(dir string, filename string, content string) (*os.File, error) {
	tmpfile, err := ioutil.TempFile(dir, filename)
	if err != nil {
		return nil, fmt.Errorf("Cannot create tempfile %s/%s: %s", dir, filename, err)
	}

	if _, err := tmpfile.Write([]byte(content)); err != nil {
		tmpfile.Close()
		os.Remove(tmpfile.Name())
		return nil, fmt.Errorf("Cannot write to tempfile %s/%s: %s", dir, filename, err)
	}

	if err := tmpfile.Close(); err != nil {
		os.Remove(tmpfile.Name())
		return nil, fmt.Errorf("Cannot close tempfile %s/%s: %s", dir, filename, err)
	}

	if err := os.Chmod(tmpfile.Name(), 0555); err != nil {
		os.Remove(tmpfile.Name())
		return nil, fmt.Errorf("Cannot make tempfile %s/%s executable: %s", dir, filename, err)
	}

	return tmpfile, nil
}

// TestSignalExit tests handling processes that exit due to a signal
func TestSignalExit(t *testing.T) {
	ctx, _ := context.WithCancel(context.Background())
	ch := make(chan ExitStatus)
	desired := "config"
	var signo syscall.Signal = 9

	// Generate a script that will kill itself with the signal specified as a commandline argument
	script, err := generateTempScript("", "test_signal_exit.sh", "#!/bin/sh\nkill -$1 $$")
	if err != nil {
		t.Error(err)
		return
	}
	defer os.Remove(script.Name())

	start := func(config interface{}, epoch int, _ <-chan error) (*exec.Cmd, error) {
		cmd := exec.Command(script.Name(), strconv.Itoa(int(signo)))
		return cmd, nil
	}
	cleanup := func(status ExitStatus) {
		log.Info("Command cleaned up");
		ch <- status
	}

	a := NewAgent(TestProxy{start, cleanup, nil}, testRetry)
	go a.Run(ctx)

	a.ScheduleConfigUpdate(desired)
	status := <-ch

	if status.Reason != Killed || status.Signal != signo {
		t.Errorf("expected killed by signal %d, but got %v", signo, status)
	}
}
