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
	"fmt"
	"strings"
	"testing"
	"time"

	"istio.io/istio/pkg/test/framework/components/galley"
	"istio.io/istio/pkg/test/framework/components/pilot"

	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/apps"
	"istio.io/istio/pkg/test/framework/components/environment"
	"istio.io/istio/pkg/test/framework/components/environment/kube"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/util/connection"
	"istio.io/istio/pkg/test/util/policy"
	"istio.io/istio/pkg/test/util/retry"
	"istio.io/istio/tests/util"
)

const (
	rbacClusterConfigTmpl = "testdata/istio-clusterrbacconfig.yaml.tmpl"
	rbacV2RulesTmpl       = "testdata/istio-rbac-v2-rules.yaml.tmpl"
)

var (
	inst          istio.Instance
	isMtlsEnabled bool
)

type testCase struct {
	request       connection.Connection
	expectAllowed bool
}

func TestMain(m *testing.M) {
	framework.
		NewSuite("rbac_v2", m).
		// TODO(pitlv2109: Turn on the presubmit label once the test is stable.
		RequireEnvironment(environment.Kube).
		Setup(istio.SetupOnKube(&inst, setupConfig)).
		Run()
}

func setupConfig(cfg *istio.Config) {
	if cfg == nil {
		return
	}
	isMtlsEnabled = cfg.IsMtlsEnabled()
	cfg.Values["sidecarInjectorWebhook.rewriteAppHTTPProbe"] = "true"
}

func TestRBACV2(t *testing.T) {
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
	appD, _ := appInst.GetAppOrFail("d", t).(apps.KubeApp)

	cases := []testCase{
		// Port 80 is where HTTP is served, 90 is where TCP is served. When an HTTP request is at port
		// 90, this means it is a TCP request. The test framework uses HTTP to mimic TCP calls in this case.
		{request: connection.Connection{To: appA, From: appB, Port: 80, Protocol: apps.AppProtocolHTTP, Path: "/xyz"}, expectAllowed: false},
		{request: connection.Connection{To: appA, From: appB, Port: 90, Protocol: apps.AppProtocolHTTP}, expectAllowed: false},
		{request: connection.Connection{To: appA, From: appC, Port: 80, Protocol: apps.AppProtocolHTTP, Path: "/"}, expectAllowed: false},
		{request: connection.Connection{To: appA, From: appC, Port: 90, Protocol: apps.AppProtocolHTTP}, expectAllowed: false},
		{request: connection.Connection{To: appA, From: appD, Port: 80, Protocol: apps.AppProtocolHTTP, Path: "/"}, expectAllowed: false},
		{request: connection.Connection{To: appA, From: appD, Port: 90, Protocol: apps.AppProtocolHTTP}, expectAllowed: false},

		{request: connection.Connection{To: appB, From: appA, Port: 80, Protocol: apps.AppProtocolHTTP, Path: "/xyz"}, expectAllowed: isMtlsEnabled},
		{request: connection.Connection{To: appB, From: appA, Port: 80, Protocol: apps.AppProtocolHTTP, Path: "/secret"}, expectAllowed: false},
		{request: connection.Connection{To: appB, From: appA, Port: 90, Protocol: apps.AppProtocolHTTP}, expectAllowed: isMtlsEnabled},
		{request: connection.Connection{To: appB, From: appC, Port: 80, Protocol: apps.AppProtocolHTTP, Path: "/"}, expectAllowed: isMtlsEnabled},
		{request: connection.Connection{To: appB, From: appC, Port: 90, Protocol: apps.AppProtocolHTTP}, expectAllowed: isMtlsEnabled},
		{request: connection.Connection{To: appB, From: appD, Port: 80, Protocol: apps.AppProtocolHTTP, Path: "/"}, expectAllowed: isMtlsEnabled},
		{request: connection.Connection{To: appB, From: appD, Port: 90, Protocol: apps.AppProtocolHTTP}, expectAllowed: isMtlsEnabled},

		{request: connection.Connection{To: appC, From: appA, Port: 80, Protocol: apps.AppProtocolHTTP, Path: "/"}, expectAllowed: false},
		{request: connection.Connection{To: appC, From: appA, Port: 80, Protocol: apps.AppProtocolHTTP, Path: "/secrets/admin"}, expectAllowed: false},
		{request: connection.Connection{To: appC, From: appA, Port: 90, Protocol: apps.AppProtocolHTTP}, expectAllowed: false},
		{request: connection.Connection{To: appC, From: appB, Port: 80, Protocol: apps.AppProtocolHTTP, Path: "/"}, expectAllowed: false},
		{request: connection.Connection{To: appC, From: appB, Port: 80, Protocol: apps.AppProtocolHTTP, Path: "/credentials/admin"}, expectAllowed: false},
		{request: connection.Connection{To: appC, From: appB, Port: 90, Protocol: apps.AppProtocolHTTP}, expectAllowed: false},
		{request: connection.Connection{To: appC, From: appD, Port: 80, Protocol: apps.AppProtocolHTTP, Path: "/"}, expectAllowed: isMtlsEnabled},
		{request: connection.Connection{To: appC, From: appD, Port: 80, Protocol: apps.AppProtocolHTTP, Path: "/any_path/admin"}, expectAllowed: false},
		{request: connection.Connection{To: appC, From: appD, Port: 90, Protocol: apps.AppProtocolHTTP}, expectAllowed: false},

		{request: connection.Connection{To: appD, From: appA, Port: 80, Protocol: apps.AppProtocolHTTP, Path: "/xyz"}, expectAllowed: true},
		{request: connection.Connection{To: appD, From: appA, Port: 90, Protocol: apps.AppProtocolHTTP}, expectAllowed: false},
		{request: connection.Connection{To: appD, From: appB, Port: 80, Protocol: apps.AppProtocolHTTP, Path: "/"}, expectAllowed: true},
		{request: connection.Connection{To: appD, From: appB, Port: 90, Protocol: apps.AppProtocolHTTP}, expectAllowed: false},
		{request: connection.Connection{To: appD, From: appC, Port: 80, Protocol: apps.AppProtocolHTTP, Path: "/any_path"}, expectAllowed: true},
		{request: connection.Connection{To: appD, From: appC, Port: 90, Protocol: apps.AppProtocolHTTP}, expectAllowed: false},
	}

	testDir := ctx.WorkDir()
	testNameSpace := appInst.Namespace().Name()
	rbacYamlFiles := getRbacYamlFiles(t, testDir, testNameSpace)

	policy.ApplyPolicyFiles(t, env, testNameSpace, rbacYamlFiles)

	// Sleep 5 seconds for the policy to take effect.
	// TODO(pitlv2109: Check to make sure policies have been created instead.
	time.Sleep(5 * time.Second)
	for _, tc := range cases {
		retry.UntilSuccessOrFail(t, func() error {
			return checkRBACRequest(tc)
		}, retry.Delay(time.Second), retry.Timeout(10*time.Second))
	}
}

