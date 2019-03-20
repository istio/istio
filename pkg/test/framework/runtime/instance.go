//  Copyright 2018 Istio Authors
//
//  Licensed under the Apache License, Version 2.0 (the "License");
//  you may not use this file except in compliance with the License.
//  You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
//  Unless required by applicable law or agreed to in writing, software
//  distributed under the License is distributed on an "AS IS" BASIS,
//  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//  See the License for the specific language governing permissions and
//  limitations under the License.

package runtime

import (
	"flag"
	"fmt"
	"sync"
	"testing"

	"istio.io/istio/pkg/log"
	"istio.io/istio/pkg/test/framework/api/context"
	"istio.io/istio/pkg/test/scopes"
)

// internal state for the runtime.
type state int

const (
	// States for the runtime.

	// The runtime is freshly created. It hasn't run yet.
	created state = iota

	// The runtime is currently running tests.
	running

	// the runtime has completed running tests.
	completed
)

// Instance for the test environment.
type Instance struct {
	lock sync.Mutex

	ctx *contextImpl

	// The names of the tests that we've encountered so far.
	testNames map[string]struct{}

	state state
}

// New returns a new runtime instance.
func New() *Instance {
	return &Instance{
		testNames: make(map[string]struct{}),
		state:     created,
	}
}

// Run is a helper for executing test main with appropriate resource allocation/doCleanup steps.
// It allows us to do post-run doCleanup, and flag parsing.
func (d *Instance) Run(testID string, m *testing.M) (int, error) {
	rt, err := d.initialize(testID)
	if err != nil {
		return rt, err
	}

	// Call m.Run() while not holding the lock.
	scopes.CI.Infof("=== BEGIN: test run: '%s' ===", testID)
	rt = m.Run()
	scopes.CI.Infof("=== DONE: test run: '%s' ===", testID)

	d.lock.Lock()
	defer d.lock.Unlock()
	d.state = completed

	if err := d.ctx.Close(); err != nil {
		scopes.Framework.Warnf("Error during environment close: %v", err)
	}

	return rt, nil
}

// GetContext resets and returns the environment. Should be called exactly once per test.
func (d *Instance) GetContext(t testing.TB) context.Instance {
	t.Helper()
	scopes.Framework.Debugf("Enter: runtime.GetContext (%s)", d.ctx.TestID())
	d.lock.Lock()
	defer d.lock.Unlock()

	if d.state != running {
		t.Fatalf("Test runtime is not running.")
	}

	// Double check the test name to see if this is a singleton call for this test?
	if _, ok := d.testNames[t.Name()]; ok {
		t.Fatalf("GetContext should be called only once during a test session. (test='%s')", t.Name())
	}
	d.testNames[t.Name()] = struct{}{}

	if err := d.ctx.Reset(); err != nil {
		d.ctx.DumpState(t.Name())
		t.Fatalf("GetContext failed to reset the environment state: %v", err)
	}

	return d.ctx
}

func (d *Instance) initialize(testID string) (int, error) {
	d.lock.Lock()
	defer d.lock.Unlock()
	if d.state != created {
		return -1, fmt.Errorf("runtime.Run must be called only once")
	}
	d.state = running

	// Parse flags and init logging.
	flag.Parse()

	// Initialize settings
	var err error
	d.ctx, err = newContext(testID)
	if err != nil {
		return -1, err
	}
	scopes.CI.Infof("Test Framework runtime settings:\n%s", d.ctx)

	if err := log.Configure(d.ctx.logOptions); err != nil {
		return -1, err
	}

	return 0, nil
}
