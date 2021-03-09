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

package migrationca

import (
	"testing"

	"istio.io/istio/pkg/test/echo/common/scheme"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/echoboot"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/framework/label"
	"istio.io/istio/pkg/test/framework/resource"
	"istio.io/istio/tests/integration/security/util"
	"istio.io/istio/tests/integration/security/util/connection"
)

var inst istio.Instance

const (
	ASvc = "a"
	BSvc = "b"
)

func checkConnectivity(t *testing.T, ctx framework.TestContext, a echo.Instances, b echo.Instances) {
	t.Helper()

	callOptions := echo.CallOptions{
		Target:   b[0],
		PortName: "http",
		Scheme:   scheme.HTTP,
		Count:    1,
	}
	checker := connection.Checker{
		From:          a[0],
		Options:       callOptions,
		ExpectSuccess: true,
		DestClusters:  b.Clusters(),
	}
	checker.CheckOrFail(ctx)

	callOptions = echo.CallOptions{
		Target:   a[0],
		PortName: "http",
		Scheme:   scheme.HTTP,
		Count:    1,
	}
	checker = connection.Checker{
		From:          b[0],
		Options:       callOptions,
		ExpectSuccess: true,
		DestClusters:  a.Clusters(),
	}
	checker.CheckOrFail(ctx)
}

func TestIstiodToMeshCAMigration(t *testing.T) {
	framework.NewTest(t).
		RequiresSingleCluster().
		Features("security.migrationca.citadel-meshca").
		Run(func(ctx framework.TestContext) {
			oldCaNamespace := namespace.NewOrFail(t, ctx, namespace.Config{
				Prefix:   "citadel",
				Inject:   true,
				Revision: "asm-revision-istiodca",
			})

			newCaNamespace := namespace.NewOrFail(t, ctx, namespace.Config{
				Prefix:   "meshca",
				Inject:   true,
				Revision: "asm-revision-meshca",
			})

			builder := echoboot.NewBuilder(ctx)
			builder.
				WithClusters(ctx.Clusters()...).
				WithConfig(util.EchoConfig(ASvc, oldCaNamespace, false, nil)).
				WithConfig(util.EchoConfig(BSvc, newCaNamespace, false, nil))

			echos, err := builder.Build()
			if err != nil {
				t.Fatalf("failed to bring up apps for ca_migration: %v", err)
				return
			}
			cluster := ctx.Clusters().Default()
			a := echos.Match(echo.Service(ASvc)).Match(echo.InCluster(cluster))
			b := echos.Match(echo.Service(BSvc)).Match(echo.InCluster(cluster))

			// Test Basic setup
			checkConnectivity(t, ctx, a, b)

			// Migration tests

			// Annotate oldCANamespace with newer CA revision label.
			err = oldCaNamespace.SetLabel("istio.io/rev", "asm-revision-meshca")
			if err != nil {
				t.Fatalf("unable to annotate namespace %v with label %v: %v",
					oldCaNamespace.Name(), "asm-revision-meshca", err)
			}

			// Restart workload A to allow mutating webhook to do its job
			if err := a[0].Restart(); err != nil {
				t.Fatalf("revisioned instance rollout failed with: %v", err)
			}

			// Check mTLS works
			checkConnectivity(t, ctx, a, b)

			// TODO: Handle removal of root of trust
		})
}

func TestMain(t *testing.M) {
	// Integration test for testing migration of workloads from Istiod Ca based control plane to
	// Google Mesh Ca based control plane
	framework.NewSuite(t).
		Label(label.CustomSetup).
		Setup(istio.Setup(&inst, setupConfig)).
		Run()
}

func setupConfig(_ resource.Context, cfg *istio.Config) {
	if cfg == nil {
		return
	}
}
