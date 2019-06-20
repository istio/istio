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

package policy

import (
	"net/http"
	"testing"

	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/bookinfo"
	util "istio.io/istio/tests/integration/mixer"
)

func validateIPRule(t *testing.T) {
	t.Helper()
	if galInst == nil {
		t.Fatalf("galley not setup")
	}
	g := *galInst
	if bookinfoNamespace == nil {
		t.Fatalf("bookinfo namespace not allocated in setup")
	}
	bookinfoNs := *bookinfoNamespace
	g.ApplyConfigOrFail(
		t,
		bookinfoNs,
		bookinfo.NetworkingBookinfoGateway.LoadGatewayFileWithNamespaceOrFail(t, bookinfoNs.Name()))
	defer g.DeleteConfigOrFail(
		t,
		bookinfoNs,
		bookinfo.NetworkingBookinfoGateway.LoadGatewayFileWithNamespaceOrFail(t, bookinfoNs.Name()))

	if ingInst == nil {
		t.Fatalf("ingress not setup")
	}
	ing := *ingInst
	// Verify you can access productpage right now.
	util.SendTrafficAndWaitForExpectedStatus(ing, t, "Sending traffic...", "", 2, http.StatusOK)

	g.ApplyConfigOrFail(
		t,
		bookinfoNs,
		bookinfo.PolicyDenyIPRule.LoadWithNamespaceOrFail(t, bookinfoNs.Name()))
	defer g.DeleteConfigOrFail(
		t,
		bookinfoNs,
		bookinfo.PolicyDenyIPRule.LoadWithNamespaceOrFail(t, bookinfoNs.Name()))
	util.AllowRuleSync(t)
	// Verify you can't access productpage now.
	util.SendTrafficAndWaitForExpectedStatus(ing, t, "Sending traffic...", "", 30, http.StatusForbidden)
}

func TestWhiteListing(t *testing.T) {
	framework.Run(t, func(_ framework.TestContext) {
		validateIPRule(t)
	})
}

func TestWhiteListingTCP(t *testing.T) {
	// re-validate after marking productpage port 9080 TCP
	framework.Run(t, func(_ framework.TestContext) {
		if galInst == nil {
			t.Fatalf("galley not setup")
		}
		g := *galInst
		if bookinfoNamespace == nil {
			t.Fatalf("bookinfo namespace not allocated in setup")
		}
		bookinfoNs := *bookinfoNamespace

		g.ApplyConfigOrFail(t, bookinfoNs,
			`apiVersion: v1
kind: Service
metadata:
  name: productpage
spec:
  ports:
  - port: 9080
    name: tcp`)
		defer g.ApplyConfigOrFail(t, bookinfoNs,
			`apiVersion: v1
kind: Service
metadata:
  name: productpage
spec:
  ports:
  - port: 9080
    name: http`)

		validateIPRule(t)
	})
}
