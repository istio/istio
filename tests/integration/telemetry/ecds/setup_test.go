//go:build integ
// +build integ

// Copyright Istio Authors. All Rights Reserved.
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

package ecds

import (
	"context"
	"fmt"
	"testing"

	"golang.org/x/sync/errgroup"

	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/check"
	echodeployment "istio.io/istio/pkg/test/framework/components/echo/common/deployment"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/components/istioctl"
	"istio.io/istio/pkg/test/framework/components/prometheus"
	"istio.io/istio/pkg/test/framework/label"
	"istio.io/istio/pkg/test/framework/resource"
	"istio.io/istio/pkg/test/util/retry"
	util "istio.io/istio/tests/integration/telemetry"
)

var (
	apps echodeployment.SingleNamespaceView

	ist      istio.Instance
	promInst prometheus.Instance
)

func TestMain(m *testing.M) {
	framework.NewSuite(m).
		Label(label.CustomSetup).
		// TODO: Remove this restriction once we validate our prometheus helm chart version is high enough
		Label(label.IPv4). // https://github.com/istio/istio/issues/35915
		Setup(istio.Setup(&ist, setupConfig)).
		Setup(SetupSuite).
		Run()
}

func setupConfig(_ resource.Context, cfg *istio.Config) {
	if cfg == nil {
		return
	}
	cfg.ControlPlaneValues = `
values:
  pilot:
    env:
      ENABLE_ECDS_FOR_STATS: "true"
`
}

// SetupSuite set up echo app for stats testing.
func SetupSuite(ctx resource.Context) (err error) {
	echos := (&echodeployment.Config{}).DefaultEchoConfigs(ctx)
	for _, e := range echos {
		if e.Subsets[0].Annotations == nil {
			e.Subsets[0].Annotations = map[echo.Annotation]*echo.AnnotationValue{}
		}
	}

	if err := echodeployment.SetupSingleNamespace(&apps, echodeployment.Config{Configs: echo.ConfigFuture(&echos)})(ctx); err != nil {
		return err
	}

	if err != nil {
		return err
	}
	promInst, err = prometheus.New(ctx, prometheus.Config{})
	if err != nil {
		return
	}
	return nil
}

func TestStats(t *testing.T) {
	framework.NewTest(t).
		Features("observability.telemetry.stats.ecds").
		Run(func(t framework.TestContext) {
			istioCtl := istioctl.NewOrFail(t, t, istioctl.Config{})
			g, _ := errgroup.WithContext(context.Background())
			for _, item := range apps.A {
				from := item

				podID, err := getPodID(from)
				if err != nil {
					t.Fatalf("failed to get pod ID: %v", err)
				}
				args := []string{
					"pc", "ecds", fmt.Sprintf("%s.%s", podID, apps.Namespace.Name()),
				}
				// ECDS is enabled for stats, so we should see the ECDS config dump
				_, _ = istioCtl.InvokeOrFail(t, args)

				g.Go(func() error {
					err := retry.UntilSuccess(func() error {
						if err := SendTraffic(from); err != nil {
							return err
						}
						c := from.Config().Cluster
						sourceCluster := "Kubernetes"
						if len(t.AllClusters()) > 1 {
							sourceCluster = c.Name()
						}
						sourceQuery, destinationQuery, appQuery := util.BuildQuery(apps.Namespace, sourceCluster)
						// Query client side metrics
						prom := promInst
						if _, err := prom.QuerySum(c, sourceQuery); err != nil {
							util.PromDiff(t, prom, c, sourceQuery)
							return err
						}
						// Query client side metrics for non-injected server
						outOfMeshServerQuery := util.BuildOutOfMeshServerQuery(apps.Namespace, sourceCluster)
						if _, err := prom.QuerySum(c, outOfMeshServerQuery); err != nil {
							util.PromDiff(t, prom, c, outOfMeshServerQuery)
							return err
						}
						// Query server side metrics.
						if _, err := prom.QuerySum(c, destinationQuery); err != nil {
							util.PromDiff(t, prom, c, destinationQuery)
							return err
						}
						// This query will continue to increase due to readiness probe; don't wait for it to converge
						if _, err := prom.QuerySum(c, appQuery); err != nil {
							util.PromDiff(t, prom, c, appQuery)
							return err
						}
						return nil
					}, retry.Delay(framework.TelemetryRetryDelay), retry.Timeout(framework.TelemetryRetryTimeout))
					if err != nil {
						return err
					}
					return nil
				})
			}
			if err := g.Wait(); err != nil {
				t.Fatalf("test failed: %v", err)
			}
		})
}

// SendTraffic makes a client call to the "server" service on the http port.
func SendTraffic(from echo.Instance) error {
	_, err := from.Call(echo.CallOptions{
		To: apps.B,
		Port: echo.Port{
			Name: "http",
		},
		Check: check.OK(),
		Retry: echo.Retry{
			NoRetry: true,
		},
	})
	if err != nil {
		return err
	}
	_, err = from.Call(echo.CallOptions{
		To: apps.Naked,
		Port: echo.Port{
			Name: "http",
		},
		Retry: echo.Retry{
			NoRetry: true,
		},
	})
	if err != nil {
		return err
	}
	return nil
}

func getPodID(i echo.Instance) (string, error) {
	wls, err := i.Workloads()
	if err != nil {
		return "", nil
	}

	for _, wl := range wls {
		return wl.PodName(), nil
	}

	return "", fmt.Errorf("no workloads")
}
