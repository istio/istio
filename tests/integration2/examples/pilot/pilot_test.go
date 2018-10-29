//  Copyright 2018 Istio Authors
//
//  Licensed under the Apache License, Version 2.0 (the "License");
//  you may not use this file except in compliance with the License.
//  You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
//  Unless required by applicable law or agreed to in writing, software
//  distributed under the License is distributed on an "AS IS" BASIS,
//  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//  See the License for the specific language governing permissions and
//  limitations under the License.

package pilot

import (
	"fmt"
	"testing"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/dependency"
	"istio.io/istio/pkg/test/framework/environment"
)

var (
	reportTemplate = `
apiVersion: "config.istio.io/v1alpha2"
kind: metric
metadata:
  name: metric1
  namespace: {{.DependencyNamespace}}
spec:
  value: "2"
  dimensions:
    requestId: request.headers["x-request-id"]
    host: request.host
    protocol: context.protocol
    responseCode: response.code
---
apiVersion: "config.istio.io/v1alpha2"
kind: rule
metadata:
  name: rule1
  namespace: {{.DependencyNamespace}}
spec:
  actions:
  - handler: handler1.bypass
    instances:
    - metric1.metric
`
)

func TestHTTP(t *testing.T) {
	framework.Requires(t, dependency.Apps)

	testHTTP(t)
}

func TestHTTPKubernetes(t *testing.T) {
	framework.Requires(t, dependency.Kubernetes, dependency.Apps)

	testHTTP(t)
}

func TestHTTPLocal(t *testing.T) {
	framework.Requires(t, dependency.PolicyBackend, dependency.Local, dependency.Apps)

	// TODO(nmittler): When k8s deployment is supported for apps, enable policy checking for both local and k8s.
	env := framework.AcquireEnvironment(t)

	a := env.GetAppOrFail("a", t)
	b := env.GetAppOrFail("b", t)
	telemetry := env.GetPolicyBackendOrFail(t)

	env.Configure(t,
		test.JoinConfigs(
			env.Evaluate(t, reportTemplate),
			telemetry.CreateConfigSnippet("handler1"),
		))

	be := b.EndpointsForProtocol(model.ProtocolHTTP)[0]
	result := a.CallOrFail(be, environment.AppCallOptions{}, t)[0]

	if !result.IsOK() {
		t.Fatalf("HTTP Request unsuccessful: %s", result.Body)
	}

	expected := env.Evaluate(t, fmt.Sprintf(`
{
  "name":"metric1.metric.{{.DependencyNamespace}}",
  "value":{"int64Value":"2"},
  "dimensions":{
    "requestId":{"stringValue":"%s"},
    "host":{"stringValue":"b"},
    "protocol":{"stringValue":"http"},
    "responseCode":{"int64Value":"200"}
   }
}`, result.ID))

	telemetry.ExpectReportJSON(t, expected)
}

func testHTTP(t *testing.T) {
	t.Helper()

	env := framework.AcquireEnvironment(t)

	a := env.GetAppOrFail("a", t)
	b := env.GetAppOrFail("b", t)

	be := b.EndpointsForProtocol(model.ProtocolHTTP)[0]
	result := a.CallOrFail(be, environment.AppCallOptions{}, t)[0]

	if !result.IsOK() {
		t.Fatalf("HTTP Request unsuccessful: %s", result.Body)
	}
}

// Capturing TestMain allows us to:
// - Do cleanup before exit
// - process testing specific flags
func TestMain(m *testing.M) {
	framework.Run("pilot_test", m)
}
