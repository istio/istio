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

package driver

import (
	"testing"

	"istio.io/istio/pkg/test/dependency"
	"istio.io/istio/pkg/test/environment"
	"istio.io/istio/pkg/test/label"
)

// Interface for the test framework driver.
type Interface interface {
	// Initialize the driver. This must be called exactly once, before Run().
	Initialize(a *Args) error

	// Run the tests by calling into testing.M. This must be called exactly once, after Initialize().
	Run() int

	// AcquireEnvironment resets and returns the environment. Once AcquireEnvironment should be called exactly
	// once per test.
	AcquireEnvironment(t testing.TB) environment.Interface

	// InitializeTestDependencies checks and initializes the supplied dependencies appropriately.
	InitializeTestDependencies(t testing.TB, dependencies []dependency.Instance)

	// CheckLabels checks the labels against the user supplied filter, and skips the test if the labels
	// do not match user supplied labels.
	CheckLabels(t testing.TB, labels []label.Label)
}
