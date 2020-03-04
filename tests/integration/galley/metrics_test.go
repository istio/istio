//  Copyright 2020 Istio Authors
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

package galley

import (
	"fmt"
	"testing"

	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/bookinfo"
	"istio.io/istio/pkg/test/framework/components/environment"
	"istio.io/istio/pkg/test/framework/components/galley"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/framework/components/prometheus"
	util "istio.io/istio/tests/integration/mixer"
)

func TestStateMetrics(t *testing.T) {
	framework.
		NewTest(t).
		RequiresEnvironment(environment.Kube).
		Run(func(ctx framework.TestContext) {
			g := galley.NewOrFail(ctx, ctx, galley.Config{})
			prom := prometheus.NewOrFail(ctx, ctx)
			bookinfoNs := namespace.NewOrFail(ctx, ctx, namespace.Config{
				Prefix: "istio-bookinfo",
				Inject: true,
			})
			g.ApplyConfigOrFail(
				t,
				bookinfoNs,
				bookinfo.GetDestinationRuleConfigFileOrFail(t, ctx).LoadWithNamespaceOrFail(t, bookinfoNs.Name()),
				bookinfo.NetworkingVirtualServiceAllV1.LoadWithNamespaceOrFail(t, bookinfoNs.Name()),
				bookinfo.NetworkingBookinfoGateway.LoadWithNamespaceOrFail(t, bookinfoNs.Name()),
			)
			defer g.DeleteConfigOrFail(t,
				bookinfoNs,
				bookinfo.GetDestinationRuleConfigFileOrFail(t, ctx).LoadWithNamespaceOrFail(t, bookinfoNs.Name()),
				bookinfo.NetworkingVirtualServiceAllV1.LoadWithNamespaceOrFail(t, bookinfoNs.Name()),
				bookinfo.NetworkingBookinfoGateway.LoadWithNamespaceOrFail(t, bookinfoNs.Name()),
			)

			util.AllowRuleSync(t)

			query := fmt.Sprintf("sum(galley_istio_networking_destinationrules{namespace=\"%s\",version=\"v1alpha3\"})", bookinfoNs.Name())
			util.ValidateMetric(t, prom, query, "galley_istio_networking_destinationrules", 4)

			query = fmt.Sprintf("sum(galley_istio_networking_virtualservices{namespace=\"%s\"})", bookinfoNs.Name())
			util.ValidateMetric(t, prom, query, "galley_istio_networking_virtualservices", 4)

			query = fmt.Sprintf("sum(galley_istio_networking_gateways{namespace=\"%s\"})", bookinfoNs.Name())
			util.ValidateMetric(t, prom, query, "galley_istio_networking_gateways", 1)
		})
}
