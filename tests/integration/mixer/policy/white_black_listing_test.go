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

	"istio.io/istio/pkg/test/framework/label"

	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/bookinfo"
	util "istio.io/istio/tests/integration/mixer"
)

func TestWhiteListing(t *testing.T) {
	framework.NewTest(t).Label(label.Flaky).Run(func(ctx framework.TestContext) {
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
	})
}
