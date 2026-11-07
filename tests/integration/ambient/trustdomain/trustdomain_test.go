//go:build integ

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

package trustdomain

import (
	"fmt"
	"testing"

	"istio.io/istio/pkg/test/echo/common/scheme"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/check"
	"istio.io/istio/pkg/test/framework/components/echo/common/ports"
	"istio.io/istio/pkg/test/framework/components/echo/match"
)

func TestCrossClusterTrustDomains(t *testing.T) {
	framework.NewTest(t).Run(func(t framework.TestContext) {
		client.CallOrFail(t, callOptions(t, check.OK()))
	})
}

func TestAuthorizationPolicyTrustDomainAliases(t *testing.T) {
	framework.NewTest(t).Run(func(t framework.TestContext) {
		policy := func(serviceAccount string) string {
			return fmt.Sprintf(`apiVersion: security.istio.io/v1
kind: AuthorizationPolicy
metadata:
  name: trust-domain-alias
spec:
  selector:
    matchLabels:
      app: server
  action: ALLOW
  rules:
  - from:
    - source:
        principals: ["spiffe://%s/ns/%s/sa/%s"]`, serverTrustDomain, testNamespace.Name(), serviceAccount)
		}

		t.NewSubTest("caller alias is accepted").Run(func(t framework.TestContext) {
			t.ConfigIstio().YAML(testNamespace.Name(), policy("client")).ApplyOrFail(t)
			client.CallOrFail(t, callOptions(t, check.OK()))
		})

		t.NewSubTest("unrelated principal is denied").Run(func(t framework.TestContext) {
			t.ConfigIstio().YAML(testNamespace.Name(), policy("other")).ApplyOrFail(t)
			client.CallOrFail(t, callOptions(t, check.Or(
				check.ConnectionResetByPeer(),
				check.NoErrorAndStatus(403),
			)))
		})
	})
}

func callOptions(t framework.TestContext, responseCheck echo.Checker) echo.CallOptions {
	t.Helper()
	return echo.CallOptions{
		To:                      match.Cluster(serverCluster).FirstOrFail(t, servers),
		Address:                 "server",
		Port:                    ports.HTTP,
		Scheme:                  scheme.HTTP,
		Check:                   responseCheck,
		NewConnectionPerRequest: true,
	}
}
