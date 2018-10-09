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

	kubeCore "k8s.io/api/core/v1"

	"istio.io/istio/pkg/test/deployment"

	"istio.io/istio/pkg/test/env"
	"istio.io/istio/pkg/test/framework/settings"
)

const (
	// DefaultIstioSystemNamespace default value for IstioSystemNamespace
	DefaultIstioSystemNamespace = "istio-system"

	// DefaultImagePullPolicy for Docker images
	DefaultImagePullPolicy = kubeCore.PullIfNotPresent

	// DefaultReplicaCount for deployments
	DefaultReplicaCount = 1

	// DefaultAutoscaleMin for deployments
	DefaultAutoscaleMin = 1

	// DefaultAutoscaleMax for deployments
	DefaultAutoscaleMax = 5

	// LatestTag value
	LatestTag = "latest"
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

	// IMAGE_PULL_POLICY is the Docker pull policy to be used for images.
	// nolint: golint
	IMAGE_PULL_POLICY env.Variable = "IMAGE_PULL_POLICY"

	globalSettings = &Settings{
		KubeConfig:           ISTIO_TEST_KUBE_CONFIG.Value(),
		IstioSystemNamespace: DefaultIstioSystemNamespace,
		Images:               defaultImageSettings(),
		IngressGateway:       defaultReplicaSettings(),
	}
)

func defaultImageSettings() deployment.ImageSettings {
	return deployment.ImageSettings{
		Hub:             HUB.Value(),
		Tag:             TAG.Value(),
		ImagePullPolicy: kubeCore.PullPolicy(IMAGE_PULL_POLICY.ValueOrDefault(string(DefaultImagePullPolicy))),
	}
}

func defaultReplicaSettings() deployment.ReplicaSettings {
	return deployment.ReplicaSettings{
		ReplicaCount: DefaultReplicaCount,
		AutoscaleMin: DefaultAutoscaleMin,
		AutoscaleMax: DefaultAutoscaleMax,
	}
}

// New returns settings built from flags and environment variables.
func newSettings() (*Settings, error) {
	// Make a local copy.
	s := &(*globalSettings)

	// Always pull Docker images if using the "latest".
	if s.Images.Tag == LatestTag {
		s.Images.ImagePullPolicy = kubeCore.PullAlways
	}

	if err := s.validate(); err != nil {
		return nil, err
	}

	return s, nil
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

	// Images settings for docker images.
	Images deployment.ImageSettings

	// IngressGateway replica settings for istio-ingressgateway.
	IngressGateway deployment.ReplicaSettings
}

// toDeploymentSettings converts this object into deployment settings.
func (s *Settings) toDeploymentSettings(workDir string) *deployment.Settings {
	return &deployment.Settings{
		GlobalMtlsEnabled: true,
		GalleyEnabled:     true,
		EnableCoreDump:    true,
		KubeConfig:        s.KubeConfig,
		WorkDir:           workDir,
		Namespace:         s.IstioSystemNamespace,
		Images:            s.Images,
		IngressGateway:    s.IngressGateway,
	}
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

	result += fmt.Sprintf("KubeConfig:                %s\n", s.KubeConfig)
	result += fmt.Sprintf("IstioSystemNamespace:      %s\n", s.IstioSystemNamespace)
	result += fmt.Sprintf("DependencyNamespace:       %s\n", s.DependencyNamespace)
	result += fmt.Sprintf("TestNamespace:             %s\n", s.TestNamespace)
	result += fmt.Sprintf("DeployIstio:               %v\n", s.DeployIstio)
	result += fmt.Sprintf("MinikubeIngress:           %v\n", s.MinikubeIngress)
	result += fmt.Sprintf("Images.Hub:                %s\n", s.Images.Hub)
	result += fmt.Sprintf("Images.Tag:                %s\n", s.Images.Tag)
	result += fmt.Sprintf("Images.ImagePullPolicy:    %s\n", s.Images.ImagePullPolicy)
	result += fmt.Sprintf("IngressGeway.ReplicaCount: %v\n", s.IngressGateway.ReplicaCount)
	result += fmt.Sprintf("IngressGeway.AutoscaleMin: %v\n", s.IngressGateway.AutoscaleMin)
	result += fmt.Sprintf("IngressGeway.AutoscaleMax: %v\n", s.IngressGateway.AutoscaleMax)

	return result
}
