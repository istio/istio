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
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"istio.io/istio/pkg/test/env"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/components/istioctl"
	"istio.io/istio/pkg/test/framework/features"
	"istio.io/istio/pkg/test/framework/label"
	"istio.io/istio/pkg/test/scopes"
	"istio.io/istio/pkg/test/util/retry"
)

// GatewayTest tests that istio ingress gateway can be started successfully in remote cluster
func GatewayTest(t *testing.T, feature features.Feature) {
	framework.NewTest(t).
		Label(label.Multicluster).
		Features(feature).
		Run(func(ctx framework.TestContext) {
			ctx.NewSubTest("gateway").
				Run(func(ctx framework.TestContext) {
					cfg, err := istio.DefaultConfig(ctx)
					if err != nil {
						t.Fatalf("error ")
					}
					clusters := ctx.Environment().Clusters()
					args := []string{
						"install", "-f", filepath.Join(env.IstioSrc,
							"tests/integration/multicluster/testdata/gateway.yaml"), "--manifests",
						filepath.Join(env.IstioSrc, "manifests"), "--skip-confirmation",
					}
					for _, cluster := range clusters {
						_, err = cluster.AdmissionregistrationV1().ValidatingWebhookConfigurations().Get(context.TODO(),
							"istiod-istio-system", metav1.GetOptions{})
						if err == nil {
							istioCtl := istioctl.NewOrFail(ctx, ctx, istioctl.Config{Cluster: cluster})
							scopes.Framework.Debugf("cluster Name %s", cluster.Name())
							istioCtl.Invoke(args)
							retry.UntilSuccessOrFail(t, func() error {
								pods, err := cluster.CoreV1().Pods(cfg.SystemNamespace).List(context.TODO(), metav1.ListOptions{})
								if err != nil {
									return err
								}
								if len(pods.Items) == 0 {
									return fmt.Errorf("still waiting the ingress gateway pod to start")
								}
								if pods.Items[0].Status.Phase != v1.PodRunning {
									return fmt.Errorf("still waiting the ingress gateway pod to start")
								}
								return nil
							}, retry.Delay(3*time.Second), retry.Timeout(80*time.Second))
							break
						}

					}
				})
		})
}
