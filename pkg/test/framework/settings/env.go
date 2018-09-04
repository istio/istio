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

package settings

import "istio.io/istio/pkg/test/env"

// Below or the environment variables that are used by the framework.

var (
	// ISTIO_TEST_ENVIRONMENT indicates in which mode the test framework should run. It can be "local", or
	// "kube".
	// nolint: golint
	ISTIO_TEST_ENVIRONMENT env.Variable = "ISTIO_TEST_ENVIRONMENT"

	// ISTIO_TEST_KUBE_CONFIG is the Kubernetes configuration file to use for testing. If a configuration file
	// is specified on the command-line, that takes precedence.
	// nolint: golint
	ISTIO_TEST_KUBE_CONFIG env.Variable = "ISTIO_TEST_KUBE_CONFIG"
)
