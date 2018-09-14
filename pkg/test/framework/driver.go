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

package framework

import (
	"flag"
	"fmt"
	"io"
	"sync"
	"testing"

	"istio.io/istio/pkg/log"

	"istio.io/istio/pkg/test/framework/components"
	"istio.io/istio/pkg/test/framework/components/registry"
	"istio.io/istio/pkg/test/framework/dependency"
	env "istio.io/istio/pkg/test/framework/environment"
	"istio.io/istio/pkg/test/framework/environments/kubernetes"
	"istio.io/istio/pkg/test/framework/environments/local"
	"istio.io/istio/pkg/test/framework/internal"
	"istio.io/istio/pkg/test/framework/settings"
)

// internal state for the driver.
type state int

const (
	// Runtime states for the driver

	// The driver is freshly created. It hasn't run yet.
	created state = iota

	// The driver is currently running tests.
	running

	// the driver has completed running tests.
	completed
)

var (
	// TODO(nmittler): Add logging options to flags.
	logOptions = log.DefaultOptions()
)

type driver struct {
	lock sync.Mutex

	context *internal.TestContext

	env *environment

	// The names of the tests that we've encountered so far.
	testNames map[string]struct{}

	state state

	// Hold on to the suite dependencies as those can be set before we can build context.
	suiteDependencies []dependency.Instance
}

// New returns a new driver instance.
func newDriver() *driver {
	return &driver{
		testNames: make(map[string]struct{}),
		state:     created,
	}
}

// Run implements same-named Driver method.
func (d *driver) Run(testID string, m *testing.M) (int, error) {
	rt, err := d.initialize(testID)
	if err != nil {
		return rt, err
	}

	// Call m.Run() while not holding the lock.
	lab.Infof(">>> Beginning test run for: '%s'", testID)
	rt = m.Run()
	lab.Infof("<<< Completing test run for: '%s'", testID)

	d.lock.Lock()
	defer d.lock.Unlock()
	d.state = completed

	if !d.context.Settings().NoCleanup {
		d.context.Tracker.Cleanup()
		if closer, ok := d.context.Environment().(io.Closer); ok {
			err := closer.Close()
			if err != nil {
				lab.Warnf("Error during environment close: %v", err)
			}
		}
	}

	return rt, nil
}

// AcquireEnvironment implementation
func (d *driver) AcquireEnvironment(t testing.TB) env.Environment {
	t.Helper()
	scope.Debugf("Enter: driver.AcquireEnvironment (%s)", d.context.Settings().TestID)
	d.lock.Lock()
	defer d.lock.Unlock()

	if d.state != running {
		t.Fatalf("Test driver is not running.")
	}

	// Double check the test name to see if this is a singleton call for this test?
	if _, ok := d.testNames[t.Name()]; ok {
		t.Fatalf("AcquireEnvironment should be called only once during a test session. (test='%s')", t.Name())
	}
	d.testNames[t.Name()] = struct{}{}

	if err := d.env.controller.Reset(); err != nil {
		t.Fatalf("AcquireEnvironment failed to reset the environment state: %v", err)
	}

	// Reset all resettables, as we're going to be executing within the context of a new test.
	if err := d.context.Tracker.Reset(); err != nil {
		t.Fatalf("driver.AcquireEnvironment failed to reset the resource state: %v", err)
	}

	return d.env
}

// SuiteRequires indicates that the whole suite requires particular dependencies.
func (d *driver) SuiteRequires(dependencies []dependency.Instance) error {
	d.lock.Lock()
	defer d.lock.Unlock()

	if d.state != created {
		return fmt.Errorf("test driver is not in a valid state")
	}

	d.suiteDependencies = append(d.suiteDependencies, dependencies...)
	return nil
}

// Requires implements same-named Driver method.
func (d *driver) Requires(t testing.TB, dependencies []dependency.Instance) {
	t.Helper()
	scope.Debugf("Enter: driver.Requires (%s)", d.context.Settings().TestID)
	d.lock.Lock()
	defer d.lock.Unlock()

	if d.state != running {
		t.Fatalf("Test driver is not running.")
	}

	// Initialize dependencies only once.
	for _, dep := range dependencies {
		envID := d.context.Settings().Environment

		switch dep {
		case dependency.Local:
			if envID != settings.Local {
				t.Skipf("requires %s environment. Running %s", settings.Local, envID)
			}
		case dependency.Kubernetes:
			if envID != settings.Kubernetes {
				t.Skipf("requires %s environment. Running %s", settings.Kubernetes, envID)
			}
		default:
			c, ok := d.context.Registry.Get(dep)
			if !ok {
				t.Skipf("unable to locate dependency '%v' in environment %s", dep, envID)
			}
			if _, err := d.context.Tracker.Initialize(d.context, c); err != nil {
				scope.Errorf("Failed to initialize dependency '%s': %v", dep, err)
				t.Fatalf("unable to satisfy dependency '%v': %v", dep, err)
			}
		}
	}
}

func (d *driver) initialize(testID string) (int, error) {
	d.lock.Lock()
	defer d.lock.Unlock()
	if d.state != created {
		return -1, fmt.Errorf("driver.Run must be called only once")
	}
	d.state = running

	// Parse flags and init logging.
	flag.Parse()
	if err := log.Configure(logOptions); err != nil {
		return -1, err
	}

	// Initialize settings
	s, err := settings.New(testID)
	if err != nil {
		return -1, err
	}
	scope.Debugf("driver settings: %+v", s)

	// Initialize the environment.
	var impl internal.EnvironmentController
	var reg *registry.Registry
	switch s.Environment {
	case settings.Local:
		impl = local.New()
		reg = components.Local
	case settings.Kubernetes:
		impl = kubernetes.New()
		reg = components.Kubernetes
	default:
		return -2, fmt.Errorf("unrecognized environment: %s", d.context.Settings().Environment)
	}
	d.context = internal.NewTestContext(*s, impl, reg)
	if err := impl.Initialize(d.context); err != nil {
		return -2, err
	}
	d.env = &environment{
		ctx:        d.context,
		controller: impl,
	}

	// Finally initialize suite-level dependencies.
	for _, dep := range d.suiteDependencies {
		c, ok := reg.Get(dep)
		if !ok {
			return -3, fmt.Errorf("dependency %s not available for environment %s", dep, impl.EnvironmentID())
		}
		if _, err := d.context.Tracker.Initialize(d.context, c); err != nil {
			scope.Errorf("driver.Run: Dependency error '%s': %v", dep, err)
			return -3, err
		}
	}

	return 0, nil
}
