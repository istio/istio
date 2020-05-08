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

package multicluster

import (
	"fmt"
	"testing"

	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/echoboot"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/framework/components/pilot"
	"istio.io/istio/pkg/test/framework/label"
)

// ClusterLocalTest tests that traffic works within a local cluster while in a multicluster configuration
func ClusterLocalTest(t *testing.T, clusterLocalNS namespace.Instance, pilots []pilot.Instance) {
	framework.NewTest(t).
		Label(label.Multicluster).
		Run(func(ctx framework.TestContext) {
			cluster1 := ctx.Environment().Clusters()[0]
			cluster2 := ctx.Environment().Clusters()[1]

			// Deploy a only in cluster1, but b in both clusters.
			var a1, b1, b2 echo.Instance
			echoboot.NewBuilderOrFail(ctx, ctx).
				With(&a1, newEchoConfig("a", clusterLocalNS, cluster1, pilots)).
				With(&b1, newEchoConfig("b", clusterLocalNS, cluster1, pilots)).
				With(&b2, newEchoConfig("b", clusterLocalNS, cluster2, pilots)).
				BuildOrFail(ctx)

			results := callOrFail(ctx, a1, b1)

			// Ensure that all requests went to cluster 1.
			results.CheckClusterOrFail(ctx, fmt.Sprintf("%d", cluster1.Index()))
		})
}
