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

func TestWhiteListing(t *testing.T) {
	framework.Run(t, func(ctx framework.TestContext) {
		t.Skipf("https://github.com/istio/istio/issues/13845")
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
		g.ApplyConfigOrFail(
			t,
			bookinfoNs,
			bookinfo.GetDestinationRuleConfigFile(t, ctx).LoadWithNamespaceOrFail(t, bookinfoNs.Name()),
			bookinfo.NetworkingVirtualServiceAllV1.LoadWithNamespaceOrFail(t, bookinfoNs.Name()),
		)
		defer g.DeleteConfigOrFail(
			t,
			bookinfoNs,
			bookinfo.NetworkingBookinfoGateway.LoadGatewayFileWithNamespaceOrFail(t, bookinfoNs.Name()),
		)
		defer g.DeleteConfigOrFail(
			t,
			bookinfoNs,
			bookinfo.GetDestinationRuleConfigFile(t, ctx).LoadWithNamespaceOrFail(t, bookinfoNs.Name()),
			bookinfo.NetworkingVirtualServiceAllV1.LoadWithNamespaceOrFail(t, bookinfoNs.Name()),
		)

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
		// Verify you can't access productpage now.
		util.SendTrafficAndWaitForExpectedStatus(ing, t, "Sending traffic...", "", 30, http.StatusForbidden)
	})
}
