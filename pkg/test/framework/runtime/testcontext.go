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

	"istio.io/istio/pkg/test/framework/label"

	"istio.io/istio/pkg/test/framework/components/environment"
	"istio.io/istio/pkg/test/framework/resource"

	"istio.io/istio/pkg/test/framework/core"

	"istio.io/istio/pkg/test/scopes"
)

// testContext for the currently executing test.
type testContext struct {
	// The id of the test context. useful for debugging the test framework itself.
	id string

	// suite-level context
	suite *suiteContext

	// resource scope for this context.
	scope *scope

	// The workDir for this particular context
	workDir string
}

func newTestContext(t *testing.T, s *suiteContext, parentScope *scope, labels label.Set) *testContext {
	id := s.allocateContextID(t.Name())

	allLabels := s.suiteLabels.Merge(labels)
	if !s.settings.Selector.Selects(allLabels) {
		t.Skipf("Skipping: label mismatch: labels=%v, filter=%v", allLabels, s.settings.Selector)
	}

	scopes.Framework.Debugf("Creating New test context")
	workDir := path.Join(s.settings.RunDir(), t.Name())
	if err := os.MkdirAll(workDir, os.ModePerm); err != nil {
		t.Fatalf("Error creating work dir %q: %v", workDir, err)
	}

	scopes.Framework.Debugf("Creating new testContext: %q", id)

	scopeID := fmt.Sprintf("[%s]", id)
	return &testContext{
		id:      id,
		suite:   s,
		scope:   newScope(scopeID, parentScope),
		workDir: workDir,
	}
}

// Settings returns the current runtime.Settings.
func (c *testContext) Settings() *core.Settings {
	return c.suite.settings
}

// TrackResource adds a new resource to track to the context at this level.
func (c *testContext) TrackResource(r resource.Resource) resource.ID {
	id := c.suite.allocateResourceID(c.id, r)
	rid := &resourceID{id: id}
	c.scope.add(r, rid)
	return rid
}

// RunDir allocated for this test.
func (c *testContext) WorkDir() string {
	return c.workDir
}

// Environment returns the environment
func (c *testContext) Environment() resource.Environment {
	return c.suite.environment
}

// CreateDirectory creates a new subdirectory within this context.
func (c *testContext) CreateDirectory(name string) (string, error) {
	dir, err := ioutil.TempDir(c.workDir, name)
	if err != nil {
		scopes.Framework.Errorf("Error creating temp dir: runID='%v', prefix='%s', workDir='%v', err='%v'",
			c.suite.settings.RunID, name, c.workDir, err)
	} else {
		scopes.Framework.Debugf("Created a temp dir: runID='%v', name='%s'", c.suite.settings.RunID, dir)
	}
	return dir, err
}

// CreateDirectoryOrFail creates a new sub directory with the given name in the workdir, or fails the test.
func (c *testContext) CreateDirectoryOrFail(t *testing.T, name string) string {
	t.Helper()

	tmp, err := c.CreateDirectory(name)
	if err != nil {
		t.Fatalf("Error creating  directory with name %q: %v", name, err)
	}
	return tmp
}

// CreateTmpDirectory creates a new temporary directory with the given prefix.
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

// CreateTmpDirectoryOrFail creates a new temporary directory with the given prefix, or fails the test.
func (c *testContext) CreateTmpDirectoryOrFail(t *testing.T, prefix string) string {
	t.Helper()

	tmp, err := c.CreateTmpDirectory(prefix)
	if err != nil {
		t.Fatalf("Error creating temp directory with prefix %q: %v", prefix, err)
	}
	return tmp
}

// RequireOrSkip skips the test if the environment is not as expected.
func (c *testContext) RequireOrSkip(t *testing.T, envName environment.Name) {
	t.Helper()
	if c.Environment().EnvironmentName() != envName {
		t.Skipf("Skipping %q: expected environment not found: %s", t.Name(), envName)
	}
}

// Done should be called when this scope is cleaned up.
func (c *testContext) Done(t *testing.T) {
	if t.Failed() {
		scopes.Framework.Debugf("Begin dumping testContext: %q", c.id)
		c.scope.dump()
		scopes.Framework.Debugf("Completed dumping testContext: %q", c.id)
	}

	scopes.Framework.Debugf("Begin cleaning up testContext: %q", c.id)
	if err := c.scope.done(c.suite.settings.NoCleanup); err != nil {
		t.Logf("error scope cleanup: %v", err)
	}
	scopes.Framework.Debugf("Completed cleaning up testContext: %q", c.id)
}
