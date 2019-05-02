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

package group

import (
	"testing"
	"time"

	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/label"

	"istio.io/istio/tests/integration/security/rbac/util"

	"istio.io/istio/pkg/test/framework/components/galley"
	"istio.io/istio/pkg/test/framework/components/pilot"

	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/apps"
	"istio.io/istio/pkg/test/framework/components/environment"
	"istio.io/istio/pkg/test/framework/components/environment/kube"
	"istio.io/istio/pkg/test/util/connection"
	"istio.io/istio/pkg/test/util/policy"
	"istio.io/istio/pkg/test/util/retry"
)

const (
	rbacClusterConfigTmpl  = "testdata/istio-clusterrbacconfig.yaml.tmpl"
	rbacGroupListRulesTmpl = "testdata/istio-group-list-rbac-v2-rules.yaml.tmpl"
	// groupsScopeJwt contains the claims:
	// "groups": ["group1", "group2"],
	// "scope": ["scope1", "scope2"].
	groupsScopeJwt = "eyJhbGciOiJSUzI1NiIsImtpZCI6IkRIRmJwb0lVcXJZOHQyenBBMnFYZkNtcj" +
		"VWTzVaRXI0UnpIVV8tZW52dlEiLCJ0eXAiOiJKV1QifQ.eyJleHAiOjM1MzczOTExMDQsImdyb3VwcyI6WyJncm91cD" +
		"EiLCJncm91cDIiXSwiaWF0IjoxNTM3MzkxMTA0LCJpc3MiOiJ0ZXN0aW5nQHNlY3VyZS5pc3Rpby5pbyIsInNjb3BlI" +
		"jpbInNjb3BlMSIsInNjb3BlMiJdLCJzdWIiOiJ0ZXN0aW5nQHNlY3VyZS5pc3Rpby5pbyJ9.EdJnEZSH6X8hcyEii7c" +
		"8H5lnhgjB5dwo07M5oheC8Xz8mOllyg--AHCFWHybM48reunF--oGaG6IXVngCEpVF0_P5DwsUoBgpPmK1JOaKN6_pe" +
		"9sh0ZwTtdgK_RP01PuI7kUdbOTlkuUi2AO-qUyOm7Art2POzo36DLQlUXv8Ad7NBOqfQaKjE9ndaPWT7aexUsBHxmgi" +
		"Gbz1SyLH879f7uHYPbPKlpHU6P9S-DaKnGLaEchnoKnov7ajhrEhGXAQRukhDPKUHO9L30oPIr5IJllEQfHYtt6IZvl" +
		"NUGeLUcif3wpry1R5tBXRicx2sXMQ7LyuDremDbcNy_iE76Upg"
	// noGroupScopeJwt contains no groups and scope claims.
	noGroupScopeJwt = "eyJhbGciOiJSUzI1NiIsImtpZCI6IkRIRmJwb0lVcXJZOHQyenBBMnFYZkNtcj" +
		"VWTzVaRXI0UnpIVV8tZW52dlEiLCJ0eXAiOiJKV1QifQ.eyJleHAiOjQ2ODU5ODk3MDAsImZvbyI6ImJhciIsImlhdC" +
		"I6MTUzMjM4OTcwMCwiaXNzIjoidGVzdGluZ0BzZWN1cmUuaXN0aW8uaW8iLCJzdWIiOiJ0ZXN0aW5nQHNlY3VyZS5pc" +
		"3Rpby5pbyJ9.CfNnxWP2tcnR9q0vxyxweaF3ovQYHYZl82hAUsn21bwQd9zP7c-LS9qd_vpdLG4Tn1A15NxfCjp5f7Q" +
		"NBUo-KC9PJqYpgGbaXhaGx7bEdFWjcwv3nZzvc7M__ZpaCERdwU7igUmJqYGBYQ51vr2njU9ZimyKkfDe3axcyiBZde" +
		"7G6dabliUosJvvKOPcKIWPccCgefSj_GNfwIip3-SsFdlR7BtbVUcqR-yv-XOxJ3Uc1MI0tz3uMiiZcyPV7sNCU4KRn" +
		"emRIMHVOfuvHsU60_GhGbiSFzgPTAa9WTltbnarTbxudb_YEOx12JiwYToeX0DCPb43W1tzIBxgm8NxUg"
	rbacTestRejectionCode = "403"
)

