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
package tests

import (
	"testing"

	"istio.io/istio/pkg/test/istio.io/examples"

	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/environment"

	"istio.io/istio/pkg/test/framework/components/istio"
)

var (
	ist istio.Instance
)

func TestMain(m *testing.M) {
	framework.NewSuite("mtls-migration", m).
		SetupOnEnv(environment.Kube, istio.Setup(&ist, nil)).
		RequireEnvironment(environment.Kube).
		Run()
}

//https://istio.io/docs/tasks/security/mtls-migration/
//https://github.com/istio/istio.io/blob/release-1.2/content/docs/tasks/security/mtls-migration/index.md
func TestMTLS(t *testing.T) {
	ex := examples.New(t, "mtls-migration")

	//The following line is just an example of how to use addfile.
	ex.AddFile("istio-system", "samples/sleep/sleep.yaml")
	ex.AddScript("", "create-ns-foo-bar.sh", examples.TextOutput)

	ex.AddScript("", "curl-foo-bar-legacy.sh", examples.TextOutput)

	//verify that all requests returns 200 ok

	ex.AddScript("", "verify-initial-policies.sh", examples.TextOutput)

	//verify that only the following exist:
	// NAMESPACE      NAME                          AGE
	// istio-system   grafana-ports-mtls-disabled   3m

	ex.AddScript("", "verify-initial-destinationrules.sh", examples.TextOutput)

	//verify that only the following exists:
	//NAMESPACE      NAME              AGE
	//istio-system   istio-policy      25m
	//istio-system   istio-telemetry   25m

	ex.AddScript("", "configure-mtls-destinationrule.sh", examples.TextOutput)
	ex.AddScript("", "curl-foo-bar-legacy.sh", examples.TextOutput)

	//verify 200ok from all requests

	ex.AddScript("", "httpbin-foo-mtls-only.sh", examples.TextOutput)
	ex.AddScript("", "curl-foo-bar-legacy.sh", examples.TextOutput)

	//verify 200 from first 2 requests and 503 from 3rd request

	ex.AddScript("", "cleanup.sh", examples.TextOutput)
	ex.Run()
}
