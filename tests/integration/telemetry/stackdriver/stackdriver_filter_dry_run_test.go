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

package stackdriver

import (
	"context"
	"testing"

	"golang.org/x/sync/errgroup"

	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/stackdriver"
	telemetrypkg "istio.io/istio/pkg/test/framework/components/telemetry"
	"istio.io/istio/pkg/test/util/file"
	"istio.io/istio/pkg/test/util/retry"
	"istio.io/istio/pkg/test/util/tmpl"
	"istio.io/istio/tests/integration/security/util"
	"istio.io/istio/tests/integration/telemetry"
)

const (
	dryRunServerLogEntry         = "testdata/security_authz_dry_run/server_access_log.json.tmpl"
	dryRunTCPServerLogEntry      = "testdata/security_authz_dry_run/tcp_server_access_log.json.tmpl"
	dryRunAuthorizationPolicy    = "testdata/security_authz_dry_run/authorization_policy.yaml.tmpl"
	dryRunTCPAuthorizationPolicy = "testdata/security_authz_dry_run/tcp_authorization_policy.yaml.tmpl"
)

// TestStackdriverAuthzDryRun verifies that stackdriver WASM filter exports dry-run logs with expected labels for HTTP traffic.
func TestStackdriverAuthzDryRun(t *testing.T) {
	framework.NewTest(t).
		Features("observability.telemetry.stackdriver").
		Run(func(ctx framework.TestContext) {
			createDryRunPolicy(t, ctx, dryRunAuthorizationPolicy)
			g, _ := errgroup.WithContext(context.Background())
			for _, cltInstance := range clt {
				cltInstance := cltInstance
				g.Go(func() error {
					err := retry.UntilSuccess(func() error {
						if err := sendTraffic(t, cltInstance); err != nil {
							return err
						}
						return verifyAccessLog(t, cltInstance, dryRunServerLogEntry)
					}, retry.Delay(telemetrypkg.RetryDelay), retry.Timeout(telemetrypkg.RetryTimeout))
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

// TestTCPStackdriverAuthzDryRun verifies that stackdriver WASM filter exports dry-run logs with expected labels for TCP traffic.
func TestTCPStackdriverAuthzDryRun(t *testing.T) {
	framework.NewTest(t).
		Features("observability.telemetry.stackdriver").
		Run(func(ctx framework.TestContext) {
			createDryRunPolicy(t, ctx, dryRunTCPAuthorizationPolicy)
			g, _ := errgroup.WithContext(context.Background())
			for _, cltInstance := range clt {
				cltInstance := cltInstance
				g.Go(func() error {
					err := retry.UntilSuccess(func() error {
						_, err := cltInstance.Call(echo.CallOptions{
							Target:   srv[0],
							PortName: "tcp",
							Count:    telemetry.RequestCountMultipler * len(srv),
						})
						if err != nil {
							return err
						}
						return verifyAccessLog(t, cltInstance, dryRunTCPServerLogEntry)
					}, retry.Delay(telemetrypkg.RetryDelay), retry.Timeout(telemetrypkg.RetryTimeout))
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

func createDryRunPolicy(t *testing.T, ctx framework.TestContext, authz string) {
	ns := getEchoNamespaceInstance()
	policies := tmpl.EvaluateAllOrFail(t, map[string]string{"Namespace": ns.Name()}, file.AsStringOrFail(t, authz))
	ctx.Config().ApplyYAMLOrFail(t, ns.Name(), policies...)
	util.WaitForConfig(ctx, policies[0], ns)
}

func verifyAccessLog(t *testing.T, cltInstance echo.Instance, wantLog string) error {
	t.Logf("Validating for cluster %v", cltInstance.Config().Cluster)
	clName := cltInstance.Config().Cluster.Name()
	trustDomain := telemetry.GetTrustDomain(cltInstance.Config().Cluster, ist.Settings().SystemNamespace)
	if err := validateLogs(t, wantLog, clName, trustDomain, stackdriver.ServerAccessLog); err != nil {
		return err
	}
	return nil
}
