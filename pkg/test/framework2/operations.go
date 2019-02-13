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

package framework2

import (
	"flag"
	"fmt"
	"os"
	"sync"
	"testing"

	"github.com/pkg/errors"
	"istio.io/istio/pkg/test/framework2/components/environment/factory"
	"istio.io/istio/pkg/test/framework2/runtime"
	"istio.io/istio/pkg/test/scopes"
)

const exitCodeFrameworkError = 255

var (
	mu sync.Mutex
	rt *runtime.Instance
)

// SetupFn is a function used for performing suite-level setup actions
type SetupFn func(scope *runtime.SuiteContext) error

// RunSuite runs the test suite.
func RunSuite(testID string, m *testing.M, setupFn SetupFn) {
	if setupFn == nil {
		setupFn = func(scope *runtime.SuiteContext) error { return nil }
	}

	errlevel := runSuite(testID, m, setupFn)
	os.Exit(errlevel)
}

// Run is a wrapper for wrapping around *testing.T in a test function.
func Run(t *testing.T, fn func(s *runtime.TestContext)) {
	mu.Lock()

	if rt == nil {
		mu.Unlock()
		panic("call to scope wihtout running the test framework")
	}
	r := rt
	mu.Unlock()

	r.RunTest(t, fn)
}

func runSuite(testID string, m *testing.M, setupFn SetupFn) int {
	err := doInit(testID)
	if err != nil {
		scopes.Framework.Errorf("Framework init error: %v", err)
		return exitCodeFrameworkError
	}

	defer doCleanup()

	if err = doSetup(setupFn); err != nil {
		scopes.Framework.Errorf("Error during setup: %v", err)
		return exitCodeFrameworkError
	}

	scopes.CI.Infof("=== BEGIN: test runSuite: '%s' ===", testID)
	errLevel := m.Run()
	scopes.CI.Infof("=== DONE: test runSuite: '%s' ===", testID)

	if errLevel != 0 {
		if rt.Suite.Settings().CIMode {
			rt.Dump()
		}
	}

	return errLevel
}

func doSetup(fn SetupFn) error {
	mu.Lock()
	defer mu.Unlock()

	scopes.CI.Infof("=== BEGIN: test setup: '%s' ===", rt.Suite.Settings().TestID)
	err := fn(rt.Suite)
	scopes.CI.Infof("=== DONE: test setup: '%s' ===", rt.Suite.Settings().TestID)
	return err
}

func doInit(testID string) error {
	mu.Lock()
	defer mu.Unlock()

	if rt != nil {
		return errors.New("already initialized")
	}

	// Parse flags and init logging.
	flag.Parse()

	s := runtime.SettingsFromCommandline(testID)

	if err := configureLogging(s.CIMode); err != nil {
		return err
	}

	scopes.CI.Infof("Test Framework runtime settings:\n%s", s.String())

	// Ensure that the work dir is set.
	if err := os.MkdirAll(s.RunDir(), os.ModePerm); err != nil {
		return fmt.Errorf("error creating workdir %q: %v", s.RunDir(), err)
	}
	scopes.Framework.Infof("Output path: %v", s.RunDir())

	var err error
	rt, err = runtime.New(s, factory.New)
	return err
}

func doCleanup() {
	mu.Lock()
	defer mu.Unlock()

	if rt == nil {
		return
	}

	if err := rt.Close(); err != nil {
		scopes.Framework.Errorf("Error during close: %v", err)
	}
	rt = nil
}
