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

package driver

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"

	"istio.io/istio/pkg/log"
	"istio.io/istio/pkg/test/cluster"
	"istio.io/istio/pkg/test/dependency"
	"istio.io/istio/pkg/test/environment"
	"istio.io/istio/pkg/test/internal"
	"istio.io/istio/pkg/test/local"
)

const (
	maxTestIDLength = 30
)

var scope = log.RegisterScope("testframework", "General scope for the test framework", 0)

type driver struct {
	lock sync.Mutex

	args          *Args
	ctx           *internal.TestContext

	// The names of the tests that we've encountered so far.
	testNames map[string]struct{}

	running bool
}

var _ Interface = &driver{}

// New returns a new driver instance.
func New() Interface {
	return &driver{
		testNames: make(map[string]struct{}),
	}
}

// Initialize implements same-named Interface method.
func (d *driver) Initialize(a *Args) error {
	scope.Debugf("Enter: driver.Initialize (%s)", a.TestID)
	d.lock.Lock()
	defer d.lock.Unlock()

	if d.ctx != nil {
		return errors.New("test driver is already initialized")
	}

	// Make a copy of the args
	args := &(*a)
	if err := args.Validate(); err != nil {
		return err
	}

	// Initialize the environment.
	var env internal.Environment
	var err error
	switch a.Environment {
	case EnvLocal:
		env, err = local.NewEnvironment()
	case EnvKube:
		env, err = cluster.NewEnvironment(a.KubeConfig)
	default:
		return fmt.Errorf("unrecognized environment: %s", a.Environment)
	}

	if err != nil {
		return fmt.Errorf("unable to initialize environment '%s': %v", a.Environment, err)
	}

	// Create the context, but do not attach it to the member fields before initializing.
	ctx := internal.NewTestContext(
		args.TestID,
		generateRunID(args.TestID),
		a.WorkDir,
		a.Hub,
		a.Tag,
		env)

	if err = env.Initialize(ctx); err != nil {
		return err
	}

	d.ctx = ctx
	d.args = args

	return nil
}

// Run implements same-named Interface method.
func (d *driver) Run() int {
	scope.Debugf("Enter: driver.Run (%s)", d.ctx.TestID())
	d.lock.Lock()

	// If context is not set, then we're not initialized.
	if d.ctx == nil {
		d.lock.Unlock()
		scope.Error("test driver is not initialized yet")
		return -1
	}

	if d.running {
		d.lock.Unlock()
		scope.Error("test driver is already running")
		return -2
	}

	for _, dep := range d.args.SuiteDependencies {
		if err := d.ctx.Tracker().Initialize(d.ctx, d.ctx.Environment(), dep); err != nil {
			log.Errorf("Failed to initialize dependency '%s': %v", dep, err)
			return -3
		}
	}

	d.running = true

	m := d.args.M
	d.lock.Unlock()

	// Call m.Run() while not holding the lock.
	rt := m.Run()

	// Reacquire lock.
	d.lock.Lock()
	defer d.lock.Unlock()

	d.running = false

	if !d.args.NoCleanup {
		d.ctx.Tracker().Cleanup()
	}

	return rt
}

// AcquireEnvironment implementation
func (d *driver) AcquireEnvironment(t testing.TB) environment.Interface {
	t.Helper()
	scope.Debugf("Enter: driver.AcquireEnvionment (%s)", d.ctx.TestID())
	d.lock.Lock()
	defer d.lock.Unlock()

	if !d.running {
		t.Fatalf("Test driver is not running.")
	}

	// Double check the test name to see if this is a singleton call for this test?
	if _, ok := d.testNames[t.Name()]; ok {
		t.Fatalf("AcquireEnvironment should be called only once during a test session. (test='%s')", t.Name())
	}
	d.testNames[t.Name()] = struct{}{}

	if err := d.ctx.Environment().Reset(); err != nil {
		t.Fatalf("AcquireEnvironment failed to reset the environment state: %v", err)
	}

	// Reset all resettables, as we're going to be executing within the context of a new test.
	if err := d.ctx.Tracker().Reset(d.ctx); err != nil {
		t.Fatalf("AcquireEnvironment failed to reset the resource state: %v", err)
	}

	return d.ctx.Environment()
}

// InitializeTestDependencies implements same-named Interface method.
func (d *driver) InitializeTestDependencies(t testing.TB, dependencies []dependency.Instance) {
	t.Helper()
	scope.Debugf("Enter: driver.InitializeTestDependencies (%s)", d.ctx.TestID())
	d.lock.Lock()
	defer d.lock.Unlock()

	if !d.running {
		t.Fatalf("Test driver is not running.")
	}

	// Initialize dependencies only once.
	for _, dep := range dependencies {
		if err := d.ctx.Tracker().Initialize(d.ctx, d.ctx.Environment(), dep); err != nil {
			log.Errorf("Failed to initialize dependency '%s': %v", dep, err)
			t.Fatalf("unable to satisfy dependency '%v': %v", dep, err)
		}
	}
}

func generateRunID(testID string) string {
	u := uuid.New().String()
	u = strings.Replace(u, "-", "", -1)
	testID = strings.Replace(testID, "_", "-", -1)
	// We want at least 6 characters of uuid padding
	padding := maxTestIDLength - len(testID)
	return fmt.Sprintf("%s-%s", testID, u[0:padding])
}
