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
	"fmt"
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
	// The id of the test context. useful for debugging the test framework itself.
	id string

	// suite-level context
	suite *SuiteContext

	// resource scope for this context.
	scope *scope

	// The workDir for this particular context
	workDir string
}

var _ resource.Context = &TestContext{}

func newTestContext(s *SuiteContext, parentScope *scope, t *testing.T) *TestContext {
	id := s.allocateContextID(t.Name())
	scopes.Framework.Debugf("Creating New test context")
	workDir := path.Join(s.settings.RunDir(), t.Name())
	if err := os.MkdirAll(workDir, os.ModePerm); err != nil {
		t.Fatalf("Error creating work dir %q: %v", workDir, err)
	}

	scopes.Framework.Debugf("Creating new TestContext: %q", id)

	scopeID := fmt.Sprintf("[%s]", id)
	return &TestContext{
		id:      id,
		suite:   s,
		scope:   newScope(scopeID, parentScope),
		workDir: workDir,
	}
}

// Settings returns the current runtime.Settings.
func (c *TestContext) Settings() *common.Settings {
	return c.suite.settings
}

// TrackResource adds a new resource to track to the context at this level.
func (c *TestContext) TrackResource(r resource.Instance) {
	c.scope.add(r)
}

// RunDir allocated for this test.
func (c *TestContext) WorkDir() string {
	return c.workDir
}

// Environment returns the environment
func (c *TestContext) Environment() environment.Instance {
	return c.suite.environment
}

// CreateTmpDirectory creates a new temporary directory with the given prefix.
func (c *TestContext) CreateTmpDirectory(prefix string) (string, error) {
	dir, err := ioutil.TempDir(c.workDir, prefix)
	if err != nil {
		scopes.Framework.Errorf("Error creating temp dir: runID='%s', prefix='%s', workDir='%v', err='%v'",
			c.suite.settings.RunID, prefix, c.workDir, err)
	} else {
		scopes.Framework.Debugf("Created a temp dir: runID='%s', name='%s'", c.suite.settings.RunID, dir)
	}

	return dir, err
}

// CreateTmpDirectoryOrFail creates a new temporary directory with the given prefix, or fails the test.
func (c *TestContext) CreateTmpDirectoryOrFail(t *testing.T, prefix string) string {
	t.Helper()

	tmp, err := c.CreateTmpDirectory(prefix)
	if err != nil {
		t.Fatalf("Error creating temp directory with prefix %q: %v", prefix, err)
	}
	return tmp
}

// RequireOrSkip skips the test if the environment is not as expected.
func (c *TestContext) RequireOrSkip(t *testing.T, envName environment.Name) {
	t.Helper()
	if c.Environment().EnvironmentName() != envName {
		t.Skipf("Skipping %q: expected environment not found: %s", t.Name(), envName)
	}
}

func (c *TestContext) newChild(t *testing.T) *TestContext {
	return newTestContext(c.suite, c.scope, t)
}

// Done should be called when this scope is cleaned up.
func (c *TestContext) Done(t *testing.T) {
	scopes.Framework.Debugf("Begin cleaning up TestContext: %q", c.id)
	if err := c.scope.done(c.suite.settings.NoCleanup); err != nil {
		t.Fatalf("error scope cleanup: %v", err)
	}
	scopes.Framework.Debugf("Completed cleaning up TestContext: %q", c.id)
}
