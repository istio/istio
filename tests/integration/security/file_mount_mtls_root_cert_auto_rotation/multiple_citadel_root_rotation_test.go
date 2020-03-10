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

package filemountmtlsrootcertautorotation

import (
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"istio.io/istio/pkg/test/echo/common/scheme"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/echoboot"
	"istio.io/istio/pkg/test/framework/components/environment"
	"istio.io/istio/pkg/test/framework/components/environment/kube"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/util/retry"
	"istio.io/istio/tests/integration/security/util"
	"istio.io/istio/tests/integration/security/util/connection"
)

var (
	citadelDeployName = "istio-citadel"
	citadelReplica    = 5
)

// Deploy multiple Citadels, each has jitter enabled by default and permission to
// rotate root cert. Verifies mTLS between workloads.
func TestMtlsWithMultipleCitadel(t *testing.T) {
	framework.NewTest(t).
		RequiresEnvironment(environment.Kube).
		Run(func(ctx framework.TestContext) {
			istioCfg := istio.DefaultConfigOrFail(t, ctx)

			// Scale Citadel deployment
			cluster := ctx.Environment().(*kube.Environment).KubeClusters[0]
			scaleDeployment(istioCfg.SystemNamespace, citadelDeployName, citadelReplica, t, cluster)

			namespace.ClaimOrFail(t, ctx, istioCfg.SystemNamespace)
			ns := namespace.NewOrFail(t, ctx, namespace.Config{
				Prefix: "multiple-citadel-root-cert-rotation-with-jitter",
				Inject: true,
			})

			var a, b echo.Instance
			echoboot.NewBuilderOrFail(t, ctx).
				With(&a, util.EchoConfig("a", ns, false, nil, g, p)).
				With(&b, util.EchoConfig("b", ns, false, nil, g, p)).
				BuildOrFail(t)

			checkers := []connection.Checker{
				{
					From: a,
					Options: echo.CallOptions{
						Target:   b,
						PortName: "http",
						Scheme:   scheme.HTTP,
					},
					ExpectSuccess: true,
				},
			}
			// Get initial root cert.
			systemNS := namespace.ClaimOrFail(t, ctx, istioCfg.SystemNamespace)
			// Wait for at least one root rotation to let workloads use new CA cert
			// to set up mTLS connections.
			for i := 0; i < 5; i++ {
				caScrt, err := cluster.GetSecret(systemNS.Name()).Get(CASecret, metav1.GetOptions{})
				if err != nil {
					t.Fatalf("unable to load root secret: %s", err.Error())
				}
				for _, checker := range checkers {
					retry.UntilSuccessOrFail(t, checker.Check, retry.Delay(time.Second), retry.Timeout(10*time.Second))
				}
				err = waitUntilRootCertRotate(t, caScrt, cluster, systemNS.Name(), 40*time.Second)
				if err != nil {
					t.Errorf("Root cert is not rotated: %s", err.Error())
				}
			}
			// Restore to one Citadel for other tests.
			scaleDeployment(istioCfg.SystemNamespace, citadelDeployName, 1, t, cluster)
		})
}

func scaleDeployment(namespace, deployment string, replicas int, t *testing.T, cluster kube.Cluster) {
	if err := cluster.ScaleDeployment(namespace, deployment, replicas); err != nil {
		t.Fatalf("Error scaling deployment %s to %d: %v", deployment, replicas, err)
	}
	if err := cluster.WaitUntilDeploymentIsReady(namespace, deployment); err != nil {
		t.Fatalf("Error waiting for deployment %s to be ready: %v", deployment, err)
	}
}
