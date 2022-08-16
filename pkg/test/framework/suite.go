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
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	goruntime "runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hashicorp/go-multierror"
	"gopkg.in/yaml.v2"

	kubelib "istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/test/echo"
	"istio.io/istio/pkg/test/framework/components/cluster"
	"istio.io/istio/pkg/test/framework/components/environment/kube"
	"istio.io/istio/pkg/test/framework/config"
	ferrors "istio.io/istio/pkg/test/framework/errors"
	"istio.io/istio/pkg/test/framework/label"
	"istio.io/istio/pkg/test/framework/resource"
	"istio.io/istio/pkg/test/prow"
	"istio.io/istio/pkg/test/scopes"
	"istio.io/istio/pkg/test/util/file"
	"istio.io/pkg/log"
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

	// Well-known paths which are stripped when generating test IDs.
	// Note: Order matters! Always specify the most specific directory first.
	wellKnownPaths = mustCompileAll(
		// This allows us to trim test IDs on the istio.io/istio.io repo.
		".*/istio.io/istio.io/",

		// These allow us to trim test IDs on istio.io/istio repo.
		".*/istio.io/istio/tests/integration/",
		".*/istio.io/istio/",

		// These are also used for istio.io/istio, but make help to satisfy
		// the feature label enforcement when running with BUILD_WITH_CONTAINER=1.
		"^/work/tests/integration/",
		"^/work/",

		// Outside of standard Istio  GOPATH
		".*/istio/tests/integration/",
	)
)

// getSettingsFunc is a function used to extract the default settings for the Suite.
type getSettingsFunc func(string) (*resource.Settings, error)

// mRunFn abstracts testing.M.run, so that the framework itself can be tested.
type mRunFn func(ctx *suiteContext) int

// Suite allows the test author to specify suite-related metadata and do setup in a fluent-style, before commencing execution.
type Suite interface {
	// EnvironmentFactory sets a custom function used for creating the resource.Environment for this Suite.
	EnvironmentFactory(fn resource.EnvironmentFactory) Suite
	// Label all the tests in suite with the given labels
	Label(labels ...label.Instance) Suite
	// SkipIf skips the suite if the function returns true
	SkipIf(reason string, fn resource.ShouldSkipFn) Suite
	// Skip marks a suite as skipped with the given reason. This will prevent any setup functions from occurring.
	Skip(reason string) Suite
	// RequireMinClusters ensures that the current environment contains at least the given number of clusters.
	// Otherwise it stops test execution.
	//
	// Deprecated: Tests should not make assumptions about number of clusters.
	RequireMinClusters(minClusters int) Suite
	// RequireMaxClusters ensures that the current environment contains at least the given number of clusters.
	// Otherwise it stops test execution.
	//
	// Deprecated: Tests should not make assumptions about number of clusters.
	RequireMaxClusters(maxClusters int) Suite
	// RequireSingleCluster is a utility method that requires that there be exactly 1 cluster in the environment.
	//
	// Deprecated: All new tests should support multiple clusters.
	RequireSingleCluster() Suite
	// RequireMultiPrimary ensures that each cluster is running a control plane.
	//
	// Deprecated: All new tests should work for any control plane topology.
	RequireMultiPrimary() Suite
	// SkipExternalControlPlaneTopology skips the tests in external plane and config cluster topology
	SkipExternalControlPlaneTopology() Suite
	// RequireExternalControlPlaneTopology requires the environment to be external control plane topology
	RequireExternalControlPlaneTopology() Suite
	// RequireMinVersion validates the environment meets a minimum version
	RequireMinVersion(minorVersion uint) Suite
	// RequireMaxVersion validates the environment meets a maximum version
	RequireMaxVersion(minorVersion uint) Suite
	// Setup runs enqueues the given setup function to run before test execution.
	Setup(fn resource.SetupFn) Suite
	// SetupParallel runs the given setup functions in parallel before test execution.
	SetupParallel(fns ...resource.SetupFn) Suite
	// Run the suite. This method calls os.Exit and does not return.
	Run()
}

