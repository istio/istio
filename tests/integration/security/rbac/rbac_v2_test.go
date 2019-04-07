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
	inst                 istio.Instance
	successIfAuthEnabled bool
)

func TestMain(m *testing.M) {
	framework.Main("rbac_v2_test", m, istio.SetupOnKube(&inst, setupConfig))
}

func setupConfig(cfg *istio.Config) {
	if cfg == nil {
		return
	}
	if cfg.IsMtlsEnabled() {
		successIfAuthEnabled = true
	} else {
		successIfAuthEnabled = false
	}
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

	connections := []connection.Connection{
		// Port 80 is where HTTP is served, 90 is where TCP is served.
		// In RBAC, ExpectedSuccess = true means that the client receives a valid, non-RBAC denied response from the server.
		{To: appA, From: appB, Port: 80, Protocol: apps.AppProtocolHTTP, Path: "/xyz", ExpectedSuccess: false},
		{To: appA, From: appB, Port: 90, Protocol: apps.AppProtocolTCP, ExpectedSuccess: false},
		{To: appA, From: appC, Port: 80, Protocol: apps.AppProtocolHTTP, Path: "/", ExpectedSuccess: false},
		{To: appA, From: appC, Port: 90, Protocol: apps.AppProtocolTCP, ExpectedSuccess: false},
		{To: appA, From: appD, Port: 80, Protocol: apps.AppProtocolHTTP, Path: "/", ExpectedSuccess: false},
		{To: appA, From: appD, Port: 90, Protocol: apps.AppProtocolTCP, ExpectedSuccess: false},

		{To: appB, From: appA, Port: 80, Protocol: apps.AppProtocolHTTP, Path: "/xyz", ExpectedSuccess: successIfAuthEnabled},
		{To: appB, From: appA, Port: 80, Protocol: apps.AppProtocolHTTP, Path: "/secret", ExpectedSuccess: false},
		{To: appB, From: appA, Port: 90, Protocol: apps.AppProtocolTCP, ExpectedSuccess: successIfAuthEnabled},
		{To: appB, From: appC, Port: 80, Protocol: apps.AppProtocolHTTP, Path: "/", ExpectedSuccess: successIfAuthEnabled},
		{To: appB, From: appC, Port: 90, Protocol: apps.AppProtocolTCP, ExpectedSuccess: successIfAuthEnabled},
		{To: appB, From: appD, Port: 80, Protocol: apps.AppProtocolHTTP, Path: "/", ExpectedSuccess: successIfAuthEnabled},
		{To: appB, From: appD, Port: 90, Protocol: apps.AppProtocolTCP, ExpectedSuccess: successIfAuthEnabled},

		{To: appC, From: appA, Port: 80, Protocol: apps.AppProtocolHTTP, Path: "/", ExpectedSuccess: false},
		{To: appC, From: appA, Port: 80, Protocol: apps.AppProtocolHTTP, Path: "/secrets/admin", ExpectedSuccess: false},
		{To: appC, From: appA, Port: 90, Protocol: apps.AppProtocolTCP, ExpectedSuccess: false},
		{To: appC, From: appB, Port: 80, Protocol: apps.AppProtocolHTTP, Path: "/", ExpectedSuccess: false},
		{To: appC, From: appB, Port: 80, Protocol: apps.AppProtocolHTTP, Path: "/credentials/admin", ExpectedSuccess: false},
		{To: appC, From: appB, Port: 90, Protocol: apps.AppProtocolTCP, ExpectedSuccess: false},
		{To: appC, From: appD, Port: 80, Protocol: apps.AppProtocolHTTP, Path: "/", ExpectedSuccess: successIfAuthEnabled},
		{To: appC, From: appD, Port: 80, Protocol: apps.AppProtocolHTTP, Path: "/any_path/admin", ExpectedSuccess: false},
		{To: appC, From: appD, Port: 90, Protocol: apps.AppProtocolTCP, ExpectedSuccess: false},

		{To: appD, From: appA, Port: 80, Protocol: apps.AppProtocolHTTP, Path: "/xyz", ExpectedSuccess: true},
		{To: appD, From: appA, Port: 90, Protocol: apps.AppProtocolTCP, ExpectedSuccess: false},
		{To: appD, From: appB, Port: 80, Protocol: apps.AppProtocolHTTP, Path: "/", ExpectedSuccess: true},
		{To: appD, From: appB, Port: 90, Protocol: apps.AppProtocolTCP, ExpectedSuccess: false},
		{To: appD, From: appC, Port: 80, Protocol: apps.AppProtocolHTTP, Path: "/any_path", ExpectedSuccess: true},
		{To: appD, From: appC, Port: 90, Protocol: apps.AppProtocolTCP, ExpectedSuccess: false},
	}

	testDir := ctx.WorkDir()
	testNameSpace := appInst.Namespace().Name()
	rbacYamlFiles := getRbacYamlFiles(t, testDir, testNameSpace)

	policy.ApplyPolicyFiles(t, env, testNameSpace, rbacYamlFiles)

	// Sleep 5 seconds for the policy to take effect.
	// TODO(pitlv2109: Check to make sure policies have been created instead.
	time.Sleep(5 * time.Second)
	for _, conn := range connections {
		retry.UntilSuccessOrFail(t, func() error {
			return connection.CheckConnection(t, conn)
		}, retry.Delay(time.Second), retry.Timeout(10*time.Second))
	}
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
