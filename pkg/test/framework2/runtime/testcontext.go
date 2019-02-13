//  Copyright 2019 Istio Authors
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

package runtime

import (
	"io/ioutil"
	"os"
	"path"
	"testing"

	"istio.io/istio/pkg/test/framework2/common"
	"istio.io/istio/pkg/test/framework2/components/environment"
	"istio.io/istio/pkg/test/framework2/resource"
	"istio.io/istio/pkg/test/scopes"
)

// TestContext for the currently executing test.
type TestContext struct {
	// testing.T for this context
	t T

	// suite-level context
	suite *SuiteContext

	// resource scope for this context.
	scope *scope

	// The workDir for this particular context
	workDir string
}

var _ resource.Context = &TestContext{}

func newTestContext(s *SuiteContext, parentScope *scope, t T) *TestContext {
	workDir := path.Join(s.settings.RunDir(), t.Name())
	if err := os.MkdirAll(workDir, os.ModePerm); err != nil {
		t.Fatalf("Error creating work dir %q: %v", workDir, err)
	}

	return &TestContext{
		t:       t,
		suite:   s,
		scope:   newScope(parentScope),
		workDir: workDir,
	}
}

// Run starts a new sub-test with the given name. It replaces testing.T.Run(...).
func (c *TestContext) Run(name string, fn func(s *TestContext)) {
	c.t.Helper()
	c.t.Run(name, func(t *testing.T) {
		child := c.newChild(t)
		defer child.Done()
		fn(child)
	})
}

// TrackResource adds a new resource to track to the context at this level.
func (c *TestContext) TrackResource(r interface{}) {
	c.t.Helper()
	c.scope.add(r)
}

// WorkDir allocated for this test.
func (c *TestContext) WorkDir() string {
	c.t.Helper()
	return c.workDir
}

// T returns *testing.T for this test.
func (c *TestContext) T() *testing.T {
	c.t.Helper()
	return c.t.asTestingT()
}

// environment returns the environment
func (c *TestContext) Environment() environment.Instance {
	c.t.Helper()
	return c.suite.environment
}

// Settings returns common settings
func (c *TestContext) Settings() *common.Settings {
	return c.suite.Settings()
}

// CreateTmpDirectory creates a new temporary directory with the given prefix.
func (c *TestContext) CreateTmpDirectory(prefix string) (string, error) {
	dir, err := ioutil.TempDir(c.workDir, prefix)
	if err != nil {
		scopes.Framework.Errorf("Error creating temp dir: runID='%s', prefix='%s', workDir='%v', err='%v'",
			c.suite.settings.RunID(), prefix, c.workDir, err)
	} else {
		scopes.Framework.Debugf("Created a temp dir: runID='%s', name='%s'", c.suite.settings.RunID(), dir)
	}

	return dir, err
}

// CreateTmpDirectoryOrFail creates a new temporary directory with the given prefix, or fails the test.
func (c *TestContext) CreateTmpDirectoryOrFail(prefix string) string {
	c.t.Helper()
	t, err := c.CreateTmpDirectory(prefix)
	if err != nil {
		c.t.Fatalf("Error creating temp directory with prefix %q: %v", prefix, err)
	}
	return t
}

// RequireEnvironmentOrSkip skips the test if the environment is not as expected.
func (c *TestContext) RequireEnvironmentOrSkip(envName string) {
	c.t.Helper()
	if c.Environment().Name() != envName {
		c.t.Skipf("Skipping %q: expected environment not found: %s", c.t.Name(), envName)
	}
}

func (c *TestContext) newChild(t *testing.T) *TestContext {
	return newTestContext(c.suite, c.scope, WrapT(t))
}

// Done should be called when this context done.
func (c *TestContext) Done() {
	c.t.Helper()
	if err := c.scope.done(c.suite.settings.NoCleanup()); err != nil {
		c.t.Fatalf("error scope cleanup: %v", err)
	}
}
