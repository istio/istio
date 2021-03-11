// Copyright Istio Authors
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

package url

import (
	"fmt"

	"istio.io/istio/operator/version"
)

var (
	v           = version.OperatorBinaryVersion
	baseVersion = v.MinorVersion.String()

	// ReleaseTar is a URL to download latest istio version from Github release
	ReleaseTar = `https://github.com/istio/istio/releases/download/` + baseVersion + `/istio-` + baseVersion + `-linux-amd64.tar.gz`
)

// istio.io related URLs
var (
	// BaseURL for istio.io
	BaseURL = "https://istio.io/"

	// DocsVersion is a documentation version for istio.io
	// This will build version as v1.6, v1.7, v1.8
	DocsVersion = fmt.Sprintf("%s%s", "v", baseVersion)

	// DocsURL is a base docs URL for istio.io
	DocsURL = fmt.Sprintf("%s%s%s", BaseURL, DocsVersion, "/docs/")

	// #####################################
	// Setup related URLs for istio.io
	// #####################################

	// SetupURL is a base URL for setup related docs
	SetupURL = fmt.Sprintf("%s%s", DocsURL, "setup/")

	// SidecarInjection should generate
	// https://istio.io/v1.7/docs/setup/additional-setup/sidecar-injection/#automatic-sidecar-injection
	SidecarInjection = fmt.Sprintf("%s%s", SetupURL, "additional-setup/sidecar-injection/#automatic-sidecar-injection")

	// SidecarDeployingApp should generate
	// https://istio.io/v1.7/docs/setup/additional-setup/sidecar-injection/#deploying-an-app
	SidecarDeployingApp = fmt.Sprintf("%s%s", SetupURL, "additional-setup/sidecar-injection/#deploying-an-app")

	// #####################################
	// Tasks related URLs for istio.io
	// #####################################

	// TasksURL is a base URL for tasks related docs
	TasksURL = fmt.Sprintf("%s%s", DocsURL, "tasks/")

	// #####################################
	// Examples related URLs for istio.io
	// #####################################

	// ExamplesURL is a base URL for examples related docs
	ExamplesURL = fmt.Sprintf("%s%s", DocsURL, "examples/")

	// #####################################
	// Operations related URLs for istio.io
	// #####################################

	// OpsURL is a base URL for operations related docs
	OpsURL = fmt.Sprintf("%s%s", DocsURL, "ops/")

	// DeploymentRequirements should generate
	// https://istio.io/v1.7/docs/ops/deployment/requirements/
	DeploymentRequirements = fmt.Sprintf("%s%s", OpsURL, "deployment/requirements/")

	// ConfigureSAToken should generate
	// https://istio.io/v1.7/docs/ops/best-practices/security/#configure-third-party-service-account-tokens
	ConfigureSAToken = fmt.Sprintf("%s%s", OpsURL, "best-practices/security/#configure-third-party-service-account-tokens")

	// #####################################
	// Reference related URLs for istio.io
	// #####################################

	// ReferenceURL is a base URL for reference related docs
	ReferenceURL = fmt.Sprintf("%s%s", DocsURL, "reference/")

	// IstioOperatorSpec should generate
	// https://istio.io/v1.7/docs/reference/config/istio.operator.v1alpha1/#IstioOperatorSpec
	IstioOperatorSpec = fmt.Sprintf("%s%s", ReferenceURL, "config/istio.operator.v1alpha1/#IstioOperatorSpec")

	// ConfigAnalysis should generate
	// https://istio.io/v1.7/docs/reference/config/analysis
	ConfigAnalysis = fmt.Sprintf("%s%s", ReferenceURL, "config/analysis")
)

// Kubernetes related URLs
var (

	// K8TLSBootstrapping is a link for Kubelet TLS Bootstrapping
	K8TLSBootstrapping = "https://kubernetes.io/docs/reference/command-line-tools-reference/kubelet-tls-bootstrapping"
)