// suiteImpl will actually run the test suite
type suiteImpl struct {
	testID      string
	skipMessage string
	skipFn      resource.ShouldSkipFn
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
	// We will trim out paths preceding some well known paths. This should handle anything in istio or docs repo,
	// as well as special case tests/integration. The end result is a test under ./tests/integration/pilot/ingress
	// will become pilot_ingress
	// Note: if this fails to trim, we end up with "ugly" suite names but otherwise no real impact.
	for _, wellKnownPath := range wellKnownPaths {
		// Try removing this path from the directory name.
		result := wellKnownPath.ReplaceAllString(d, "")
		if len(result) < len(d) {
			// Successfully found and removed this path from the directory.
			d = result
			break
		}
	}
	return strings.ReplaceAll(d, "/", "_")
}

// NewSuite returns a new suite instance.
func NewSuite(m *testing.M) Suite {
	_, f, _, _ := goruntime.Caller(1)
	suiteName := deriveSuiteName(f)

	if analyze() {
		return newSuiteAnalyzer(
			suiteName,
			func(_ *suiteContext) int {
				return m.Run()
			},
			os.Exit)
	}

	return newSuite(suiteName,
		func(_ *suiteContext) int {
			return m.Run()
		},
		os.Exit,
		getSettings)
}

func newSuite(testID string, fn mRunFn, osExit func(int), getSettingsFn getSettingsFunc) *suiteImpl {
	s := &suiteImpl{
		testID:      testID,
		mRun:        fn,
		osExit:      osExit,
		getSettings: getSettingsFn,
		labels:      label.NewSet(),
	}

	return s
}

func (s *suiteImpl) EnvironmentFactory(fn resource.EnvironmentFactory) Suite {
	if fn != nil && s.envFactory != nil {
		scopes.Framework.Warn("EnvironmentFactory overridden multiple times for Suite")
	}
	s.envFactory = fn
	return s
}

func (s *suiteImpl) Label(labels ...label.Instance) Suite {
	s.labels = s.labels.Add(labels...)
	return s
}

func (s *suiteImpl) Skip(reason string) Suite {
	s.skipMessage = reason
	s.skipFn = func(ctx resource.Context) bool {
		return true
	}
	return s
}

func (s *suiteImpl) SkipIf(reason string, fn resource.ShouldSkipFn) Suite {
	s.skipMessage = reason
	s.skipFn = fn
	return s
}

func (s *suiteImpl) RequireMinClusters(minClusters int) Suite {
	if minClusters <= 0 {
		minClusters = 1
	}

	fn := func(ctx resource.Context) error {
		if len(clusters(ctx)) < minClusters {
			s.Skip(fmt.Sprintf("Number of clusters %d does not exceed minimum %d",
				len(clusters(ctx)), minClusters))
		}
		return nil
	}

	s.requireFns = append(s.requireFns, fn)
	return s
}

func (s *suiteImpl) RequireMaxClusters(maxClusters int) Suite {
	if maxClusters <= 0 {
		maxClusters = 1
	}

	fn := func(ctx resource.Context) error {
		if len(clusters(ctx)) > maxClusters {
			s.Skip(fmt.Sprintf("Number of clusters %d exceeds maximum %d",
				len(clusters(ctx)), maxClusters))
		}
		return nil
	}

	s.requireFns = append(s.requireFns, fn)
	return s
}

func (s *suiteImpl) RequireSingleCluster() Suite {
	// nolint: staticcheck
	return s.RequireMinClusters(1).RequireMaxClusters(1)
}

func (s *suiteImpl) RequireMultiPrimary() Suite {
	fn := func(ctx resource.Context) error {
		for _, c := range ctx.Clusters() {
			if !c.IsPrimary() {
				s.Skip(fmt.Sprintf("Cluster %s is not using a local control plane",
					c.Name()))
			}
		}
		return nil
	}

	s.requireFns = append(s.requireFns, fn)
	return s
}

func (s *suiteImpl) SkipExternalControlPlaneTopology() Suite {
	fn := func(ctx resource.Context) error {
		for _, c := range ctx.Clusters() {
			if c.IsConfig() && !c.IsPrimary() {
				s.Skip(fmt.Sprintf("Cluster %s is a config cluster, we can't run external control plane topology",
					c.Name()))
			}
		}
		return nil
	}
	s.requireFns = append(s.requireFns, fn)
	return s
}

