//go:build integ
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

package filebasedtlsorigination

import (
	"testing"

	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/label"
	"istio.io/istio/pkg/test/framework/resource"
	"istio.io/istio/tests/integration/security/util/cert"
)

var inst istio.Instance

func TestMain(m *testing.M) {
	// nolint: staticcheck
	framework.
		NewSuite(m).
		Label(label.CustomSetup).
		Label("CustomSetup").
		RequireMultiPrimary().
		Setup(istio.Setup(&inst, setupConfig, cert.CreateCustomEgressSecret)).
		Run()
}

func setupConfig(_ resource.Context, cfg *istio.Config) {
	if cfg == nil {
		return
	}
	cfg.ControlPlaneValues = `
components:
  egressGateways:
  - enabled: true
    name: istio-egressgateway
values:
   gateways:
      istio-egressgateway:
         secretVolumes:
         - name: client-custom-certs
           secretName: egress-gw-cacerts
           mountPath: /etc/certs/custom
`
}
