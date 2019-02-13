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

package mixer

import (
	"testing"

	"istio.io/istio/pkg/test/framework2"
	"istio.io/istio/pkg/test/framework2/components/galley"
	"istio.io/istio/pkg/test/framework2/components/mixer"
	"istio.io/istio/pkg/test/framework2/runtime"
)

func TestCheck_Allow(t *testing.T) {
	framework2.Run(t, func(s *runtime.TestContext) {
		gal := galley.NewOrFail(s)
		mxr := mixer.NewOrFail(s, gal)
		be := policyBackend.NewOrFail(s, mxr)

	})
	//
	//ctx := framework.GetContext(t)
	//ctx.RequireOrSkip(t, lifecycle.Test, &ids.PolicyBackend, &ids.Mixer)
	//
	//mxr := components.GetMixer(ctx, t)
	//be := components.GetPolicyBackend(ctx, t)
	//
	//mxr.Configure(t,
	//	lifecycle.Test,
	//	test.JoinConfigs(
	//		testCheckConfig,
	//		be.CreateConfigSnippet("handler1"),
	//	))
	//
	//// Prime the policy backend's behavior. It should deny all check requests.
	//// This is not strictly necessary, but it is done so for posterity.
	//be.DenyCheck(t, false)
	//
	//result := mxr.Check(t, map[string]interface{}{
	//	"context.protocol":      "http",
	//	"destination.name":      "somesrvcname",
	//	"destination.namespace": "{{.TestNamespace}}",
	//	"response.time":         time.Now(),
	//	"request.time":          time.Now(),
	//	"destination.service":   `svc.{{.TestNamespace}}`,
	//	"origin.ip":             []byte{1, 2, 3, 4},
	//})
	//
	//if !result.Succeeded() {
	//	t.Fatalf("Check failed: %v", result.Raw)
	//}
}

func TestCheck_Deny(t *testing.T) {
	//ctx := framework.GetContext(t)
	//ctx.RequireOrSkip(t, lifecycle.Test, &ids.PolicyBackend, &ids.Mixer)
	//
	//mxr := components.GetMixer(ctx, t)
	//be := components.GetPolicyBackend(ctx, t)
	//
	//mxr.Configure(t,
	//	lifecycle.Test,
	//	test.JoinConfigs(
	//		testCheckConfig,
	//		be.CreateConfigSnippet("handler1"),
	//	))
	//
	//// Prime the policy backend's behavior. It should deny all check requests.
	//be.DenyCheck(t, true)
	//
	//result := mxr.Check(t, map[string]interface{}{
	//	"context.protocol":      "http",
	//	"destination.name":      "somesrvcname",
	//	"destination.namespace": "{{.TestNamespace}}",
	//	"response.time":         time.Now(),
	//	"request.time":          time.Now(),
	//	"destination.service":   `svc.{{.TestNamespace}}`,
	//	"origin.ip":             []byte{1, 2, 3, 4},
	//})
	//
	//if result.Succeeded() {
	//	t.Fatalf("Check succeeded: %v", result.Raw)
	//}
}

var testCheckConfig = `
apiVersion: "config.istio.io/v1alpha2"
kind: checknothing
metadata:
  name: checknothing1
  namespace: {{.TestNamespace}}
spec:
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
    - checknothing1.checknothing
`
