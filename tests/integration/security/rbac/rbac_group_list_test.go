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

package rbac

import (
	"testing"
	"time"

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
	rbacGroupListRulesTmpl = "testdata/grouplist/istio-group-list-rbac-rules.yaml.tmpl"
	groupsScopeJwt         = "eyJhbGciOiJSUzI1NiIsImtpZCI6IkRIRmJwb0lVcXJZOHQyenBBMnFYZkNtcj" +
		"VWTzVaRXI0UnpIVV8tZW52dlEiLCJ0eXAiOiJKV1QifQ.eyJleHAiOjM1MzczOTExMDQsImdyb3VwcyI6WyJncm91cD" +
		"EiLCJncm91cDIiXSwiaWF0IjoxNTM3MzkxMTA0LCJpc3MiOiJ0ZXN0aW5nQHNlY3VyZS5pc3Rpby5pbyIsInNjb3BlI" +
		"jpbInNjb3BlMSIsInNjb3BlMiJdLCJzdWIiOiJ0ZXN0aW5nQHNlY3VyZS5pc3Rpby5pbyJ9.EdJnEZSH6X8hcyEii7c" +
		"8H5lnhgjB5dwo07M5oheC8Xz8mOllyg--AHCFWHybM48reunF--oGaG6IXVngCEpVF0_P5DwsUoBgpPmK1JOaKN6_pe" +
		"9sh0ZwTtdgK_RP01PuI7kUdbOTlkuUi2AO-qUyOm7Art2POzo36DLQlUXv8Ad7NBOqfQaKjE9ndaPWT7aexUsBHxmgi" +
		"Gbz1SyLH879f7uHYPbPKlpHU6P9S-DaKnGLaEchnoKnov7ajhrEhGXAQRukhDPKUHO9L30oPIr5IJllEQfHYtt6IZvl" +
		"NUGeLUcif3wpry1R5tBXRicx2sXMQ7LyuDremDbcNy_iE76Upg"
	rbacTestRejectionCode = "401"
)

func TestGroupListRBAC(t *testing.T) {
	ctx := framework.NewContext(t)
	defer ctx.Done(t)
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

	cases := []testCase{
		// Port 80 is where HTTP is served
		{request: connection.Connection{To: appB, From: appA, Port: 80, Protocol: apps.AppProtocolHTTP, Path: "/xyz"}, expectAllowed: false},
		{request: connection.Connection{To: appB, From: appA, Port: 80, Protocol: apps.AppProtocolHTTP, Path: "/xyz"}, jwt: groupsScopeJwt, expectAllowed: true},
		{request: connection.Connection{To: appC, From: appA, Port: 80, Protocol: apps.AppProtocolHTTP, Path: "/xyz"}, expectAllowed: false},
		{request: connection.Connection{To: appC, From: appA, Port: 80, Protocol: apps.AppProtocolHTTP, Path: "/xyz"}, jwt: groupsScopeJwt, expectAllowed: true},
	}

	testDir := ctx.WorkDir()
	testNameSpace := appInst.Namespace().Name()
	rbacTmplFiles := []string{rbacClusterConfigTmpl, rbacGroupListRulesTmpl}
	rbacYamlFiles := getRbacYamlFiles(t, testDir, testNameSpace, rbacTmplFiles)

	policy.ApplyPolicyFiles(t, env, testNameSpace, rbacYamlFiles)

	// Sleep 60 seconds for the policy to take effect.
	time.Sleep(60 * time.Second)
	for _, tc := range cases {
		retry.UntilSuccessOrFail(t, func() error {
			return checkRBACRequest(tc, rbacTestRejectionCode)
		}, retry.Delay(time.Second), retry.Timeout(30*time.Second))
	}
}
