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
	"istio.io/istio/pkg/test/label"
	"istio.io/istio/pkg/test/local"
)

const (
	maxTestIDLength = 30
)

var scope = log.RegisterScope("driver", "Logger for the test framework driver", 0)

type driver struct {
	lock sync.Mutex

	allowedLabels map[label.Label]struct{}

	testID  string
	runID   string
	m       *testing.M
	env     environment.Interface
	running bool

	suiteDependencies []dependency.Dependency

	initializedDependencies map[dependency.Dependency]interface{}
}

var _ Interface = &driver{}

// New returns a new driver instance.
func New() Interface {
	return &driver{
		initializedDependencies: make(map[dependency.Dependency]interface{}),
	}
}

// Initialize implements same-named Interface method.
func (d *driver) Initialize(a *Args) error {
	scope.Debugf("Enter: driver.Initialize (%s)", d.testID)
	d.lock.Lock()
	defer d.lock.Unlock()

	if err := a.Validate(); err != nil {
		return err
	}

	if d.testID != "" {
		return errors.New("test driver is already initialized")
	}

	// TODO
	// if driv.tmpDir, err = tmp.Create(driv.runID); err != nil {
	// 	return
	// }
	//
	// if err = logging.Initialize(driv.runID); err != nil {
	// 	return
	// }
	//

	d.suiteDependencies = a.SuiteDependencies

	var env environment.Interface
	var err error
	switch a.Environment {
	case EnvLocal:
		env, err = local.NewEnvironment()

	case EnvKubernetes:
		env, err = cluster.NewEnvironment(a.KubeConfig)

	default:
		return fmt.Errorf("unrecognized environment: %s", a.Environment)
	}

	if err != nil {
		return fmt.Errorf("unable to initialize environment '%s': %v", a.Environment, err)
	}
	d.env = env

	d.testID = a.TestID
	d.runID = generateRunID(a.TestID)
	d.m = a.M

	if a.Labels != "" {
		d.allowedLabels = make(map[label.Label]struct{})

		parts := strings.Split(a.Labels, ",")
		for _, p := range parts {
			d.allowedLabels[label.Label(p)] = struct{}{}
		}
		scope.Debugf("Suite level labels: %s", a.Labels)
	}

	return nil
}

// TestID implements same-named Interface method.
func (d *driver) TestID() string {
	scope.Debugf("Enter: driver.TestID (%s)", d.testID)
	d.lock.Lock()
	defer d.lock.Unlock()

	return d.testID
}

// RunID implements same-named Interface method.
func (d *driver) RunID() string {
	scope.Debugf("Enter: driver.RunID (%s)", d.testID)
	d.lock.Lock()
	defer d.lock.Unlock()

	return d.runID
}

// Run implements same-named Interface method.
func (d *driver) Run() int {
	scope.Debugf("Enter: driver.Run (%s)", d.testID)
	d.lock.Lock()

	if d.testID == "" {
		d.lock.Unlock()
		scope.Error("test driver is not initialized yet")
		return -1
	}

	if d.running {
		d.lock.Unlock()
		scope.Error("test driver is already running")
		return -2
	}

	for _, dep := range d.suiteDependencies {
		if err := d.initializeDependency(dep); err != nil {
			log.Errorf("Failed initializing dependency '%s': %v", dep, err)
			return -3
		}
	}

	d.running = true

	m := d.m
	d.lock.Unlock()

	// Call m.Run() while not holding the lock.
	rt := m.Run()

	// Reacquire lock.
	d.lock.Lock()
	defer d.lock.Unlock()

	d.running = false

	d.doCleanup()
	return rt
}

// GetEnvironment implements same-named Interface method.
func (d *driver) GetEnvironment(t testing.TB) environment.Interface {
	t.Helper()
	scope.Debugf("Enter: driver.GetEnvironment (%s)", d.testID)
	d.lock.Lock()
	defer d.lock.Unlock()

	if !d.running {
		t.Fatalf("Test driver is not running.")
	}

	return d.env
}

// InitializeTestDependencies implements same-named Interface method.
func (d *driver) InitializeTestDependencies(t testing.TB, dependencies []dependency.Dependency) {
	t.Helper()
	scope.Debugf("Enter: driver.InitializeTestDependencies (%s)", d.testID)
	d.lock.Lock()
	defer d.lock.Unlock()

	if !d.running {
		t.Fatalf("Test driver is not running.")
	}

	// Initialize dependencies only once.
	for _, dep := range dependencies {
		if err := d.initializeDependency(dep); err != nil {
			t.Fatalf("unable to satisfy dependency '%v': %v", dep, err)
		}
	}
}

// CheckLabels implements same-named Interface method.
func (d *driver) CheckLabels(t testing.TB, labels []label.Label) {
	t.Helper()
	scope.Debugf("Enter: driver.CheckLabels (%s)", d.testID)
	d.lock.Lock()
	defer d.lock.Unlock()

	if !d.running {
		t.Fatalf("Test driver is not running.")
	}

	skip := false
	if len(d.allowedLabels) > 0 {
		// Only filter if the labels are specified.
		skip = true
		for _, l := range labels {
			if _, ok := d.allowedLabels[l]; ok {
				skip = false
				break
			}
		}
	}

	if skip && !t.Skipped() {
		t.Skip("Skipping(Filtered): No matching label found")
	}
}

func (d *driver) doCleanup() {
	// should be already locked.

	for k, v := range d.initializedDependencies {
		if s, ok := k.(internal.Stateful); ok {
			s.Cleanup(d.env, v)
		}
	}
}

func (d *driver) initializeDependency(dep dependency.Dependency) error {
	scope.Debugf("initializing dependency: %v", dep)
	s, ok := dep.(internal.Stateful)
	if !ok {
		return nil
	}

	instance, ok := d.initializedDependencies[dep]
	if ok {
		// If they are already satisfied, then signal a "reset", to ensure a clean, well-known driverState.
		if err := s.Reset(d.env, instance); err != nil {
			return fmt.Errorf("unable to reset: %v", err)
		}
		return nil
	}

	var err error
	if instance, err = s.Initialize(d.env); err != nil {
		return fmt.Errorf("dependency init error: %v", err)
	}

	d.initializedDependencies[dep] = instance
	return nil
}

func generateRunID(testID string) string {
	u := uuid.New().String()
	u = strings.Replace(u, "-", "", -1)
	testID = strings.Replace(testID, "_", "-", -1)
	// We want at least 6 characters of uuid padding
	padding := maxTestIDLength - len(testID)
	return fmt.Sprintf("%s-%s", testID, u[0:padding])
}
