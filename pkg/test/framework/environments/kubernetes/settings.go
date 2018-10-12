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
	"strings"

	kubeCore "k8s.io/api/core/v1"

	"istio.io/istio/pkg/test/env"
	"istio.io/istio/pkg/test/framework/settings"
)

const (
	// HubValuesKey values key for the Docker image hub.
	HubValuesKey = "global.hub"

	// TagValuesKey values key for the Docker image tag.
	TagValuesKey = "global.tag"

	// ImagePullPolicyValuesKey values key for the Docker image pull policy.
	ImagePullPolicyValuesKey = "global.imagePullPolicy"

	// DefaultIstioSystemNamespace default value for IstioSystemNamespace
	DefaultIstioSystemNamespace = "istio-system"

	// DefaultValuesFile for Istio Helm deployment.
	DefaultValuesFile = "values-istio-mcp.yaml"

	// LatestTag value
	LatestTag = "latest"
)

var (
	helmValues string

	globalSettings = &Settings{
		KubeConfig:           ISTIO_TEST_KUBE_CONFIG.Value(),
		IstioSystemNamespace: DefaultIstioSystemNamespace,
		ChartDir:             env.IstioChartDir,
		ValuesFile:           DefaultValuesFile,
	}

	// Defaults for helm overrides.
	defaultHelmValues = map[string]string{
		HubValuesKey:                  HUB.Value(),
		TagValuesKey:                  TAG.Value(),
		"global.proxy.enableCoreDump": "true",
		"global.mtls.enabled":         "true",
		"galley.enabled":              "true",
	}
)

// New returns settings built from flags and environment variables.
func newSettings() (*Settings, error) {
	// Make a local copy.
	s := &(*globalSettings)

	var err error
	s.Values, err = newHelmValues()
	if err != nil {
		return nil, err
	}

	if err := s.validate(); err != nil {
		return nil, err
	}

	return s, nil
}

func newHelmValues() (map[string]string, error) {
	userValues, err := parseHelmValues()
	if err != nil {
		return nil, err
	}

	// Copy the defaults first.
	values := make(map[string]string)
	for k, v := range defaultHelmValues {
		values[k] = v
	}
	// Copy the user values.
	for k, v := range userValues {
		values[k] = v
	}
	// Always pull Docker images if using the "latest".
	if values[TagValuesKey] == LatestTag {
		values[ImagePullPolicyValuesKey] = string(kubeCore.PullAlways)
	}
	return values, nil
}

// Settings is the set of arguments to the test driver.
type Settings struct {
	// Path to kube config file. Required if the environment is kubernetes.
	KubeConfig string

	// The namespace where the Istio components reside in a typical deployment (default: "istio-system").
	IstioSystemNamespace string

	// The namespace in which dependency components are deployed. If not specified, a new namespace will be generated
	// with a UUID once per run. Test framework dependencies can deploy components here when they get initialized.
	// They will get deployed only once.
	DependencyNamespace string

	// The namespace for each individual test. If not specified, the namespaces are created when an environment is acquired
	// in a test, and the previous one gets deleted. This ensures that during a single test run, there is only
	// one test namespace in the system.
	TestNamespace string

	// Indicates that the test should deploy Istio into the target Kubernetes cluster before running tests.
	DeployIstio bool

	// Indicates that the Ingress Gateway is not available. This typically happens in Minikube. The Ingress
	// component will fall back to node-port in this case.
	MinikubeIngress bool

	// The top-level Helm chart dir.
	ChartDir string

	// The Helm values file to be used.
	ValuesFile string

	// Overrides for the Helm values file.
	Values map[string]string
}

func parseHelmValues() (map[string]string, error) {
	out := make(map[string]string)
	if helmValues == "" {
		return out, nil
	}

	values := strings.Split(helmValues, ",")
	for _, v := range values {
		parts := strings.Split(v, "=")
		if len(parts) != 2 {
			return nil, fmt.Errorf("failed parsing helm values: %s", helmValues)
		}
		out[strings.TrimSpace(parts[0])] = strings.TrimSpace(parts[1])
	}
	return out, nil
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

func (s *Settings) String() string {
	result := ""

	result += fmt.Sprintf("KubeConfig:           %s\n", s.KubeConfig)
	result += fmt.Sprintf("IstioSystemNamespace: %s\n", s.IstioSystemNamespace)
	result += fmt.Sprintf("DependencyNamespace:  %s\n", s.DependencyNamespace)
	result += fmt.Sprintf("TestNamespace:        %s\n", s.TestNamespace)
	result += fmt.Sprintf("DeployIstio:          %v\n", s.DeployIstio)
	result += fmt.Sprintf("MinikubeIngress:      %v\n", s.MinikubeIngress)
	result += fmt.Sprintf("Values:               %v\n", s.Values)

	return result
}
