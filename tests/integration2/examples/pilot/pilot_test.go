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

	"istio.io/istio/pkg/test/framework/runtime/components/environment/kube"

	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/api/components"
	"istio.io/istio/pkg/test/framework/api/context"
	"istio.io/istio/pkg/test/framework/api/descriptors"
	"istio.io/istio/pkg/test/framework/api/ids"
	"istio.io/istio/pkg/test/framework/api/lifecycle"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/test"
)

var (
	reportTemplate = `
apiVersion: "config.istio.io/v1alpha2"
kind: metric
metadata:
  name: metric1
  namespace: {{.TestNamespace}}
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
  namespace: {{.TestNamespace}}
spec:
  actions:
  - handler: handler1.bypass
    instances:
    - metric1.metric
`
)

func TestHTTP(t *testing.T) {
	ctx := framework.GetContext(t)
	ctx.RequireOrSkip(t, lifecycle.Test, &ids.Apps)
	testHTTP(t, ctx)
}

func TestHTTPKubernetes(t *testing.T) {
	ctx := framework.GetContext(t)
	ctx.RequireOrSkip(t, lifecycle.Test, &descriptors.KubernetesEnvironment, &ids.Apps)

	testHTTP(t, ctx)
}

func TestPermissive(t *testing.T) {
	ctx := framework.GetContext(t)
	// TODO(incfly): rewrite tests to run it on both k8s and native env.
	// switch go galley.Apply once it's turned on by default and supports k8s env.
	ctx.RequireOrSkip(t, lifecycle.Test, &descriptors.KubernetesEnvironment, &ids.Apps)
	env := kube.GetEnvironmentOrFail(ctx, t)
	_, err := env.ApplyContents(env.TestNamespace(), `
	apiVersion: "authentication.istio.io/v1alpha1"
	kind: "MeshPolicy"
	metadata:
		name: "default"
	spec:
		peers:
		- mtls:
				mode: PERMISSIVE
`)
	if err != nil {
		t.Fatalf("failed to apply content %v", err)
	}
}

func TestHTTPNative(t *testing.T) {
	ctx := framework.GetContext(t)

	// TODO(nmittler): When k8s deployment is supported for apps, enable policy checking for both local and k8s.
	ctx.RequireOrSkip(t, lifecycle.Test, &descriptors.NativeEnvironment, &ids.Apps, &ids.PolicyBackend, &ids.Mixer)

	apps := components.GetApps(ctx, t)
	a := apps.GetAppOrFail("a", t)
	b := apps.GetAppOrFail("b", t)
	mixer := components.GetMixer(ctx, t)
	policy := components.GetPolicyBackend(ctx, t)

	mixer.Configure(t,
		lifecycle.Test,
		test.JoinConfigs(
			ctx.Evaluate(t, reportTemplate),
			policy.CreateConfigSnippet("handler1"),
		))

	be := b.EndpointsForProtocol(model.ProtocolHTTP)[0]
	result := a.CallOrFail(be, components.AppCallOptions{}, t)[0]

	if !result.IsOK() {
		t.Fatalf("HTTP Request unsuccessful: %s", result.Body)
	}

	expected := ctx.Evaluate(t, fmt.Sprintf(`
{
  "name":"metric1.metric.{{.TestNamespace}}",
  "value":{"int64Value":"2"},
  "dimensions":{
    "requestId":{"stringValue":"%s"},
    "host":{"stringValue":"b"},
    "protocol":{"stringValue":"http"},
    "responseCode":{"int64Value":"200"}
   }
}`, result.ID))

	policy.ExpectReportJSON(t, expected)
}

func testHTTP(t *testing.T, ctx context.Instance) {
	t.Helper()

	apps := components.GetApps(ctx, t)
	a := apps.GetAppOrFail("a", t)
	b := apps.GetAppOrFail("b", t)

	be := b.EndpointsForProtocol(model.ProtocolHTTP)[0]
	result := a.CallOrFail(be, components.AppCallOptions{}, t)[0]

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
