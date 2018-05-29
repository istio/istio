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

package internal

import "istio.io/istio/pkg/test/dependency"

// TestContext provides the ambient context to internal code.
type TestContext struct {
	testID  string
	runID   string
	workDir string
	env     Environment
	deps    Tracker
}

// NewTestContext initializes and returns a new instance of TestContext.
func NewTestContext(testID, runID, workDir string, env Environment) *TestContext {
	return &TestContext{
		testID:  testID,
		runID:   runID,
		workDir: workDir,
		env:     env,
		deps:    make(map[dependency.Instance]interface{}),
	}
}

// TestID of the current test.
func (t *TestContext) TestID() string {
	scope.Debugf("Enter: TestContext.TestID (%s)", t.testID)

	return t.testID
}

// RunID of the current run.
func (t *TestContext) RunID() string {
	scope.Debugf("Enter: TestContext.RunID (%s)", t.runID)

	return t.runID
}

// Environment interface for internal use.
func (t *TestContext) Environment() Environment {
	scope.Debugf("Enter: TestContext.Environment (%s)", t.testID)

	return t.env
}

// CreateTmpDirectory allows creation of temporary directories.
func (t *TestContext) CreateTmpDirectory(name string) (string, error) {
	scope.Debugf("Enter: TestContext.CreateTmpDirectory (%s)", t.testID)

	return createTmpDirectory(t.workDir, t.runID, name)
}

// Tracker returns the ambient Tracker.
func (t *TestContext) Tracker() Tracker {
	return t.deps
}
