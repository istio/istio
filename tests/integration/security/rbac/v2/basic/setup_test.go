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

package basic

import (
	"testing"

	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/environment"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/util/connection"
)

const (
	rbacClusterConfigTmpl   = "testdata/istio-clusterrbacconfig.yaml.tmpl"
	basicRbacV2RulesTmpl    = "testdata/istio-rbac-v2-rules.yaml.tmpl"
	extendedRbacV2RulesTmpl = "testdata/istio-extended-rbac-v2-rules.yaml.tmpl"
)

var (
	inst             istio.Instance
	isMtlsEnabled    bool
	respCodeFromMtls = map[bool]string{
		true:  connection.AllowHTTPRespCode,
		false: connection.DenyHTTPRespCode,
	}
)

func TestMain(m *testing.M) {
	framework.
		NewSuite("rbac_v2", m).
		// TODO(pitlv2109: Turn on the presubmit label once the test is stable.
		RequireEnvironment(environment.Kube).
		SetupOnEnv(environment.Kube, istio.Setup(&inst, setupConfig)).
		Run()
}

func setupConfig(cfg *istio.Config) {
	if cfg == nil {
		return
	}
	isMtlsEnabled = cfg.IsMtlsEnabled()
	cfg.Values["sidecarInjectorWebhook.rewriteAppHTTPProbe"] = "true"
}
