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
	"fmt"
	"testing"

	"istio.io/istio/pkg/test/dependency"
	"istio.io/istio/pkg/test/label"
)

const (
	// EnvKubernetes is the kubernetes environment
	EnvKubernetes = "kubernetes"

	// EnvLocal is the loal environment
	EnvLocal = "local"
)

// Args is the set of arguments to the test driver.
type Args struct {
	// Labels is the comma-separated set of set of labels for selecting tests to run. Tests matching to
	// at least one specifed label will be executed. If unspecified, all applicable tests will be executed.
	Labels string

	// Environment to run the tests in. By default, a local environment will be used.
	Environment string

	// Path to kube config file. Required if the environment is kubernetes.
	KubeConfig string

	// TestID is the id of the test suite. This should supplied by the author once, and must be immutable.
	TestID string

	// M is testing.M
	M *testing.M

	// SuiteDependencies is the set of dependencies the suite needs.
	SuiteDependencies []dependency.Dependency

	// SuiteLabels is the set of labels that is attached to the suite.
	SuiteLabels []label.Label
}

// DefaultArgs returns the default set of arguments.
func DefaultArgs() *Args {
	return &Args{
		Environment: EnvLocal,
	}
}

// Validate the arguments.
func (a *Args) Validate() error {
	switch a.Environment {
	case EnvLocal, EnvKubernetes:

	default:
		return fmt.Errorf("unrecognized environment: %s", a.Environment)
	}

	if a.Environment == EnvKubernetes && a.KubeConfig == "" {
		return fmt.Errorf("kube configuration must be specified for the %s environment", EnvKubernetes)
	}

	if a.TestID == "" || len(a.TestID) > maxTestIDLength {
		return fmt.Errorf("testID must be non-empty and  cannot be longer than %d characters", maxTestIDLength)
	}

	return nil
}
