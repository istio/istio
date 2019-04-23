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
	"testing"

	"istio.io/istio/pkg/test/framework/components/environment"
	"istio.io/istio/pkg/test/framework/resource"
)

// SuiteContext contains suite-level items used during runtime.
type SuiteContext interface {
	resource.Context
}

// TestContext is a test-level context that can be created as part of test executing tests.
type TestContext interface {
	resource.Context

	// WorkDir allocated for this test.
	WorkDir() string

	// CreateDirectoryOrFail creates a new sub directory with the given name in the workdir, or fails the test.
	CreateDirectoryOrFail(t *testing.T, name string) string

	// CreateTmpDirectoryOrFail creates a new temporary directory with the given prefix in the workdir, or fails the test.
	CreateTmpDirectoryOrFail(t *testing.T, prefix string) string

	// RequireOrSkip skips the test if the environment is not as expected.
	RequireOrSkip(t *testing.T, envName environment.Name)

	// Done should be called when this context is to be cleaned up
	Done(t *testing.T)
}
