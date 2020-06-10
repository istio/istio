// Copyright Istio Authors
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
	"io/ioutil"
	"os"
	"path"
	"path/filepath"
	goruntime "runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"gopkg.in/yaml.v2"

	"istio.io/pkg/log"

	"istio.io/istio/pkg/test/framework/components/environment/kube"
	ferrors "istio.io/istio/pkg/test/framework/errors"
	"istio.io/istio/pkg/test/framework/label"
	"istio.io/istio/pkg/test/framework/resource"
	"istio.io/istio/pkg/test/framework/resource/environment"
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
	rt   *runtime
	rtMu sync.Mutex
)

// getSettingsFunc is a function used to extract the default settings for the Suite.
type getSettingsFunc func(string) (*resource.Settings, error)

// mRunFn abstracts testing.M.run, so that the framework itself can be tested.
type mRunFn func(ctx *suiteContext) int

// Suite allows the test author to specify suite-related metadata and do setup in a fluent-style, before commencing execution.
type Suite struct {
	testID      string
	skipMessage string
	mRun        mRunFn
	osExit      func(int)
	labels      label.Set

	requireFns []resource.SetupFn
	setupFns   []resource.SetupFn

	getSettings getSettingsFunc
	envFactory  resource.EnvironmentFactory
}

// Given the filename of a test, derive its suite name
func deriveSuiteName(caller string) string {
	d := filepath.Dir(caller)
	matches := []string{"istio.io/istio.io", "istio.io/istio", "tests/integration"}
	// We will trim out paths preceding some well known paths. This should handle anything in istio or docs repo,
	// as well as special case tests/integration. The end result is a test under ./tests/integration/pilot/ingress
	// will become pilot_ingress
	// Note: if this fails to trim, we end up with "ugly" suite names but otherwise no real impact.
	for _, match := range matches {
		if i := strings.Index(d, match); i >= 0 {
			d = d[i+len(match)+1:]
		}
	}
	return strings.ReplaceAll(d, "/", "_")
}

// NewSuite returns a new suite instance.
func NewSuite(testID string, m *testing.M) *Suite {
	_, f, _, _ := goruntime.Caller(1)
	return newSuite(deriveSuiteName(f),
		func(_ *suiteContext) int {
			return m.Run()
		},
		os.Exit,
		getSettings)
}

func newSuite(testID string, fn mRunFn, osExit func(int), getSettingsFn getSettingsFunc) *Suite {
	s := &Suite{
		testID:      testID,
		mRun:        fn,
		osExit:      osExit,
		getSettings: getSettingsFn,
		labels:      label.NewSet(),
	}

	return s
}

// Label all the tests in suite with the given labels
func (s *Suite) Label(labels ...label.Instance) *Suite {
	s.labels = s.labels.Add(labels...)
	return s
}

// Skip marks a suite as skipped with the given reason. This will prevent any setup functions from occurring.
func (s *Suite) Skip(reason string) *Suite {
	s.skipMessage = reason
	return s
}

// RequireMinClusters ensures that the current environment contains at least the given number of clusters.
// Otherwise it stops test execution.
func (s *Suite) RequireMinClusters(minClusters int) *Suite {
	if minClusters <= 0 {
		minClusters = 1
	}

	fn := func(ctx resource.Context) error {
		if len(ctx.Environment().Clusters()) < minClusters {
			s.Skip(fmt.Sprintf("Number of clusters %d does not exceed minimum %d",
				len(ctx.Environment().Clusters()), minClusters))
		}
		return nil
	}

	s.requireFns = append(s.requireFns, fn)
	return s
}

// RequireMaxClusters ensures that the current environment contains at least the given number of clusters.
// Otherwise it stops test execution.
func (s *Suite) RequireMaxClusters(maxClusters int) *Suite {
	if maxClusters <= 0 {
		maxClusters = 1
	}

	fn := func(ctx resource.Context) error {
		if len(ctx.Environment().Clusters()) > maxClusters {
			s.Skip(fmt.Sprintf("Number of clusters %d exceeds maximum %d",
				len(ctx.Environment().Clusters()), maxClusters))
		}
		return nil
	}

	s.requireFns = append(s.requireFns, fn)
	return s
}

