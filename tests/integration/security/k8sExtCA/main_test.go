// +build integ
//  Copyright Istio Authors
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

package k8sExtCA

import (
	"testing"

	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/label"
	"istio.io/istio/pkg/test/framework/resource"
)

var (
	inst istio.Instance
)

func TestMain(m *testing.M) {
	// Integration test for provisioning DNS certificates.
	// TODO (lei-tang): investigate whether this test can be moved to integration/security.
	framework.NewSuite(m).
		Label(label.CustomSetup).
		Setup(istio.Setup(&inst, setupConfig)).
		Run()
}

func setupConfig(_ resource.Context, cfg *istio.Config) {
	if cfg == nil {
		return
	}

	// Adding K8s specific environment variables
	// TODO: Deploy external CA that signs certificates using a known root-cert.pem
	cfg.ControlPlaneValues = `
values:
  meshConfig:
    trustDomainAliases: [some-other, trust-domain-foo]
  global:
    ca:
      type: KUBERNETES
      kubernetesConfig:
        signerName: kubernetes.io/legacy-unknown
`
}
