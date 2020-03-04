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
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path"
	"path/filepath"
	"testing"

	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/framework/components/environment"
	"istio.io/istio/pkg/test/framework/core"
	"istio.io/istio/pkg/test/framework/errors"
	"istio.io/istio/pkg/test/framework/label"
	"istio.io/istio/pkg/test/framework/resource"
	"istio.io/istio/pkg/test/scopes"
)

// TestContext is a test-level context that can be created as part of test executing tests.
type TestContext interface {
	resource.Context
	test.Failer

	// NewSubTest creates a new sub-test under the current running Test. The lifecycle of a sub-Test is scoped to the
	// parent. Calls to Done() will block until all children are also Done(). When Run, sub-Tests will automatically
	// create their own Golang *testing.T with the name provided.
	//
	// If this TestContext was not created by a Test or if that Test is not running, this method will panic.
	NewSubTest(name string) *Test

	// WorkDir allocated for this test.
	WorkDir() string

	// CreateDirectoryOrFail creates a new sub directory with the given name in the workdir, or fails the test.
	CreateDirectoryOrFail(name string) string

	// CreateTmpDirectoryOrFail creates a new temporary directory with the given prefix in the workdir, or fails the test.
	CreateTmpDirectoryOrFail(prefix string) string

	// RequireOrSkip skips the test if the environment is not as expected.
	RequireOrSkip(envName environment.Name)

	// WhenDone runs the given function when the test context completes.
	// This function may not (safely) access the test context.
	WhenDone(fn func() error)

	// Done should be called when this context is no longer needed. It triggers the asynchronous cleanup of any
	// allocated resources.
	Done()

	// Methods for interacting with the underlying *testing.T.
	Error(args ...interface{})
	Errorf(format string, args ...interface{})
	Failed() bool
	Log(args ...interface{})
	Logf(format string, args ...interface{})
	Name() string
	Skip(args ...interface{})
	SkipNow()
	Skipf(format string, args ...interface{})
	Skipped() bool
}

var _ TestContext = &testContext{}
var _ test.Failer = &testContext{}

// testContext for the currently executing test.
type testContext struct {
	// The id of the test context. useful for debugging the test framework itself.
	id string

	// The currently running Test. Non-nil if this context was created by a Test
	test *Test

	// The underlying Go testing.T for this context.
	*testing.T

	// suite-level context
	suite *suiteContext

	// resource scope for this context.
	scope *scope

	// The workDir for this particular context
	workDir string
}

func newTestContext(test *Test, goTest *testing.T, s *suiteContext, parentScope *scope, labels label.Set) *testContext {
	id := s.allocateContextID(goTest.Name())

	allLabels := s.suiteLabels.Merge(labels)
	if !s.settings.Selector.Selects(allLabels) {
		goTest.Skipf("Skipping: label mismatch: labels=%v, filter=%v", allLabels, s.settings.Selector)
	}

	scopes.Framework.Debugf("Creating New test context")
	workDir := path.Join(s.settings.RunDir(), goTest.Name())
	if err := os.MkdirAll(workDir, os.ModePerm); err != nil {
		goTest.Fatalf("Error creating work dir %q: %v", workDir, err)
	}

	scopes.Framework.Debugf("Creating new testContext: %q", id)

	if parentScope == nil {
		parentScope = s.globalScope
	}

	scopeID := fmt.Sprintf("[%s]", id)
	return &testContext{
		id:      id,
		test:    test,
		T:       goTest,
		suite:   s,
		scope:   newScope(scopeID, parentScope),
		workDir: workDir,
	}
}

func (c *testContext) Settings() *core.Settings {
	return c.suite.settings
}

func (c *testContext) TrackResource(r resource.Resource) resource.ID {
	id := c.suite.allocateResourceID(c.id, r)
	rid := &resourceID{id: id}
	c.scope.add(r, rid)
	return rid
}

func (c *testContext) WorkDir() string {
	return c.workDir
}

func (c *testContext) Environment() resource.Environment {
	return c.suite.environment
}

func (c *testContext) CreateDirectory(name string) (string, error) {
	dir := filepath.Join(c.workDir, name)
	err := os.Mkdir(dir, os.ModePerm)
	if err != nil {
		scopes.Framework.Errorf("Error creating dir: runID='%v', prefix='%s', workDir='%v', err='%v'",
			c.suite.settings.RunID, name, c.workDir, err)
	} else {
		scopes.Framework.Debugf("Created a dir: runID='%v', name='%s'", c.suite.settings.RunID, dir)
	}
	return dir, err
}