var (
	inst          istio.Instance
	isMtlsEnabled bool
)

func setupConfig(cfg *istio.Config) {
	if cfg == nil {
		return
	}
	isMtlsEnabled = cfg.IsMtlsEnabled()
	cfg.Values["sidecarInjectorWebhook.rewriteAppHTTPProbe"] = "true"
}

func TestMain(m *testing.M) {
	framework.
		NewSuite("rbac_v2_group_list", m).
		RequireEnvironment(environment.Kube).
		Label(label.CustomSetup).
		SetupOnEnv(environment.Kube, istio.Setup(&inst, setupConfig)).
		Run()
}

func TestGroupV2RBAC(t *testing.T) {
	ctx := framework.NewContext(t)
	defer ctx.Done(t)
	// TODO(lei-tang): add the test to the native environment
	ctx.RequireOrSkip(t, environment.Kube)

	env := ctx.Environment().(*kube.Environment)
	g := galley.NewOrFail(t, ctx, galley.Config{})
	p := pilot.NewOrFail(t, ctx, pilot.Config{
		Galley: g,
	})
	appInst := apps.NewOrFail(t, ctx, apps.Config{Pilot: p, Galley: g})

	appA, _ := appInst.GetAppOrFail("a", t).(apps.KubeApp)
	appB, _ := appInst.GetAppOrFail("b", t).(apps.KubeApp)
	appC, _ := appInst.GetAppOrFail("c", t).(apps.KubeApp)

	cases := []util.TestCase{
		// Port 80 is where HTTP is served
		{Request: connection.Connection{To: appB, From: appA, Port: 80, Protocol: apps.AppProtocolHTTP, Path: "/xyz"}, Jwt: noGroupScopeJwt,
			ExpectAllowed: false, RejectionCode: rbacTestRejectionCode},
		{Request: connection.Connection{To: appB, From: appA, Port: 80, Protocol: apps.AppProtocolHTTP, Path: "/xyz"}, Jwt: groupsScopeJwt,
			ExpectAllowed: true, RejectionCode: rbacTestRejectionCode},
		{Request: connection.Connection{To: appC, From: appA, Port: 80, Protocol: apps.AppProtocolHTTP, Path: "/xyz"}, Jwt: noGroupScopeJwt,
			ExpectAllowed: false, RejectionCode: rbacTestRejectionCode},
		{Request: connection.Connection{To: appC, From: appA, Port: 80, Protocol: apps.AppProtocolHTTP, Path: "/xyz"}, Jwt: groupsScopeJwt,
			ExpectAllowed: true, RejectionCode: rbacTestRejectionCode},
	}

	testDir := ctx.WorkDir()
	testNameSpace := appInst.Namespace().Name()
	rbacTmplFiles := []string{rbacClusterConfigTmpl, rbacGroupListRulesTmpl}
	rbacYamlFiles := util.GetRbacYamlFiles(t, testDir, testNameSpace, rbacTmplFiles)

	policy.ApplyPolicyFiles(t, env, testNameSpace, rbacYamlFiles)

	// Sleep 60 seconds for the policy to take effect.
	// TODO(lei-tang): programmatically check that policies have taken effect instead.
	time.Sleep(60 * time.Second)
	for _, tc := range cases {
		retry.UntilSuccessOrFail(t, func() error {
			return util.CheckRBACRequest(tc)
		}, retry.Delay(10*time.Second), retry.Timeout(120*time.Second))
	}
}
