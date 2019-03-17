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
	"os"
	"testing"
	"time"

	"github.com/pkg/errors"

	"istio.io/istio/pkg/test/framework/components/environment"
	"istio.io/istio/pkg/test/framework/components/environment/api"
	"istio.io/istio/pkg/test/framework/components/environment/kube"
	"istio.io/istio/pkg/test/framework/components/environment/native"
	"istio.io/istio/pkg/test/framework/core"
	"istio.io/istio/pkg/test/framework/resource"
	"istio.io/istio/pkg/test/framework/runtime"
	"istio.io/istio/pkg/test/scopes"
)

// test.Run uses 0, 1, 2 exit codes. Use different exit codes for our framework.

const (
	// Indicates a framework-level init error
	exitCodeInitError = -1

	// Indicates an error due to the setup function supplied by the user
	exitCodeSetupError = -2
)

var (
	rt *runtime.Instance
)

// SetupFn is a function used for performing suite-level setup actions.
type SetupFn func(ctx SuiteContext) error

// mRunFn abstracts testing.M.run, so that the framework itself can be tested.
type mRunFn func() int

// Main runs the test suite. The Main will run the supplied setup functions before starting test execution.
// It will not return, and will exit the process after running tests.
func Main(testID string, m *testing.M, setupFn ...SetupFn) {
	errlevel := runSuite(testID, m.Run, setupFn...)
	os.Exit(errlevel)
}

func runSuite(testID string, mRun mRunFn, setupFn ...SetupFn) (errLevel int) {
	start := time.Now()

	err := doInit(testID)
	if err != nil {
		scopes.Framework.Errorf("Error during test framework init: %v", err)
		errLevel = exitCodeInitError
		return
	}
	defer doCleanup()

	// Ensure that we will dump if we bail out in the middle of setup.
	defer func() {
		if errLevel != 0 {
			if rt.SuiteContext().Settings().CIMode {
				rt.Dump()
			}
		}
	}()

	defer func() {
		end := time.Now()
		scopes.CI.Infof("=== Suite %q run time: %v ===", testID, end.Sub(start))
	}()

	if err = doTestSetup(setupFn...); err != nil {
		errLevel = exitCodeSetupError
		return
	}

	errLevel = doRun(mRun)
	return
}

// NewContext creates a new test context and returns. It is upto the caller to close to context by calling
// .Done() at the end of the test run.
func NewContext(t *testing.T) TestContext {
	if rt == nil {
		panic("call to scope without running the test framework")
	}
	r := rt

	return r.NewTestContext(nil, t)
}

// Run is a wrapper for wrapping around *testing.T in a test function.
func Run(t *testing.T, fn func(ctx TestContext)) {
	start := time.Now()

	scopes.CI.Infof("=== BEGIN: Test: '%s[%s]' ===", rt.SuiteContext().Settings().TestID, t.Name())
	defer func() {
		end := time.Now()
		scopes.CI.Infof("=== DONE:  Test: '%s[%s] (%v)' ===", rt.SuiteContext().Settings().TestID, t.Name(), end.Sub(start))
	}()

	ctx := NewContext(t)
	defer ctx.Done(t)
	fn(ctx)
}

func doInit(testID string) error {
	if rt != nil {
		return errors.New("framework is already initialized")
	}

	// Parse flags and init logging.
	if !flag.Parsed() {
		flag.Parse()
	}

	s := core.SettingsFromCommandLine(testID)

	if err := configureLogging(s.CIMode); err != nil {
		return err
	}

	scopes.CI.Infof("=== Test Framework Settings ===")
	scopes.CI.Info(s.String())
	scopes.CI.Infof("===============================")

	// Ensure that the work dir is set.
	if err := os.MkdirAll(s.RunDir(), os.ModePerm); err != nil {
		return fmt.Errorf("error creating rundir %q: %v", s.RunDir(), err)
	}
	scopes.Framework.Infof("Test run dir: %v", s.RunDir())

	var err error
	rt, err = runtime.New(s, newEnvironment)
	return err
}

func doTestSetup(fns ...SetupFn) error {
	scopes.CI.Infof("=== BEGIN: Setup: '%s' ===", rt.SuiteContext().Settings().TestID)
	for _, fn := range fns {
		err := fn(rt.SuiteContext())
		if err != nil {
			scopes.Framework.Errorf("Test setup error: %v", err)
			scopes.CI.Infof("=== FAILED: test setup: '%s' ===", rt.SuiteContext().Settings().TestID)
			return err
		}
	}

	scopes.CI.Infof("=== DONE: Setup: '%s' ===", rt.SuiteContext().Settings().TestID)
	return nil
}

func doRun(mRun mRunFn) int {
	scopes.CI.Infof("=== BEGIN: Test Run: '%s' ===", rt.SuiteContext().Settings().TestID)
	errLevel := mRun()
	if errLevel == 0 {
		scopes.CI.Infof("=== DONE: Test Run: '%s' ===", rt.SuiteContext().Settings().TestID)
	} else {
		scopes.CI.Infof("=== FAILED: Test Run: '%s' (exitCode: %v) ===",
			rt.SuiteContext().Settings().TestID, errLevel)
	}

	return errLevel
}

func doCleanup() {
	if rt == nil {
		return
	}

	if err := rt.Close(); err != nil {
		scopes.Framework.Errorf("Error during close: %v", err)
	}
	rt = nil
}

func newEnvironment(name string, ctx api.Context) (resource.Environment, error) {
	switch name {
	case environment.Native.String():
		return native.New(ctx)
	case environment.Kube.String():
		return kube.New(ctx)
	default:
		return nil, fmt.Errorf("unknown environment: %q", name)
	}
}
