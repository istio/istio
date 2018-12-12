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

package kube

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/mitchellh/go-homedir"

	"istio.io/istio/pkg/test/env"

	kubeCore "k8s.io/api/core/v1"
)

const (
	// HubValuesKey values key for the Docker image hub.
	HubValuesKey = "global.hub"

	// TagValuesKey values key for the Docker image tag.
	TagValuesKey = "global.tag"

	// ImagePullPolicyValuesKey values key for the Docker image pull policy.
	ImagePullPolicyValuesKey = "global.imagePullPolicy"

	// DefaultSystemNamespace default value for SystemNamespace
	DefaultSystemNamespace = "istio-system"

	// DefaultValuesFile for Istio Helm deployment.
	DefaultValuesFile = "values-istio-mcp.yaml"

	// LatestTag value
	LatestTag = "latest"

	// DefaultDeployTimeout for Istio
	DefaultDeployTimeout = time.Second * 480

	// DefaultUndeployTimeout for Istio.
	DefaultUndeployTimeout = time.Second * 600
)

var (
	helmValues string

	globalSettings = &settings{
		KubeConfig:      ISTIO_TEST_KUBE_CONFIG.Value(),
		SystemNamespace: DefaultSystemNamespace,
		DeployIstio:     true,
		DeployTimeout:   DefaultDeployTimeout,
		UndeployTimeout: DefaultUndeployTimeout,
		ChartDir:        env.IstioChartDir,
		ValuesFile:      DefaultValuesFile,
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

// settings provide kube-specific settings from flags.
type settings struct {
	// Path to kube config file. Required if the environment is kubernetes.
	KubeConfig string

	// The namespace where the Istio components reside in a typical deployment (default: "istio-system").
	SystemNamespace string

	// The namespace in which dependency components are deployed. If not specified, a new namespace will be generated
	// with a UUID once per run. Test framework dependencies can deploy components here when they get initialized.
	// They will get deployed only once.
	SuiteNamespace string

	// The namespace for each individual test. If not specified, the namespaces are created when an environment is acquired
	// in a test, and the previous one gets deleted. This ensures that during a single test run, there is only
	// one test namespace in the system.
	TestNamespace string

	// Indicates that the test should deploy Istio into the target Kubernetes cluster before running tests.
	DeployIstio bool

	// DeployTimeout the timeout for deploying Istio.
	DeployTimeout time.Duration

	// UndeployTimeout the timeout for undeploying Istio.
	UndeployTimeout time.Duration

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

// New returns settings built from flags and environment variables.
func newSettings() (*settings, error) {
	// Make a local copy.
	s := &(*globalSettings)

	if s.KubeConfig != "" {
		if err := normalizeFile(&s.KubeConfig); err != nil {
			return nil, err
		}
	}

	if err := normalizeFile(&s.ChartDir); err != nil {
		return nil, err
	}

	if err := checkFileExists(filepath.Join(s.ChartDir, s.ValuesFile)); err != nil {
		return nil, err
	}

	var err error
	s.Values, err = newHelmValues()
	if err != nil {
		return nil, err
	}

	return s, nil
}

func normalizeFile(path *string) error {
	// If the path uses the homedir ~, expand the path.
	var err error
	(*path), err = homedir.Expand(*path)
	if err != nil {
		return err
	}

	return checkFileExists(*path)
}

func checkFileExists(path string) error {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return err
	}
	return nil
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

func (s *settings) String() string {
	result := ""

	result += fmt.Sprintf("KubeConfig:      %s\n", s.KubeConfig)
	result += fmt.Sprintf("SystemNamespace: %s\n", s.SystemNamespace)
	result += fmt.Sprintf("SuiteNamespace:  %s\n", s.SuiteNamespace)
	result += fmt.Sprintf("TestNamespace:   %s\n", s.TestNamespace)
	result += fmt.Sprintf("DeployIstio:     %v\n", s.DeployIstio)
	result += fmt.Sprintf("DeployTimeout:   %ds\n", s.DeployTimeout/time.Second)
	result += fmt.Sprintf("UndeployTimeout: %ds\n", s.UndeployTimeout/time.Second)
	result += fmt.Sprintf("MinikubeIngress: %v\n", s.MinikubeIngress)
	result += fmt.Sprintf("Values:          %v\n", s.Values)

	return result
}
