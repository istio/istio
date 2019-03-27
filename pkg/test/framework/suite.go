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

// SetupFn is a function used for performing suite-level setup actions.
type SetupFn func(ctx SuiteContext) error

// mRunFn abstracts testing.M.run, so that the framework itself can be tested.
type mRunFn func() int

// Suite allows the test author to specify suite-related metadata and do setup in a fluent-style, before commencing execution.
type Suite struct {
	mRun   mRunFn
	osExit func(int)
	labels []label.Instance
	ctx    SuiteContext
}

// NewSuite returns a new suite instance.
func NewSuite(testID string, m *testing.M) *Suite {
	return newSuite(testID, m.Run, os.Exit)
}

func newSuite(testID string, fn mRunFn, osExit func(int)) *Suite {
	s := &Suite{
		mRun:   fn,
		osExit: osExit,
	}

	if err := initRuntime(testID); err != nil {
		scopes.Framework.Errorf("Error during test framework init: %v", err)
		osExit(exitCodeInitError)
	}

	s.ctx = rt.SuiteContext()
	return s
}

// Label all the tests in suite with the given labels
func (s *Suite) Label(labels ...label.Instance) *Suite {
	s.labels = append(s.labels, labels...)
	return s
}

// RequireEnvironment ensures that the current environment matches what the suite expects. Otherwise it
// stops test execution. This also applies the appropriate label to the suite implicitly.
func (s *Suite) RequireEnvironment(name environment.Name) *Suite {
	if name != s.ctx.Environment().EnvironmentName() {
		scopes.Framework.Infof("Skipping suite %q: expected environment not found: %v", s.ctx.Settings().TestID, name)
		s.osExit(0)
	}

	// TODO: Move this to an appropriate abstraction.
	switch name {
	case environment.Native:
		s.Label(label.Native)
	case environment.Kube:
		s.Label(label.Kube)
	default:
		scopes.Framework.Infof("Unknown environment: %v", name)
	}

	return s
}

// Setup runs the given setup function. It calls os.Exit if it returns an error.
func (s *Suite) Setup(fn SetupFn) *Suite {
	scopes.CI.Infof("=== BEGIN: Setup: '%s' ===", rt.SuiteContext().Settings().TestID)

	err := s.setup(fn)
	if err != nil {
		scopes.Framework.Errorf("Test setup error: %v", err)
		scopes.CI.Infof("=== FAILED: test setup: '%s' ===", rt.SuiteContext().Settings().TestID)
		s.osExit(exitCodeSetupError)
		return s
	}

	scopes.CI.Infof("=== DONE: Setup: '%s' ===", rt.SuiteContext().Settings().TestID)
	return s
}

func (s *Suite) setup(fn SetupFn) (err error) {
	defer func() {
		// Dump if the setup function fails
		if err != nil && rt.SuiteContext().Settings().CIMode {
			rt.Dump()
		}
	}()
	err = fn(rt.SuiteContext())
	return
}

// EnvSetup runs the given setup function conditionally, based on the current environment.
func (s *Suite) EnvSetup(e environment.Name, fn SetupFn) *Suite {
	s.Setup(func(s SuiteContext) error {
		if s.Environment().EnvironmentName() != e {
			return nil
		}
		return fn(s)
	})

	return s
}

// Run the suite. This method calls os.Exit and does not return.
func (s *Suite) Run() {
	s.osExit(s.run())
}

func (s *Suite) run() (errLevel int) {
	start := time.Now()

	defer func() {
		if errLevel != 0 && rt.SuiteContext().Settings().CIMode {
			rt.Dump()
		}

		if err := rt.Close(); err != nil {
			scopes.Framework.Errorf("Error during close: %v", err)
		}
		rt = nil
	}()

	defer func() {
		end := time.Now()
		scopes.CI.Infof("=== Suite %q run time: %v ===", s.ctx.Settings().TestID, end.Sub(start))
	}()

	scopes.CI.Infof("=== BEGIN: Test Run: '%s' ===", rt.SuiteContext().Settings().TestID)
	errLevel = s.mRun()
	if errLevel == 0 {
		scopes.CI.Infof("=== DONE: Test Run: '%s' ===", rt.SuiteContext().Settings().TestID)
	} else {
		scopes.CI.Infof("=== FAILED: Test Run: '%s' (exitCode: %v) ===",
			rt.SuiteContext().Settings().TestID, errLevel)
	}

	return
}

func initRuntime(testID string) error {
	rtMu.Lock()
	defer rtMu.Unlock()

	if rt != nil {
		return errors.New("framework is already initialized")
	}

	// Parse flags and init logging.
	if !flag.Parsed() {
		flag.Parse()
	}

	s, err := core.SettingsFromCommandLine(testID)
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

	rt, err = runtime.New(s, newEnvironment)
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
