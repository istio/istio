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
	"istio.io/istio/pkg/test/dependency"
	"istio.io/istio/pkg/test/environment"
	"istio.io/istio/pkg/test/internal"
	"istio.io/istio/pkg/test/label"
)

const (
	maxTestIDLength = 30
)

var scope = log.RegisterScope("driver", "Logger for the test framework driver", 0)

type driver struct {
	lock sync.Mutex

	allowedLabels map[label.Label]struct{}

	testID string
	runID  string
	m      *testing.M

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
	log.Debugf("Enter: driver.Initialize (%s)", d.testID)
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
	d.testID = a.TestID
	d.runID = generateRunID(a.TestID)
	d.m = a.M

	if a.Labels != "" {
		d.allowedLabels = make(map[label.Label]struct{})

		parts := strings.Split(a.Labels, ",")
		for _, p := range parts {
			d.allowedLabels[label.Label(p)] = struct{}{}
		}
	}

	return nil
}

// TestID implements same-named Interface method.
func (d *driver) TestID() string {
	log.Debugf("Enter: driver.TestID (%s)", d.testID)
	d.lock.Lock()
	defer d.lock.Unlock()

	return d.testID
}

// RunID implements same-named Interface method.
func (d *driver) RunID() string {
	log.Debugf("Enter: driver.RunID (%s)", d.testID)
	d.lock.Lock()
	defer d.lock.Unlock()

	return d.runID
}

// Run implements same-named Interface method.
func (d *driver) Run() int {
	log.Debugf("Enter: driver.Run (%s)", d.testID)
	d.lock.Lock()

	if d.testID == "" {
		d.lock.Unlock()
		scope.Error("test driver is not initialized yet")
		return -1
	}

	// TODO: Check for multiple run-calls

	m := d.m
	d.lock.Unlock()

	// Call m.Run() while not holding the lock.
	rt := m.Run()

	// Reacquire lock.
	d.lock.Lock()
	defer d.lock.Unlock()

	d.doCleanup()
	return rt
}

// GetEnvironment implements same-named Interface method.
func (d *driver) GetEnvironment(t testing.TB) environment.Interface {
	log.Debugf("Enter: driver.GetEnvironment (%s)", d.testID)
	d.lock.Lock()
	defer d.lock.Unlock()
	// TODO
	panic("Not yet implemented.")
}

// CheckDependencies implements same-named Interface method.
func (d *driver) CheckDependencies(t testing.TB, dependencies []dependency.Dependency) {
	log.Debugf("Enter: driver.CheckDependencies (%s)", d.testID)
	d.lock.Lock()
	defer d.lock.Unlock()

	// Initialize dependencies only once.
	for _, dep := range dependencies {
		log.Debugf("dep: %v", dep)
		s, ok := dep.(internal.Stateful)
		if !ok {
			continue
		}

		instance, ok := d.initializedDependencies[dep]
		if ok {
			// If they are already satisfied, then signal a "reset", to ensure a clean, well-known driverState.
			if err := s.Reset(instance); err != nil {
				t.Fatalf("Unable to reset dependency '%v': %v", dep, err)
				return
			}
			continue
		}

		var err error
		if instance, err = s.Initialize(); err != nil {
			t.Fatalf("Unable to satisfy dependency '%v': %v", dep, err)
			return
		}

		d.initializedDependencies[dep] = instance
	}
}

// CheckLabels implements same-named Interface method.
func (d *driver) CheckLabels(t testing.TB, labels []label.Label) {
	log.Debugf("Enter: driver.CheckLabels (%s)", d.testID)
	d.lock.Lock()
	defer d.lock.Unlock()

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
			s.Cleanup(v)
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
