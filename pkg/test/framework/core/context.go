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

package core

import (
	"testing"
)

// Context is the core context interface that is used by resources and tests.
type Context interface {
	// TrackResource tracks a resource in this context. If the context is closed, then the resource will be
	// cleaned up.
	TrackResource(r Resource) ResourceID

	// The Environment in which the tests run
	Environment() Environment

	// Settings returns common settings
	Settings() *Settings

	// CreateDirectory creates a new subdirectory within this context.
	CreateDirectory(name string) (string, error)

	// CreateTmpDirectory creates a new temporary directory within this context.
	CreateTmpDirectory(prefix string) (string, error)
}

// SuiteContext contains suite-level items used during runtime.
type SuiteContext interface {
	Context

	// Skip indicates that all of the tests in this suite should be skipped.
	Skip(reason string)

	// Skip indicates that all of the tests in this suite should be skipped.
	Skipf(reasonfmt string, args ...interface{})
}

// TestContext is a test-level context that can be created as part of test executing tests.
type TestContext interface {
	Context

	// WorkDir allocated for this test.
	WorkDir() string

	// CreateDirectoryOrFail creates a new sub directory with the given name in the workdir, or fails the test.
	CreateDirectoryOrFail(t *testing.T, name string) string

	// CreateTmpDirectoryOrFail creates a new temporary directory with the given prefix in the workdir, or fails the test.
	CreateTmpDirectoryOrFail(t *testing.T, prefix string) string

	// RequireOrSkip skips the test if the environment is not as expected.
	RequireOrSkip(t *testing.T, envName EnvironmentName)

	// Done should be called when this context is to be cleaned up
	Done(t *testing.T)
}
