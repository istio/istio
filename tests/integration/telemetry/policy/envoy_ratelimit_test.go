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

package policy

import (
	"os"
	"path/filepath"
	"testing"

	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/test/env"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/check"
	"istio.io/istio/pkg/test/framework/components/echo/deployment"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/components/istio/ingress"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/framework/components/prometheus"
	"istio.io/istio/pkg/test/framework/label"
	"istio.io/istio/pkg/test/framework/resource"
	"istio.io/istio/pkg/test/kube"
	"istio.io/istio/pkg/test/util/tmpl"
)

var (
	ist         istio.Instance
	echoNsInst  namespace.Instance
	ratelimitNs namespace.Instance
	ing         ingress.Instance
	srv         echo.Instance
	clt         echo.Instance
	prom        prometheus.Instance
)

func TestRateLimiting(t *testing.T) {
	framework.
		NewTest(t).
		Run(func(t framework.TestContext) {
			cleanup := setupEnvoyFilter(t, "testdata/enable_envoy_ratelimit.yaml")
			defer cleanup()
			sendTrafficAndCheckIfRatelimited(t)
		})
}

func TestLocalRateLimiting(t *testing.T) {
	framework.
		NewTest(t).
		Run(func(t framework.TestContext) {
			cleanup := setupEnvoyFilter(t, "testdata/enable_envoy_local_ratelimit.yaml")
			defer cleanup()
			sendTrafficAndCheckIfRatelimited(t)
		})
}

func TestLocalRouteSpecificRateLimiting(t *testing.T) {
	framework.
		NewTest(t).
		Run(func(t framework.TestContext) {
			cleanup := setupEnvoyFilter(t, "testdata/enable_envoy_local_ratelimit_per_route.yaml")
			defer cleanup()
			sendTrafficAndCheckIfRatelimited(t)
		})
}

func TestLocalRateLimitingServiceAccount(t *testing.T) {
	framework.
		NewTest(t).
		Run(func(t framework.TestContext) {
			cleanup := setupEnvoyFilter(t, "testdata/enable_envoy_local_ratelimit_sa.yaml")
			defer cleanup()
			sendTrafficAndCheckIfRatelimited(t)
		})
}

func TestMain(m *testing.M) {
	// nolint: staticcheck
	framework.
		NewSuite(m).
		Label(label.CustomSetup).
		Setup(istio.Setup(&ist, nil)).
		Setup(testSetup).
		Setup(setupPrometheus).
		Run()
}

func setupPrometheus(ctx resource.Context) (err error) {
	prom, err = prometheus.New(ctx, prometheus.Config{})
	return err
}

func testSetup(ctx resource.Context) (err error) {
	echoNsInst, err = namespace.New(ctx, namespace.Config{
		Prefix: "istio-echo",
		Inject: true,
	})
	if err != nil {
		return err
	}

	_, err = deployment.New(ctx).
		With(&clt, echo.Config{
			Service:        "clt",
			Namespace:      echoNsInst,
			ServiceAccount: true,
		}).
		With(&srv, echo.Config{
			Service:   "srv",
			Namespace: echoNsInst,
			Ports: []echo.Port{
				{
					Name:     "http",
					Protocol: protocol.HTTP,
					// We use a port > 1024 to not require root
					WorkloadPort: 8888,
				},
			},
			ServiceAccount: true,
		}).
		Build()
	if err != nil {
		return err
	}

	ing = ist.IngressFor(ctx.Clusters().Default())

	ratelimitNs, err = namespace.New(ctx, namespace.Config{
		Prefix: "istio-ratelimit",
	})
	if err != nil {
		return err
	}

	err = ctx.ConfigIstio().File(ratelimitNs.Name(), "testdata/rate-limit-configmap.yaml").Apply()
	if err != nil {
		return err
	}

	err = ctx.ConfigIstio().File(ratelimitNs.Name(), filepath.Join(env.IstioSrc, "samples/ratelimit/rate-limit-service.yaml")).
		Apply()
	if err != nil {
		return err
	}

	// Wait for redis and ratelimit service to be up.
	fetchFn := kube.NewPodFetch(ctx.Clusters().Default(), ratelimitNs.Name(), "app=redis")
	if _, err = kube.WaitUntilPodsAreReady(fetchFn); err != nil {
		return err
	}
	fetchFn = kube.NewPodFetch(ctx.Clusters().Default(), ratelimitNs.Name(), "app=ratelimit")
	if _, err = kube.WaitUntilPodsAreReady(fetchFn); err != nil {
		return err
	}

	return nil
}

func setupEnvoyFilter(ctx framework.TestContext, file string) func() {
	content, err := os.ReadFile(file)
	if err != nil {
		ctx.Fatal(err)
	}

	con, err := tmpl.Evaluate(string(content), map[string]any{
		"EchoNamespace":      echoNsInst.Name(),
		"RateLimitNamespace": ratelimitNs.Name(),
	})
	if err != nil {
		ctx.Fatal(err)
	}

	err = ctx.ConfigIstio().YAML(ist.Settings().SystemNamespace, con).Apply()
	if err != nil {
		ctx.Fatal(err)
	}
	return func() {
		ctx.ConfigIstio().YAML(ist.Settings().SystemNamespace, con).DeleteOrFail(ctx)
	}
}

func sendTrafficAndCheckIfRatelimited(t framework.TestContext) {
	t.Helper()
	clt.CallOrFail(t, echo.CallOptions{
		To: srv,
		Port: echo.Port{
			Name: "http",
		},
		Count: 5,
		Check: check.TooManyRequests(),
	})
}
