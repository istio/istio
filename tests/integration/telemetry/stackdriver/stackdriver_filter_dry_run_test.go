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
	"net/http"
	"testing"

	"golang.org/x/sync/errgroup"

	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/stackdriver"
	"istio.io/istio/pkg/test/util/file"
	"istio.io/istio/pkg/test/util/retry"
	"istio.io/istio/pkg/test/util/tmpl"
	"istio.io/istio/tests/integration/security/util"
	"istio.io/istio/tests/integration/telemetry"
)

const (
	dryRunAuthorizationPolicyAllow      = "testdata/security_authz_dry_run/policy_allow.yaml.tmpl"
	dryRunAuthorizationPolicyDeny       = "testdata/security_authz_dry_run/policy_deny.yaml.tmpl"
	dryRunServerLogEntryAllowNoPolicy   = "testdata/security_authz_dry_run/server_access_log_allow_no_policy.json.tmpl"
	dryRunServerLogEntryAllowWithPolicy = "testdata/security_authz_dry_run/server_access_log_allow_with_policy.json.tmpl"
	dryRunServerLogEntryDenyNoPolicy    = "testdata/security_authz_dry_run/server_access_log_deny_no_policy.json.tmpl"
	dryRunServerLogEntryDenyWithPolicy  = "testdata/security_authz_dry_run/server_access_log_deny_with_policy.json.tmpl"
	dryRunTCPAuthorizationPolicy        = "testdata/security_authz_dry_run/tcp_authorization_policy.yaml.tmpl"
	dryRunTCPServerLogEntry             = "testdata/security_authz_dry_run/tcp_server_access_log.json.tmpl"
)

type dryRunCase struct {
	name    string
	headers http.Header
	wantLog string
}

func testDryRun(t *testing.T, policies []string, cases []dryRunCase) {
	framework.NewTest(t).
		Features("observability.telemetry.stackdriver").
		Run(func(ctx framework.TestContext) {
			for _, policy := range policies {
				createDryRunPolicy(t, ctx, policy)
			}
			for _, tc := range cases {
				t.Run(tc.name, func(t *testing.T) {
					g, _ := errgroup.WithContext(context.Background())
					for _, cltInstance := range clt {
						cltInstance := cltInstance
						g.Go(func() error {
							err := retry.UntilSuccess(func() error {
								if err := sendTraffic(t, cltInstance, tc.headers); err != nil {
									return err
								}
								return verifyAccessLog(t, cltInstance, tc.wantLog)
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
		})
}

// TestStackdriverAuthzDryRun_Deny verifies that stackdriver WASM filter exports dry-run logs with expected labels
// when there are only deny authorization policy.
func TestStackdriverAuthzDryRun_Deny(t *testing.T) {
	// DENY policy:
	// 1. matched    -> AuthzDenied + deny policy name
	// 2. notMatched -> AuthzAllowed
	testDryRun(t, []string{dryRunAuthorizationPolicyDeny}, []dryRunCase{
		{
			name:    "matched",
			headers: http.Header{"Dry-Run-Deny": []string{"matched"}},
			wantLog: dryRunServerLogEntryDenyWithPolicy,
		},
		{
			name:    "notMatched",
			headers: http.Header{"Dry-Run-Deny": []string{"notMatched"}},
			wantLog: dryRunServerLogEntryAllowNoPolicy,
		},
	})
}

// TestStackdriverAuthzDryRun_Allow verifies that stackdriver WASM filter exports dry-run logs with expected labels
// when there are only allow authorization policy.
func TestStackdriverAuthzDryRun_Allow(t *testing.T) {
	// ALLOW policy:
	// 1. matched    -> AuthzAllowed + allow policy name
	// 2. notMatched -> AuthzDenied
	testDryRun(t, []string{dryRunAuthorizationPolicyAllow}, []dryRunCase{
		{
			name:    "matched",
			headers: http.Header{"Dry-Run-Allow": []string{"matched"}},
			wantLog: dryRunServerLogEntryAllowWithPolicy,
		},
		{
			name:    "notMatched",
			headers: http.Header{"Dry-Run-Allow": []string{"notMatched"}},
			wantLog: dryRunServerLogEntryDenyNoPolicy,
		},
	})
}

// TestStackdriverAuthzDryRun_DenyAndAllow verifies that stackdriver WASM filter exports dry-run logs with expected labels
// when there are both allow and deny authorization policy.
func TestStackdriverAuthzDryRun_DenyAndAllow(t *testing.T) {
	// DENY and ALLOW policy:
	// 1. DENY matched, ALLOW matched       -> AuthzDenied + deny policy name
	// 2. DENY matched, ALLOW notMatched    -> AuthzDenied + deny policy name
	// 3. DENY notMatched, ALLOW matched    -> AuthzAllowed + allow policy name
	// 4. DENY notMatched, ALLOW notMatched -> AuthzDenied
	testDryRun(t, []string{dryRunAuthorizationPolicyDeny, dryRunAuthorizationPolicyAllow}, []dryRunCase{
		{
			name:    "matchedBoth",
			headers: http.Header{"Dry-Run-Deny": []string{"matched"}, "Dry-Run-Allow": []string{"matched"}},
			wantLog: dryRunServerLogEntryDenyWithPolicy,
		},
		{
			name:    "matchedDeny",
			headers: http.Header{"Dry-Run-Deny": []string{"matched"}, "Dry-Run-Allow": []string{"notMatched"}},
			wantLog: dryRunServerLogEntryDenyWithPolicy,
		},
		{
			name:    "matchedAllow",
			headers: http.Header{"Dry-Run-Deny": []string{"notMatched"}, "Dry-Run-Allow": []string{"matched"}},
			wantLog: dryRunServerLogEntryAllowWithPolicy,
		},
		{
			name:    "notMatched",
			headers: http.Header{"Dry-Run-Deny": []string{"notMatched"}, "Dry-Run-Allow": []string{"notMatched"}},
			wantLog: dryRunServerLogEntryDenyNoPolicy,
		},
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

func createDryRunPolicy(t *testing.T, ctx framework.TestContext, authz string) {
	ns := getEchoNamespaceInstance()
	policies := tmpl.EvaluateAllOrFail(t, map[string]string{"Namespace": ns.Name()}, file.AsStringOrFail(t, authz))
	ctx.Config().ApplyYAMLOrFail(t, ns.Name(), policies...)
	util.WaitForConfig(ctx, ns, policies...)
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
