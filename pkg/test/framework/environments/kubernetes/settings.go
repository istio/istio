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

import (
	"fmt"

	"istio.io/istio/pkg/test/env"
	"istio.io/istio/pkg/test/framework/settings"
)

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

	globalSettings = &Settings{
		Hub: HUB.Value(),
		Tag: TAG.Value(),
	}
)

// New returns settings built from flags and environment variables.
func newSettings() (*Settings, error) {
	// Make a local copy.
	s := &(*globalSettings)

	if err := s.validate(); err != nil {
		return nil, err
	}

	return s, nil
}

// Settings is the set of arguments to the test driver.
type Settings struct {
	// Path to kube config file. Required if the environment is kubernetes.
	KubeConfig string

	// Hub environment variable
	Hub string

	// Tag environment variable
	Tag string

	// The namespace where the Istio components reside in a typical deployment (typically "istio-system").
	// If not specified, a new namespace will be generated with a UUID.
	IstioSystemNamespace string

	// The namespace in which dependency components are deployed. If not specified, a new namespace will be generated
	// with a UUID once per run. Test framework dependencies can deploy components here when they get initialized.
	// They will get deployed only once.
	DependencyNamespace string

	// The namespace for each individual test. If not specified, the namespaces are created when an environment is acquired
	// in a test, and the previous one gets deleted. This ensures that during a single test run, there is only
	// one test namespace in the system.
	TestNamespace string
}

func (s *Settings) validate() error {
	// Validate the arguments.
	if s.KubeConfig == "" {
		return fmt.Errorf(
			"environment %q requires kube configuration (can be specified from command-line, or with %s)",
			string(settings.Kubernetes), ISTIO_TEST_KUBE_CONFIG.Name())
	}

	return nil
}