func (c *testContext) CreateDirectoryOrFail(name string) string {
	tmp, err := c.CreateDirectory(name)
	if err != nil {
		c.Fatalf("Error creating  directory with name %q: %v", name, err)
	}
	return tmp
}

func (c *testContext) CreateTmpDirectory(prefix string) (string, error) {
	dir, err := ioutil.TempDir(c.workDir, prefix)
	if err != nil {
		scopes.Framework.Errorf("Error creating temp dir: runID='%v', prefix='%s', workDir='%v', err='%v'",
			c.suite.settings.RunID, prefix, c.workDir, err)
	} else {
		scopes.Framework.Debugf("Created a temp dir: runID='%v', name='%s'", c.suite.settings.RunID, dir)
	}

	return dir, err
}

func (c *testContext) CreateTmpDirectoryOrFail(prefix string) string {
	tmp, err := c.CreateTmpDirectory(prefix)
	if err != nil {
		c.Fatalf("Error creating temp directory with prefix %q: %v", prefix, err)
	}
	return tmp
}

func (c *testContext) RequireOrSkip(envName environment.Name) {
	if c.Environment().EnvironmentName() != envName {
		c.Skipf("Skipping %q: expected environment not found: %s", c.Name(), envName)
	}
}

func (c *testContext) newChildContext(test *Test) *testContext {
	return newTestContext(test, test.goTest, c.suite, c.scope, label.NewSet(test.labels...))
}

func (c *testContext) NewSubTest(name string) *Test {
	if c.test == nil {
		panic(fmt.Sprintf("Attempting to create subtest %s from a TestContext with no associated Test", name))
	}

	if c.test.goTest == nil || c.test.ctx == nil {
		panic(fmt.Sprintf("Attempting to create subtest %s before running parent", name))
	}

	return &Test{
		name:   name,
		parent: c.test,
		s:      c.test.s,
	}
}

func (c *testContext) WhenDone(fn func() error) {
	c.scope.addCloser(&closer{fn})
}

func (c *testContext) Done() {
	if c.Failed() {
		scopes.Framework.Debugf("Begin dumping testContext: %q", c.id)
		c.scope.dump()
		scopes.Framework.Debugf("Completed dumping testContext: %q", c.id)
	}

	scopes.Framework.Debugf("Begin cleaning up testContext: %q", c.id)
	if err := c.scope.done(c.suite.settings.NoCleanup); err != nil {
		c.Logf("error scope cleanup: %v", err)
		if c.Settings().FailOnDeprecation {
			if errors.IsOrContainsDeprecatedError(err) {
				c.Error("Using deprecated Envoy features. Failing due to -istio.test.deprecation_failure flag.")
			}
		}
	}
	scopes.Framework.Debugf("Completed cleaning up testContext: %q", c.id)
}

func (c *testContext) Error(args ...interface{}) {
	c.Helper()
	c.T.Error(args...)
}

func (c *testContext) Errorf(format string, args ...interface{}) {
	c.Helper()
	c.T.Errorf(format, args...)
}

func (c *testContext) Fail() {
	c.Helper()
	c.T.Fail()
}

func (c *testContext) FailNow() {
	c.Helper()
	c.T.FailNow()
}

func (c *testContext) Failed() bool {
	c.Helper()
	return c.T.Failed()
}

func (c *testContext) Fatal(args ...interface{}) {
	c.Helper()
	c.T.Fatal(args...)
}

func (c *testContext) Fatalf(format string, args ...interface{}) {
	c.Helper()
	c.T.Fatalf(format, args...)
}

func (c *testContext) Log(args ...interface{}) {
	c.Helper()
	c.T.Log(args...)
}

func (c *testContext) Logf(format string, args ...interface{}) {
	c.Helper()
	c.T.Logf(format, args...)
}

func (c *testContext) Name() string {
	c.Helper()
	return c.T.Name()
}

func (c *testContext) Skip(args ...interface{}) {
	c.Helper()
	c.T.Skip(args...)
}

func (c *testContext) SkipNow() {
	c.Helper()
	c.T.SkipNow()
}

func (c *testContext) Skipf(format string, args ...interface{}) {
	c.Helper()
	c.T.Skipf(format, args...)
}

func (c *testContext) Skipped() bool {
	c.Helper()
	return c.T.Skipped()
}

var _ io.Closer = &closer{}

type closer struct {
	fn func() error
}

func (c *closer) Close() error {
	return c.fn()
}
