// Copyright 2019 Istio Authors
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

package framework

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"istio.io/istio/pkg/test/framework/components/environment"
	"istio.io/istio/pkg/test/framework/components/environment/api"
	"istio.io/istio/pkg/test/framework/components/environment/kube"
	"istio.io/istio/pkg/test/framework/components/environment/native"
	"istio.io/istio/pkg/test/framework/core"
	"istio.io/istio/pkg/test/framework/label"
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
	rt   *runtime.Instance
	rtMu sync.Mutex
)

// mRunFn abstracts testing.M.run, so that the framework itself can be tested.
type mRunFn func() int

// Suite allows the test author to specify suite-related metadata and do setup in a fluent-style, before commencing execution.
type Suite struct {
	testID string
	mRun   mRunFn
	osExit func(int)
	labels label.Set

	setupFns []resource.SetupFn

	getSettingsFn func(string) (*core.Settings, error)
}

// NewSuite returns a new suite instance.
func NewSuite(testID string, m *testing.M) *Suite {
	return newSuite(testID, m.Run, os.Exit, getSettings)
}

func newSuite(testID string, fn mRunFn, osExit func(int), getSettingsFn func(string) (*core.Settings, error)) *Suite {
	s := &Suite{
		testID:        testID,
		mRun:          fn,
		osExit:        osExit,
		getSettingsFn: getSettingsFn,
		labels:        label.NewSet(),
	}

	return s
}

// Label all the tests in suite with the given labels
func (s *Suite) Label(labels ...label.Instance) *Suite {
	s.labels = s.labels.Add(labels...)
	return s
}

// RequireEnvironment ensures that the current environment matches what the suite expects. Otherwise it
// stops test execution. This also applies the appropriate label to the suite implicitly.
func (s *Suite) RequireEnvironment(name environment.Name) *Suite {
	setupFn := func(ctx resource.Context) error {
		if name != ctx.Environment().EnvironmentName() {
			scopes.Framework.Infof("Skipping suite %q: Required environment (%v) does not match current: %v",
				ctx.Settings().TestID, name, ctx.Environment().EnvironmentName())
			s.osExit(0)

			// Adding this for testing purposes.
			return fmt.Errorf("failed setup: Required environment not found")
		}
		return nil
	}

	// Prepend the function, so that it runs as the first thing.
	fns := []resource.SetupFn{setupFn}
	fns = append(fns, s.setupFns...)
	s.setupFns = fns
	return s
}

// Setup runs enqueues the given setup function to run before test execution.
func (s *Suite) Setup(fn resource.SetupFn) *Suite {
	s.setupFns = append(s.setupFns, fn)
	return s
}

func (s *Suite) runSetupFn(fn resource.SetupFn, ctx SuiteContext) (err error) {
	defer func() {
		// Dump if the setup function fails
		if err != nil && ctx.Settings().CIMode {
			rt.Dump()
		}
	}()
	err = fn(ctx)
	return
}

// SetupOnEnv runs the given setup function conditionally, based on the current environment.
func (s *Suite) SetupOnEnv(e environment.Name, fn resource.SetupFn) *Suite {
	s.Setup(func(ctx resource.Context) error {
		if ctx.Environment().EnvironmentName() != e {
			return nil
		}
		return fn(ctx)
	})

	return s
}

// Run the suite. This method calls os.Exit and does not return.
func (s *Suite) Run() {
	s.osExit(s.run())
}

func (s *Suite) run() (errLevel int) {
	if err := initRuntime(s.testID, s.labels, s.getSettingsFn); err != nil {
		scopes.Framework.Errorf("Error during test framework init: %v", err)
		return exitCodeInitError
	}

	ctx := rt.SuiteContext()

	// Before starting, check whether the current set of labels & label selectors will ever allow us to run tests.
	// if not, simply exit now.
	if ctx.Settings().Selector.Excludes(s.labels) {
		scopes.Framework.Infof("Skipping suite %q due to label mismatch: labels=%v, selector=%v",
			ctx.Settings().TestID,
			s.labels,
			ctx.Settings().Selector)
		return 0
	}

	start := time.Now()

	defer func() {
		if errLevel != 0 && ctx.Settings().CIMode {
			rt.Dump()
		}

		if err := rt.Close(); err != nil {
			scopes.Framework.Errorf("Error during close: %v", err)
		}
		rt = nil
	}()

	if err := s.runSetupFns(ctx); err != nil {
		scopes.Framework.Errorf("Exiting due to setup failure: %v", err)
		return exitCodeSetupError
	}

	defer func() {
		end := time.Now()
		scopes.CI.Infof("=== Suite %q run time: %v ===", ctx.Settings().TestID, end.Sub(start))
	}()

	scopes.CI.Infof("=== BEGIN: Test Run: '%s' ===", ctx.Settings().TestID)
	errLevel = s.mRun()
	if errLevel == 0 {
		scopes.CI.Infof("=== DONE: Test Run: '%s' ===", ctx.Settings().TestID)
	} else {
		scopes.CI.Infof("=== FAILED: Test Run: '%s' (exitCode: %v) ===",
			ctx.Settings().TestID, errLevel)
	}

	return
}

func (s *Suite) runSetupFns(ctx SuiteContext) (err error) {
	scopes.CI.Infof("=== BEGIN: Setup: '%s' ===", ctx.Settings().TestID)

	for _, fn := range s.setupFns {
		err := s.runSetupFn(fn, ctx)
		if err != nil {
			scopes.Framework.Errorf("Test setup error: %v", err)
			scopes.CI.Infof("=== FAILED: Setup: '%s' (%v) ===", ctx.Settings().TestID, err)
			return err
		}
	}
	scopes.CI.Infof("=== DONE: Setup: '%s' ===", ctx.Settings().TestID)
	return nil
}

func initRuntime(testID string, labels label.Set, getSettingsFn func(string) (*core.Settings, error)) error {
	rtMu.Lock()
	defer rtMu.Unlock()

	if rt != nil {
		return errors.New("framework is already initialized")
	}

	s, err := getSettingsFn(testID)
	if err != nil {
		return err
	}

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

	rt, err = runtime.New(s, newEnvironment, labels)
	return err
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

func getSettings(testID string) (*core.Settings, error) {
	// Parse flags and init logging.
	if !flag.Parsed() {
		flag.Parse()
	}

	return core.SettingsFromCommandLine(testID)
}
