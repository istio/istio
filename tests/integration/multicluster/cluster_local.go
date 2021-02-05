// +build integ
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
	"testing"

	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/features"
	"istio.io/istio/pkg/test/framework/label"
)

// ClusterLocalTest tests that traffic works within a local cluster while in a multicluster configuration
// ClusterLocalNS have been configured in meshConfig.serviceSettings to be clusterLocal.
func ClusterLocalTest(t *testing.T, apps AppContext, features ...features.Feature) {
	framework.NewTest(t).
		Features(features...).
		Run(func(ctx framework.TestContext) {
			ctx.NewSubTest("respect-cluster-local-config").Run(func(ctx framework.TestContext) {
				for _, c := range ctx.Clusters() {
					c := c
					ctx.NewSubTest(c.StableName()).
						Label(label.Multicluster).
						Run(func(ctx framework.TestContext) {
							local := apps.LocalEchos.GetOrFail(ctx, echo.InCluster(c))
							callOrFail(ctx, local, local, echo.ExpectCluster(c.Name()))
						})
				}
			})
		})
}
