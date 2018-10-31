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

package kubernetes

import "istio.io/istio/pkg/test/env"

var (
	// ISTIO_TEST_KUBE_CONFIG is the Kubernetes configuration file to use for testing. If a configuration file
	// is specified on the command-line, that takes precedence.
	// nolint: golint
	ISTIO_TEST_KUBE_CONFIG env.Variable = "ISTIO_TEST_KUBE_CONFIG"

	// HUB is the Docker hub to be used for images.
	// nolint: golint
	HUB env.Variable = "HUB"

	// TAG is the Docker tag to be used for images.
	// nolint: golint
	TAG env.Variable = "TAG"
)