func (s *suiteImpl) RequireExternalControlPlaneTopology() Suite {
	fn := func(ctx resource.Context) error {
		for _, c := range ctx.Clusters() {
			if c.IsConfig() && !c.IsPrimary() {
				// the test environment is an external control plane topology, the test can go on
				return nil
			}
		}
		// the test environment is not an external control plane topology, skip the test
		s.Skip("Not an external control plane topology, skip this test")
		return nil
	}
	s.requireFns = append(s.requireFns, fn)
	return s
}

func (s *suiteImpl) RequireMinVersion(minorVersion uint) Suite {
	fn := func(ctx resource.Context) error {
		for _, c := range ctx.Clusters().Kube() {
			ver, err := c.GetKubernetesVersion()
			if err != nil {
				return fmt.Errorf("failed to get Kubernetes version: %v", err)
			}
			if !kubelib.IsAtLeastVersion(c, minorVersion) {
				s.Skip(fmt.Sprintf("Required Kubernetes version (1.%v) is greater than current: %v",
					minorVersion, ver.String()))
			}
		}
		return nil
	}

	s.requireFns = append(s.requireFns, fn)
	return s
}

func (s *suiteImpl) RequireMaxVersion(minorVersion uint) Suite {
	fn := func(ctx resource.Context) error {
		for _, c := range ctx.Clusters().Kube() {
			ver, err := c.GetKubernetesVersion()
			if err != nil {
				return fmt.Errorf("failed to get Kubernetes version: %v", err)
			}
			if !kubelib.IsLessThanVersion(c, minorVersion+1) {
				s.Skip(fmt.Sprintf("Maximum Kubernetes version (1.%v) is less than current: %v",
					minorVersion, ver.String()))
			}
		}
		return nil
	}

	s.requireFns = append(s.requireFns, fn)
	return s
}

func (s *suiteImpl) Setup(fn resource.SetupFn) Suite {
	s.setupFns = append(s.setupFns, fn)
	return s
}

func (s *suiteImpl) SetupParallel(fns ...resource.SetupFn) Suite {
	s.setupFns = append(s.setupFns, func(ctx resource.Context) error {
		g := multierror.Group{}
		for _, fn := range fns {
			fn := fn
			g.Go(func() error {
				return fn(ctx)
			})
		}
		return g.Wait().ErrorOrNil()
	})
	return s
}

func (s *suiteImpl) runSetupFn(fn resource.SetupFn, ctx SuiteContext) (err error) {
	defer func() {
		// Dump if the setup function fails
		if err != nil && ctx.Settings().CIMode {
			rt.Dump(ctx)
		}
	}()
	err = fn(ctx)
	return
}

func (s *suiteImpl) Run() {
	s.osExit(s.run())
}

func (s *suiteImpl) isSkipped(ctx SuiteContext) bool {
	if s.skipFn != nil && s.skipFn(ctx) {
		return true
	}
	return false
}

func (s *suiteImpl) doSkip(ctx *suiteContext) int {
	scopes.Framework.Infof("Skipping suite %q: %s", ctx.Settings().TestID, s.skipMessage)

	// Mark this suite as skipped in the context.
	ctx.skipped = true

	// Run the tests so that the golang test framework exits normally. The tests will not run because
	// they see that this suite has been skipped.
	_ = s.mRun(ctx)

	// Return success.
	return 0
}

