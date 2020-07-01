//  Copyright Istio Authors
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
	"fmt"
	"testing"
	"time"

	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/mixer"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/framework/components/policybackend"
	"istio.io/istio/pkg/test/util/retry"
)

func TestCheck_Allow(t *testing.T) {
	framework.
		NewTest(t).
		Run(func(ctx framework.TestContext) {
			mxr := mixer.NewOrFail(t, ctx, mixer.Config{})
			be := policybackend.NewOrFail(t, ctx, policybackend.Config{})

			ns := namespace.NewOrFail(t, ctx, namespace.Config{
				Prefix: "testcheck-allow",
				Inject: true,
			})

			ctx.Config().ApplyYAMLOrFail(
				t,
				ns.Name(),
				testCheckConfig,
				be.CreateConfigSnippet("handler1", ns.Name(), policybackend.InProcess))

			// Prime the policy backend'ctx behavior. It should deny all check requests.
			// This is not strictly necessary, but it is done so for posterity.
			be.AllowCheck(t, 1*time.Second, 1)

			retry.UntilSuccessOrFail(t, func() error {
				result := mxr.Check(t, map[string]interface{}{
					"context.protocol":      "http",
					"destination.name":      "somesrvcname",
					"destination.namespace": ns.Name(),
					"response.time":         time.Now(),
					"request.time":          time.Now(),
					"destination.service":   `svc.` + ns.Name(),
					"origin.ip":             []byte{1, 2, 3, 4},
				})

				// TODO: ensure that the policy backend receives the request.
				if !result.Succeeded() {
					return fmt.Errorf("check failed: %v", result.Raw)
				}

				return nil
			}, retry.Delay(15*time.Second), retry.Timeout(60*time.Second))
		})
}

func TestCheck_Deny(t *testing.T) {
	framework.
		NewTest(t).
		Run(func(ctx framework.TestContext) {
			mxr := mixer.NewOrFail(t, ctx, mixer.Config{})
			be := policybackend.NewOrFail(t, ctx, policybackend.Config{})

			ns := namespace.NewOrFail(t, ctx, namespace.Config{
				Prefix: "testcheck-deny",
			})

			ctx.Config().ApplyYAMLOrFail(
				t,
				ns.Name(),
				testCheckConfig,
				be.CreateConfigSnippet("handler1", ns.Name(), policybackend.InProcess))

			// Prime the policy backend'ctx behavior. It should deny all check requests.
			// This is not strictly necessary, but it is done so for posterity.
			be.DenyCheck(t, true)

			retry.UntilSuccessOrFail(t, func() error {
				result := mxr.Check(t, map[string]interface{}{
					"context.protocol":      "http",
					"destination.name":      "somesrvcname",
					"destination.namespace": ns.Name(),
					"response.time":         time.Now(),
					"request.time":          time.Now(),
					"destination.service":   `svc.` + ns.Name(),
					"origin.ip":             []byte{1, 2, 3, 4},
				})
				if result.Succeeded() {
					return fmt.Errorf("check failed: %v", result.Raw)
				}

				// TODO: ensure that the policy backend receives the request.

				return nil
			}, retry.Delay(15*time.Second), retry.Timeout(60*time.Second))
		})
}

var testCheckConfig = `
apiVersion: "config.istio.io/v1alpha2"
kind: instance
metadata:
  name: checknothing1
spec:
  compiledTemplate: checknothing
---
apiVersion: "config.istio.io/v1alpha2"
kind: rule
metadata:
  name: rule1
spec:
  actions:
  - handler: handler1
    instances:
    - checknothing1
`