// checkRBACRequest checks if a request is successful under RBAC policies.
// Under RBAC policies, a request is consider successful if:
// * If the policy is deny:
// *** For HTTP: response code is 403
// *** For TCP: EOF error
// * If the policy is allow:
// *** Response code is 200
func checkRBACRequest(tc testCase) error {
	req := tc.request
	ep := req.To.EndpointForPort(req.Port)
	if ep == nil {
		return fmt.Errorf("cannot get upstream endpoint for connection test %v", req)
	}
	resp, err := req.From.Call(ep, apps.AppCallOptions{Protocol: req.Protocol, Path: req.Path})
	if err != nil && !strings.Contains(err.Error(), "EOF") {
		return fmt.Errorf("connection error with %v", err)
	}
	if tc.expectAllowed {
		if !(len(resp) > 0 && resp[0].Code == connection.AllowHTTPRespCode) {
			return fmt.Errorf("%s to %s:%d%s using %s: expected allow, actually deny",
				req.From.Name(), req.To.Name(), req.Port, req.Path, req.Protocol)
		}
	} else {
		if req.Port == connection.TCPPort {
			if !strings.Contains(err.Error(), "EOF") {
				return fmt.Errorf("%s to %s:%d%s using %s: expected deny with EOF error, actually %v",
					req.From.Name(), req.To.Name(), req.Port, req.Path, req.Protocol, err)
			}
		} else {
			if !(len(resp) > 0 && resp[0].Code == connection.DenyHTTPRespCode) {
				return fmt.Errorf("%s to %s:%d%s using %s: expected deny, actually allow",
					req.From.Name(), req.To.Name(), req.Port, req.Path, req.Protocol)
			}
		}
	}
	// Success
	return nil
}

// getRbacYamlFiles fills the template RBAC policy files with the given namespace, writes the files to outDir
// and return the list of file paths.
func getRbacYamlFiles(t *testing.T, outDir, namespace string) []string {
	var rbacYamlFiles []string
	namespaceParams := map[string]string{
		"Namespace": namespace,
	}
	rbacTmplFiles := []string{rbacClusterConfigTmpl, rbacV2RulesTmpl}
	for _, rbacTmplFile := range rbacTmplFiles {
		yamlFile, err := util.CreateAndFill(outDir, rbacTmplFile, namespaceParams)
		if err != nil {
			t.Fatalf("Cannot create and fill %v", rbacTmplFile)
		}
		rbacYamlFiles = append(rbacYamlFiles, yamlFile)
	}
	return rbacYamlFiles
}
