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

package sdsingress

import (
	"testing"

	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/resource"
	"istio.io/istio/tests/integration/security/sds_ingress/util"
)

var (
	inst istio.Instance
)

func TestMain(m *testing.M) {
	// Integration test for the ingress SDS Gateway flow with Istiod acting as the SDS server
	framework.
		NewSuite(m).
		RequireSingleCluster().
		Setup(istio.Setup(&inst, func(ctx resource.Context, cfg *istio.Config) {
			cfg.Values["pilot.env.ISTIOD_ENABLE_SDS_SERVER"] = "true"
		})).
		Run()
}

func TestMtlsGateways(t *testing.T) {
	framework.
		NewTest(t).
		Features("security.ingress.mtls.gateway").
		Run(func(ctx framework.TestContext) {
			util.RunTestMultiMtlsGateways(ctx, inst)
		})
}

func TestTlsGateways(t *testing.T) {
	framework.
		NewTest(t).
		Features("security.ingress.tls.gateway.valid-secret").
		Run(func(ctx framework.TestContext) {
			util.RunTestMultiTLSGateways(ctx, inst)
		})
}
