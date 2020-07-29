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
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestURL(t *testing.T) {

	assert.Equal(t, ReleaseTar,
		`https://github.com/istio/istio/releases/download/`+baseVersion+`/istio-`+baseVersion+`-linux-amd64.tar.gz`,
		"base url should be equal")

	assert.Equal(t, BaseURL, "https://istio.io/", "base url should be equal")
	assert.Equal(t, DocsURL, "https://istio.io/latest/docs/", "docs url should be equal")

	assert.Equal(t, SetupURL, "https://istio.io/latest/docs/setup/", "setup url should be equal")
	assert.Equal(t, SidecarInjection,
		"https://istio.io/latest/docs/setup/additional-setup/sidecar-injection/#automatic-sidecar-injection",
		"SidecarInjection url should be equal")
	assert.Equal(t, SidecarDeployingApp,
		"https://istio.io/latest/docs/setup/additional-setup/sidecar-injection/#deploying-an-app",
		"SidecarDeployingApp url should be equal")

	assert.Equal(t, TasksURL, "https://istio.io/latest/docs/tasks/", "tasks url should be equal")
	assert.Equal(t, ExamplesURL, "https://istio.io/latest/docs/examples/", "examples url should be equal")

	assert.Equal(t, OpsURL, "https://istio.io/latest/docs/ops/", "ops url should be equal")
	assert.Equal(t, DeploymentRequirements,
		"https://istio.io/latest/docs/ops/deployment/requirements/",
		"DeploymentRequirements url should be equal")
	assert.Equal(t, ConfigureSAToken,
		"https://istio.io/latest/docs/ops/best-practices/security/#configure-third-party-service-account-tokens",
		"ConfigureSAToken url should be equal")

	assert.Equal(t, ReferenceURL, "https://istio.io/latest/docs/reference/", "reference url should be equal")
	assert.Equal(t, IstioOperatorSpec,
		"https://istio.io/latest/docs/reference/config/istio.operator.v1alpha1/#IstioOperatorSpec",
		"IstioOperatorSpec url should be equal")
	assert.Equal(t, ConfigAnalysis,
		"https://istio.io/latest/docs/reference/config/analysis",
		"ConfigAnalysis url should be equal")

	assert.Equal(t, K8TLSBootstrapping,
		"https://kubernetes.io/docs/reference/command-line-tools-reference/kubelet-tls-bootstrapping",
		"K8TLSBootstrapping url should be equal")
}