// RequireSingleCluster is a utility method that requires that there be exactly 1 cluster in the environment.
func (s *Suite) RequireSingleCluster() *Suite {
	return s.RequireMinClusters(1).RequireMaxClusters(1)
}

// RequireEnvironmentVersion validates the environment meets a minimum version
func (s *Suite) RequireEnvironmentVersion(version string) *Suite {
	fn := func(ctx resource.Context) error {

		if ctx.Environment().EnvironmentName() == environment.Kube {
			kenv := ctx.Environment().(*kube.Environment)
			ver, err := kenv.KubeClusters[0].GetKubernetesVersion()
			if err != nil {
				return fmt.Errorf("failed to get Kubernetes version: %v", err)
			}
			serverVersion := fmt.Sprintf("%s.%s", ver.Major, ver.Minor)
			if serverVersion < version {
				s.Skip(fmt.Sprintf("Required Kubernetes version (%v) is greater than current: %v",
					version, serverVersion))
			}
		}
		return nil
	}

	s.requireFns = append(s.requireFns, fn)
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

// Run the suite. This method calls os.Exit and does not return.
func (s *Suite) Run() {
	s.osExit(s.run())
}

func (s *Suite) isSkipped() bool {
	return s.skipMessage != ""
}

func (s *Suite) doSkip(ctx *suiteContext) int {
	scopes.Framework.Infof("Skipping suite %q: %s", ctx.Settings().TestID, s.skipMessage)

	// Mark this suite as skipped in the context.
	ctx.skipped = true

	// Run the tests so that the golang test framework exits normally. The tests will not run because
	// they see that this suite has been skipped.
	_ = s.mRun(ctx)

	// Return success.
	return 0
}

func (s *Suite) run() (errLevel int) {
	if err := initRuntime(s); err != nil {
		scopes.Framework.Errorf("Error during test framework init: %v", err)
		return exitCodeInitError
	}

	ctx := rt.suiteContext()

	// Skip the test if its explicitly skipped
	if s.isSkipped() {
		return s.doSkip(ctx)
	}

	// Before starting, check whether the current set of labels & label selectors will ever allow us to run tests.
	// if not, simply exit now.
	if ctx.Settings().Selector.Excludes(s.labels) {
		s.Skip(fmt.Sprintf("Label mismatch: labels=%v, selector=%v",
			s.labels,
			ctx.Settings().Selector))
		return s.doSkip(ctx)
	}

	start := time.Now()

	defer func() {
		if errLevel != 0 && ctx.Settings().CIMode {
			rt.Dump()
		}

		if err := rt.Close(); err != nil {
			scopes.Framework.Errorf("Error during close: %v", err)
			if rt.context.settings.FailOnDeprecation {
				if ferrors.IsOrContainsDeprecatedError(err) {
					errLevel = 1
				}
			}
		}
		rt = nil
	}()

	if err := s.runSetupFns(ctx); err != nil {
		scopes.Framework.Errorf("Exiting due to setup failure: %v", err)
		return exitCodeSetupError
	}

	// Check if one of the setup functions ended up skipping the suite.
	if s.isSkipped() {
		return s.doSkip(ctx)
	}

	defer func() {
		end := time.Now()
		scopes.Framework.Infof("=== Suite %q run time: %v ===", ctx.Settings().TestID, end.Sub(start))
	}()

	attempt := 0
	for attempt <= ctx.settings.Retries {
		attempt++
		scopes.Framework.Infof("=== BEGIN: Test Run: '%s' ===", ctx.Settings().TestID)
		errLevel = s.mRun(ctx)
		if errLevel == 0 {
			scopes.Framework.Infof("=== DONE: Test Run: '%s' ===", ctx.Settings().TestID)
			break
		} else {
			scopes.Framework.Infof("=== FAILED: Test Run: '%s' (exitCode: %v) ===",
				ctx.Settings().TestID, errLevel)
			if attempt <= ctx.settings.Retries {
				scopes.Framework.Warnf("=== RETRY: Test Run: '%s' ===", ctx.Settings().TestID)
			}
		}
	}
	s.writeOutput()

	return
}

type SuiteOutcome struct {
	Name         string
	Environment  string
	Multicluster bool
	TestOutcomes []TestOutcome
}

func (s *Suite) writeOutput() {
	// the ARTIFACTS env var is set by prow, and uploaded to GCS as part of the job artifact
	artifactsPath := os.Getenv("ARTIFACTS")
	if artifactsPath != "" {
		ctx := rt.suiteContext()
		ctx.outcomeMu.RLock()
		out := SuiteOutcome{
			Name:         ctx.Settings().TestID,
			Environment:  ctx.Environment().EnvironmentName().String(),
			Multicluster: ctx.Environment().IsMulticluster(),
			TestOutcomes: ctx.testOutcomes,
		}
		ctx.outcomeMu.RUnlock()
		outbytes, err := yaml.Marshal(out)
		if err != nil {
			log.Errorf("failed writing test suite outcome to yaml: %s", err)
		}
		err = ioutil.WriteFile(path.Join(artifactsPath, out.Name+".yaml"), outbytes, 0644)
		if err != nil {
			log.Errorf("failed writing test suite outcome to file: %s", err)
		}
	}
}

func (s *Suite) runSetupFns(ctx SuiteContext) (err error) {
	scopes.Framework.Infof("=== BEGIN: Setup: '%s' ===", ctx.Settings().TestID)

	// Run all the require functions first, then the setup functions.
	setupFns := append(append([]resource.SetupFn{}, s.requireFns...), s.setupFns...)

	for _, fn := range setupFns {
		err := s.runSetupFn(fn, ctx)
		if err != nil {
			scopes.Framework.Errorf("Test setup error: %v", err)
			scopes.Framework.Infof("=== FAILED: Setup: '%s' (%v) ===", ctx.Settings().TestID, err)
			return err
		}

		if s.isSkipped() {
			return nil
		}
	}
	scopes.Framework.Infof("=== DONE: Setup: '%s' ===", ctx.Settings().TestID)
	return nil
}

func initRuntime(s *Suite) error {
	rtMu.Lock()
	defer rtMu.Unlock()

	if rt != nil {
		return errors.New("framework is already initialized")
	}

	settings, err := s.getSettings(s.testID)
	if err != nil {
		return err
	}

	// Get the EnvironmentFactory.
	environmentFactory := s.envFactory
	if environmentFactory == nil {
		environmentFactory = settings.EnvironmentFactory
	}
	if environmentFactory == nil {
		environmentFactory = newEnvironment
	}

	if err := configureLogging(settings.CIMode); err != nil {
		return err
	}

	scopes.Framework.Infof("=== Test Framework Settings ===")
	scopes.Framework.Info(settings.String())
	scopes.Framework.Infof("===============================")

	// Ensure that the work dir is set.
	if err := os.MkdirAll(settings.RunDir(), os.ModePerm); err != nil {
		return fmt.Errorf("error creating rundir %q: %v", settings.RunDir(), err)
	}
	scopes.Framework.Infof("Test run dir: %v", settings.RunDir())

	rt, err = newRuntime(settings, environmentFactory, s.labels)
	return err
}

func newEnvironment(name string, ctx resource.Context) (resource.Environment, error) {
	switch name {
	case environment.Kube.String():
		s, err := kube.NewSettingsFromCommandLine()
		if err != nil {
			return nil, err
		}
		return kube.New(ctx, s)
	default:
		return nil, fmt.Errorf("unknown environment: %q", name)
	}
}

func getSettings(testID string) (*resource.Settings, error) {
	// Parse flags and init logging.
	if !flag.Parsed() {
		flag.Parse()
	}

	return resource.SettingsFromCommandLine(testID)
}
