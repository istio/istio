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
	"reflect"
	"testing"

	xdsapi "github.com/envoyproxy/go-control-plane/envoy/api/v2"
	lis "github.com/envoyproxy/go-control-plane/envoy/api/v2/listener"
	proto "github.com/gogo/protobuf/types"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/api/components"
	"istio.io/istio/pkg/test/framework/api/context"
	"istio.io/istio/pkg/test/framework/api/descriptors"
	"istio.io/istio/pkg/test/framework/api/ids"
	"istio.io/istio/pkg/test/framework/api/lifecycle"
	appst "istio.io/istio/pkg/test/framework/runtime/components/apps"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/plugin/authn"
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

func veriyListener(listener *xdsapi.Listener, t *testing.T) bool {
	t.Helper()
	if listener == nil {
		return false
	}
	if len(listener.ListenerFilters) == 0 {
		return false
	}
	inspector := false
	for _, lf := range listener.ListenerFilters {
		if lf.Name == authn.EnvoyTLSInspectorFilterName {
			// fmt.Printf("listner is %+v\n", *listener)
			inspector = true
			break
		}
	}
	if !inspector {
		return false
	}
	// Check filter chain match.
	if len(listener.FilterChains) != 2 {
		return false
	}
	mtlsChain := listener.FilterChains[0]
	if !reflect.DeepEqual(mtlsChain.FilterChainMatch.ApplicationProtocols, []string{"istio"}) {
		return false
	}
	if mtlsChain.TlsContext == nil {
		return false
	}
	defaultChain := listener.FilterChains[1]
	if !reflect.DeepEqual(defaultChain.FilterChainMatch, &lis.FilterChainMatch{}) {
		return false
	}
	if defaultChain.TlsContext != nil {
		return false
	}
	return true
}

func TestPermissive(t *testing.T) {
	ctx := framework.GetContext(t)
	// TODO(incfly): make test able to run both on k8s and native when galley is ready.
	ctx.RequireOrSkip(t, lifecycle.Test, &descriptors.NativeEnvironment, &ids.Apps)
	apps := components.GetApps(ctx, t)
	a := apps.GetAppOrFail("a", t)
	pilot := components.GetPilot(ctx, t)
	req := appst.ConstructDiscoveryRequest(a)
	resp, err := pilot.CallDiscovery(req)
	if err != nil {
		t.Errorf("failed to call discovery %v", err)
	}
	for _, r := range resp.Resources {
		foo := &xdsapi.Listener{}
		if err := proto.UnmarshalAny(&r, foo); err != nil {
			t.Errorf("failed to unmarshal %v", err)
		}
		if veriyListener(foo, t) {
			return
		}
	}
	t.Errorf("failed to find any listeners having multiplexing filter chain")
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
