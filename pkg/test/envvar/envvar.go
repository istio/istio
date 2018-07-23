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

package envvar

import (
	"os"
)

// Below or the environment variables that are used by the framework.

// EnvironmentVariable is a wrapper for an environment variable.
type EnvironmentVariable string

// Name of the environment variable.
func (e EnvironmentVariable) Name() string {
	return string(e)
}

// Value of the environment variable.
func (e EnvironmentVariable) Value() string {
	return os.Getenv(e.Name())
}

var (
	// ISTIO_TEST_ENVIRONMENT indicates in which mode the test framework should run. It can be "local", or
	// "kube".
	// nolint: golint
	ISTIO_TEST_ENVIRONMENT EnvironmentVariable = "ISTIO_TEST_ENVIRONMENT"

	// ISTIO_TEST_KUBE_CONFIG is the Kubernetes configuration file to use for testing. If a configuration file
	// is specified on the command-line, that takes precedence.
	// nolint: golint
	ISTIO_TEST_KUBE_CONFIG EnvironmentVariable = "ISTIO_TEST_KUBE_CONFIG"
)
