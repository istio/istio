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
	testID         string
	runID          string
	workDir        string
	hub            string
	tag            string
	env            Environment
	deps           Tracker
	kubeConfigPath string
}

// NewTestContext initializes and returns a new instance of TestContext.
func NewTestContext(testID, runID, workDir, hub, tag string, kubeConfigPath string, env Environment) *TestContext {
	return &TestContext{
		testID:         testID,
		runID:          runID,
		workDir:        workDir,
		hub:            hub,
		tag:            tag,
		env:            env,
		kubeConfigPath: kubeConfigPath,
		deps:           make(map[dependency.Instance]interface{}),
	}
}

// TestID of the current test.
func (t *TestContext) TestID() string {
	return t.testID
}

// RunID of the current run.
func (t *TestContext) RunID() string {
	return t.runID
}

// Environment interface for internal use.
func (t *TestContext) Environment() Environment {
	return t.env
}

// Hub environment variable.
func (t *TestContext) Hub() string {
	return t.hub
}

// Tag environment variable.
func (t *TestContext) Tag() string {
	return t.tag
}

// KubeConfigPath parameter.
func (t *TestContext) KubeConfigPath() string {
	return t.kubeConfigPath
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
