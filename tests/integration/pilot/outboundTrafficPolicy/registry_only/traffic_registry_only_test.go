// Copyright 2019 Istio Authors
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

package registry_only

import (
	"testing"

	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/environment"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/tests/integration/pilot/outboundTrafficPolicy"
)

func TestMain(m *testing.M) {
	var ist istio.Instance
	framework.NewSuite("outbound_traffic_policy_registry_only", m).
		RequireEnvironment(environment.Kube).
		Setup(istio.SetupOnKube(&ist, setupConfig)).
		Run()
}

func setupConfig(cfg *istio.Config) {
	if cfg == nil {
		return
	}
	cfg.Values["global.outboundTrafficPolicy.mode"] = "REGISTRY_ONLY"
	cfg.Values["pilot.env.PILOT_ENABLE_FALLTHROUGH_ROUTE"] = "true"
}

func TestOutboundTrafficPolicyRegistryOnly(t *testing.T) {
	expected := map[string][]string {
		"http": {"502"}, // HTTP will return an error code
		"https": {}, // HTTPS will direct to blackhole cluster, giving no response
	}
	outboundTrafficPolicy.RunExternalRequestTest(expected, t)
}
