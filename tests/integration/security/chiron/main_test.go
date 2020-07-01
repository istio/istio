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

package chiron_test

import (
	"testing"

	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/label"
)

var (
	inst istio.Instance
)

func TestMain(m *testing.M) {
	// Integration test for provisioning DNS certificates.
	// TODO (lei-tang): investigate whether this test can be moved to integration/security.
	framework.NewSuite(m).
		RequireSingleCluster().
		Label(label.CustomSetup).
		Setup(istio.Setup(&inst, setupConfig)).
		Run()
}

func setupConfig(cfg *istio.Config) {
	if cfg == nil {
		return
	}

	cfg.ControlPlaneValues = `
values:
  meshConfig:
    certificates:
      - dnsNames: [istio-pilot.istio-system.svc, istio-pilot.istio-system]
      - secretName: dns.istio-galley-service-account
        dnsNames: [istio-galley.istio-system.svc, istio-galley.istio-system]
      - secretName: dns.istio-sidecar-injector-service-account
        dnsNames: [istio-sidecar-injector.istio-system.svc, istio-sidecar-injector.istio-system]
`
}
