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
	"istio.io/istio/pkg/test/framework/components/stackdriver"
	telemetrypkg "istio.io/istio/pkg/test/framework/components/telemetry"
	"istio.io/istio/pkg/test/scopes"
	"istio.io/istio/pkg/test/util/file"
	"istio.io/istio/pkg/test/util/retry"
	"istio.io/istio/pkg/test/util/tmpl"
	"istio.io/istio/tests/integration/security/util"
	"istio.io/istio/tests/integration/telemetry"
)

const (
	dryRunServerLogEntry      = "testdata/security_authz_dry_run/server_access_log.json.tmpl"
	dryRunAuthorizationPolicy = "testdata/security_authz_dry_run/authorization_policy.yaml.tmpl"
)

// TestStackdriverAuthzDryRun verifies that stackdriver WASM filter exports dry-run metrics with expected labels.
func TestStackdriverAuthzDryRun(t *testing.T) {
	framework.NewTest(t).
		Features("observability.telemetry.stackdriver").
		Run(func(ctx framework.TestContext) {
			ns := getEchoNamespaceInstance()
			policies := tmpl.EvaluateAllOrFail(t, map[string]string{"Namespace": ns.Name()}, file.AsStringOrFail(t, dryRunAuthorizationPolicy))
			ctx.Config().ApplyYAMLOrFail(t, ns.Name(), policies...)
			util.WaitForConfig(ctx, policies[0], ns)

			g, _ := errgroup.WithContext(context.Background())
			for _, cltInstance := range clt {
				cltInstance := cltInstance
				g.Go(func() error {
					err := retry.UntilSuccess(func() error {
						if err := sendTraffic(t, cltInstance); err != nil {
							return err
						}
						clName := cltInstance.Config().Cluster.Name()
						trustDomain := telemetry.GetTrustDomain(cltInstance.Config().Cluster, ist.Settings().SystemNamespace)
						scopes.Framework.Infof("Validating for cluster %s", clName)
						if err := validateLogs(t, dryRunServerLogEntry, clName, trustDomain, stackdriver.ServerAccessLog); err != nil {
							return err
						}
						t.Logf("logs validated")
						return nil
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
