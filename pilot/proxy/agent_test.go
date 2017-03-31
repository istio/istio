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
	"errors"
	"testing"
	"time"
)

// TestStartStop tests basic start, cleanup sequence
func TestStartStop(t *testing.T) {
	current := -1
	stop := make(chan struct{})
	desired := "config"
	start := func(config interface{}, epoch int) error {
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
		close(stop)
	}
	a := NewAgent(start, cleanup, 10, time.Millisecond)
	go a.Run(stop)
	a.ScheduleConfigUpdate(desired)
	<-stop
}

// TestApplyTwice tests that scheduling the same config does not trigger a restart
func TestApplyTwice(t *testing.T) {
	stop := make(chan struct{})
	desired := "config"
	start := func(config interface{}, epoch int) error {
		if epoch == 1 {
			t.Error("Should start only once for same config")
		}
		<-stop
		return nil
	}
	cleanup := func(epoch int) {}
	a := NewAgent(start, cleanup, 10, time.Millisecond)
	go a.Run(stop)
	a.ScheduleConfigUpdate(desired)
	a.ScheduleConfigUpdate(desired)
	close(stop)
}

// TestApplyThrice applies same config twice but if a bad config between them
func TestApplyThrice(t *testing.T) {
	stop := make(chan struct{})
	good := "good"
	bad := "bad"
	applied := false
	var a Agent
	start := func(config interface{}, epoch int) error {
		if config == bad {
			return errors.New(bad)
		}
		if config == good && applied {
			t.Errorf("Config has already been applied")
		}
		applied = true
		<-stop
		return nil
	}
	cleanup := func(epoch int) {
		// we should expect to see three epochs only: 0 for good, 1 for bad
		if epoch == 1 {
			go func() {
				a.ScheduleConfigUpdate(good)
				close(stop)
			}()
		} else if epoch != 0 {
			t.Errorf("Unexpected epoch %d", epoch)
		}
	}
	a = NewAgent(start, cleanup, 0, time.Millisecond)
	go a.Run(stop)
	a.ScheduleConfigUpdate(good)
	a.ScheduleConfigUpdate(bad)
	<-stop
}

// TestStartFail injects an error in 2 tries to start the proxy
func TestStartFail(t *testing.T) {
	stop := make(chan struct{})
	retry := 0
	start := func(config interface{}, epoch int) error {
		if epoch == 0 && retry == 0 {
			retry++
			return errors.New("Injected error on first try")
		} else if epoch == 0 && retry == 1 {
			retry++
			return errors.New("Injected error on second try")
		} else if epoch == 0 && retry == 2 {
			retry++
			close(stop)
		} else {
			t.Errorf("Unexpected epoch %d and retry %d", epoch, retry)
			close(stop)
		}
		return nil
	}
	cleanup := func(epoch int) {}
	a := NewAgent(start, cleanup, 10, time.Millisecond)
	go a.Run(stop)
	a.ScheduleConfigUpdate("test")
	<-stop
}

// TestExceedBudget drives to permanent failure
func TestExceedBudget(t *testing.T) {
	stop := make(chan struct{})
	retry := 0
	start := func(config interface{}, epoch int) error {
		if epoch == 0 && retry == 0 {
			retry++
			return errors.New("Injected error on first try")
		} else if epoch == 0 && retry == 1 {
			retry++
			return errors.New("Injected error on second try")
		} else {
			t.Errorf("Unexpected epoch %d and retry %d", epoch, retry)
			close(stop)
		}
		return nil
	}
	cleanup := func(epoch int) {
		if epoch == 0 && (retry == 0 || retry == 1) {
		} else if epoch == 0 && retry == 2 {
			// make sure no more tries are attempted
			go func() {
				<-time.After(15 * time.Millisecond)
				close(stop)
			}()
		} else {
			t.Errorf("Unexpected epoch %d and retry %d", epoch, retry)
			close(stop)
		}
	}
	a := NewAgent(start, cleanup, 1, time.Millisecond)
	go a.Run(stop)
	a.ScheduleConfigUpdate("test")
	<-stop
}

// TestStartTwiceStop applies three configs and validates that cleanups are called in order
func TestStartTwiceStop(t *testing.T) {
	stop := make(chan struct{})
	stop0 := make(chan struct{})
	stop1 := make(chan struct{})
	desired0 := "config0"
	desired1 := "config1"
	desired2 := "config2"
	start := func(config interface{}, epoch int) error {
		if config == desired0 && epoch == 0 {
			<-stop0
		} else if config == desired1 && epoch == 1 {
			<-stop1
		} else if config == desired2 && epoch == 2 {
			close(stop1)
			<-stop
		} else {
			t.Errorf("Unexpected start %v, epoch %d", config, epoch)
			close(stop)
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
			close(stop)
		} else if epoch == 2 {
			finished2 = true
			if !finished0 || !finished1 {
				t.Errorf("Expected epoch 2 to be last to finish")
			}
		} else {
			t.Errorf("Unexpected epoch %d in cleanup", epoch)
			close(stop)
		}
	}
	a := NewAgent(start, cleanup, 10, time.Millisecond)
	go a.Run(stop)
	a.ScheduleConfigUpdate(desired0)
	a.ScheduleConfigUpdate(desired1)
	a.ScheduleConfigUpdate(desired2)
	<-stop
}

// TestRecovery tests that recovery is applied once
func TestRecovery(t *testing.T) {
	stop := make(chan struct{})
	desired := "config"
	failed := false
	start := func(config interface{}, epoch int) error {
		if epoch == 0 && !failed {
			failed = true
			return errors.New("Cause retry")
		}
		if epoch > 0 {
			t.Errorf("Should not reconcile after success")
		}
		<-stop
		return nil
	}
	a := NewAgent(start, func(_ int) {}, 10, time.Millisecond)
	go a.Run(stop)
	a.ScheduleConfigUpdate(desired)

	// make sure we don't try to reconcile twice
	<-time.After(100 * time.Millisecond)
	close(stop)
}
