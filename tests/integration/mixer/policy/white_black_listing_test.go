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
	"istio.io/istio/pkg/test/framework/components/environment"
	"istio.io/istio/pkg/test/framework/components/galley"
	"istio.io/istio/pkg/test/framework/components/ingress"
	"istio.io/istio/pkg/test/framework/components/mixer"
	"istio.io/istio/pkg/test/framework/components/namespace"
	util "istio.io/istio/tests/integration/mixer"
)

func TestWhiteListing(t *testing.T) {
	framework.Run(t, func(ctx framework.TestContext) {
		ctx.RequireOrSkip(t, environment.Kube)

		bookinfoNs, err := namespace.New(ctx, "istio-bookinfo", true)
		if err != nil {
			t.Fatalf("Could not create istio-bookinfo Namespace; err:%v", err)
		}
		d := bookinfo.DeployOrFail(t, ctx, bookinfo.Config{Namespace: bookinfoNs, Cfg: bookinfo.BookInfo})
		g := galley.NewOrFail(t, ctx, galley.Config{})
		_ = mixer.NewOrFail(t, ctx, mixer.Config{Galley: g})

		g.ApplyConfigOrFail(
			t,
			d.Namespace(),
			bookinfo.NetworkingBookinfoGateway.LoadGatewayFileWithNamespaceOrFail(t, bookinfoNs.Name()))
		g.ApplyConfigOrFail(
			t,
			d.Namespace(),
			bookinfo.GetDestinationRuleConfigFile(t, ctx).LoadWithNamespaceOrFail(t, bookinfoNs.Name()),
			bookinfo.NetworkingVirtualServiceAllV1.LoadWithNamespaceOrFail(t, bookinfoNs.Name()),
		)

		ing := ingress.NewOrFail(t, ctx, ingress.Config{Istio: ist})
		// Verify you can access productpage right now.
		util.SendTrafficOrFailTillRequestedReturnStatus(ing, t, "Sending traffic...", "", 2, http.StatusOK)

		g.ApplyConfigOrFail(
			t,
			d.Namespace(),
			bookinfo.PolicyDenyIPRule.LoadWithNamespaceOrFail(t, bookinfoNs.Name()))
		// Verify you can't access productpage now.
		util.SendTrafficOrFailTillRequestedReturnStatus(ing, t, "Sending traffic...", "", 30, http.StatusForbidden)
	})
}