func (s *suiteImpl) run() (errLevel int) {
	if err := initRuntime(s); err != nil {
		scopes.Framework.Errorf("Error during test framework init: %v", err)
		return exitCodeInitError
	}

	ctx := rt.suiteContext()
	// Skip the test if its explicitly skipped
	if s.isSkipped(ctx) {
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
			rt.Dump(ctx)
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
	if s.isSkipped(ctx) {
		return s.doSkip(ctx)
	}

	defer func() {
		end := time.Now()
		scopes.Framework.Infof("=== Suite %q run time: %v ===", ctx.Settings().TestID, end.Sub(start))

		ctx.RecordTraceEvent("suite-runtime", end.Sub(start).Seconds())
		ctx.RecordTraceEvent("echo-calls", echo.GlobalEchoRequests.Load())
		ctx.RecordTraceEvent("yaml-apply", GlobalYAMLWrites.Load())
		traceFile := filepath.Join(ctx.Settings().BaseDir, "trace.yaml")
		scopes.Framework.Infof("Wrote trace to %v", prow.ArtifactsURL(traceFile))
		_ = appendToFile(ctx.marshalTraceEvent(), traceFile)
	}()

	attempt := 0
	for attempt <= ctx.settings.Retries {
		attempt++
		scopes.Framework.Infof("=== BEGIN: Test Run: '%s' ===", ctx.Settings().TestID)
		errLevel = s.mRun(ctx)
		if errLevel == 0 {
			scopes.Framework.Infof("=== DONE: Test Run: '%s' ===", ctx.Settings().TestID)
			break
		}
		scopes.Framework.Infof("=== FAILED: Test Run: '%s' (exitCode: %v) ===",
			ctx.Settings().TestID, errLevel)
		if attempt <= ctx.settings.Retries {
			scopes.Framework.Warnf("=== RETRY: Test Run: '%s' ===", ctx.Settings().TestID)
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

func environmentName(ctx resource.Context) string {
	if ctx.Environment() != nil {
		return ctx.Environment().EnvironmentName()
	}
	return ""
}

func isMulticluster(ctx resource.Context) bool {
	if ctx.Environment() != nil {
		return ctx.Clusters().IsMulticluster()
	}
	return false
}

func clusters(ctx resource.Context) []cluster.Cluster {
	if ctx.Environment() != nil {
		return ctx.Environment().Clusters()
	}
	return nil
}

func (s *suiteImpl) writeOutput() {
	// the ARTIFACTS env var is set by prow, and uploaded to GCS as part of the job artifact
	artifactsPath, err := file.NormalizePath(os.Getenv("ARTIFACTS"))
	if err != nil {
		artifactsPath = os.Getenv("ARTIFACTS")
		log.Warnf("failed normalizing %s: %v", artifactsPath, err)
	}
	if artifactsPath != "" {
		ctx := rt.suiteContext()
		ctx.outcomeMu.RLock()
		out := SuiteOutcome{
			Name:         ctx.Settings().TestID,
			Environment:  environmentName(ctx),
			Multicluster: isMulticluster(ctx),
			TestOutcomes: ctx.testOutcomes,
		}
		ctx.outcomeMu.RUnlock()
		outbytes, err := yaml.Marshal(out)
		if err != nil {
			log.Errorf("failed writing test suite outcome to yaml: %s", err)
		}
		err = os.WriteFile(path.Join(artifactsPath, out.Name+".yaml"), outbytes, 0o644)
		if err != nil {
			log.Errorf("failed writing test suite outcome to file: %s", err)
		}
	}
}

func (s *suiteImpl) runSetupFns(ctx SuiteContext) (err error) {
	scopes.Framework.Infof("=== BEGIN: Setup: '%s' ===", ctx.Settings().TestID)

	// Run all the require functions first, then the setup functions.
	setupFns := append(append([]resource.SetupFn{}, s.requireFns...), s.setupFns...)

	start := time.Now()
	for _, fn := range setupFns {
		err := s.runSetupFn(fn, ctx)
		if err != nil {
			scopes.Framework.Errorf("Test setup error: %v", err)
			scopes.Framework.Infof("=== FAILED: Setup: '%s' (%v) ===", ctx.Settings().TestID, err)
			return err
		}

		if s.isSkipped(ctx) {
			return nil
		}
	}
	elapsed := time.Since(start)
	scopes.Framework.Infof("=== DONE: Setup: '%s' (%v) ===", ctx.Settings().TestID, elapsed)
	return nil
}

func initRuntime(s *suiteImpl) error {
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

	if err := configureLogging(); err != nil {
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

func newEnvironment(ctx resource.Context) (resource.Environment, error) {
	s, err := kube.NewSettingsFromCommandLine()
	if err != nil {
		return nil, err
	}
	return kube.New(ctx, s)
}

func getSettings(testID string) (*resource.Settings, error) {
	// Parse flags and init logging.
	if !config.Parsed() {
		config.Parse()
	}

	return resource.SettingsFromCommandLine(testID)
}

func mustCompileAll(patterns ...string) []*regexp.Regexp {
	out := make([]*regexp.Regexp, 0, len(patterns))
	for _, pattern := range patterns {
		out = append(out, regexp.MustCompile(pattern))
	}

	return out
}

func appendToFile(contents []byte, file string) error {
	f, err := os.OpenFile(file, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0o600)
	if err != nil {
		return err
	}

	defer func() {
		_ = f.Close()
	}()

	if _, err = f.Write(contents); err != nil {
		return err
	}
	return nil
}
