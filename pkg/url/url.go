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
	// Operations related URLs for istio.io
	// #####################################

	// OpsURL is a base URL for operations related docs
	OpsURL = fmt.Sprintf("%s%s", DocsURL, "ops/")

	// DeploymentRequirements should generate
	// https://istio.io/v1.7/docs/ops/deployment/requirements/
	DeploymentRequirements = fmt.Sprintf("%s%s", OpsURL, "deployment/requirements/")

	// ProtocolSelection should generate
	// https://istio.io/v1.15/docs/ops/configuration/traffic-management/protocol-selection/
	ProtocolSelection = fmt.Sprintf("%s%s", OpsURL, "configuration/traffic-management/protocol-selection/")

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
