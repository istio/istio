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
	"fmt"
	"testing"
	"time"

	"istio.io/istio/pkg/test/util/tmpl"

	"istio.io/istio/pkg/test/util/retry"

	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/framework2"
	"istio.io/istio/pkg/test/framework2/components/galley"
	"istio.io/istio/pkg/test/framework2/components/mixer"
	"istio.io/istio/pkg/test/framework2/components/policybackend"
	"istio.io/istio/pkg/test/framework2/runtime"
)

func TestCheck_Allow(t *testing.T) {
	framework2.Run(t, func(s *runtime.TestContext) {
		gal := galley.NewOrFail(t, s, galley.Config{})
		mxr := mixer.NewOrFail(t, s, mixer.Config{
			Galley: gal,
		})
		be := policybackend.NewOrFail(t, s)

		ns := s.Environment().AllocateNamespaceOrFail(t, "testcheck_allow", false)

		gal.ApplyConfigOrFail(
			t,
			test.JoinConfigs(
				tmpl.EvaluateOrFail(t, testCheckConfig, map[string]string{"TestNamespace": ns.Name()}),
				be.CreateConfigSnippet("handler1", ns.Name()),
			))

		// Prime the policy backend's behavior. It should deny all check requests.
		// This is not strictly necessary, but it is done so for posterity.
		be.DenyCheck(t, false)

		retry.TillSuccessOrFail(t, func() error {
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
		})
	})
}

func TestCheck_Deny(t *testing.T) {
	framework2.Run(t, func(s *runtime.TestContext) {
		gal := galley.NewOrFail(t, s, galley.Config{})
		mxr := mixer.NewOrFail(t, s, mixer.Config{
			Galley: gal,
		})
		be := policybackend.NewOrFail(t, s)

		ns := s.Environment().AllocateNamespaceOrFail(t, "testcheck_allow", false)

		gal.ApplyConfigOrFail(
			t,
			test.JoinConfigs(
				tmpl.EvaluateOrFail(t, testCheckConfig, map[string]string{"TestNamespace": ns.Name()}),
				be.CreateConfigSnippet("handler1", ns.Name()),
			))

		// Prime the policy backend's behavior. It should deny all check requests.
		// This is not strictly necessary, but it is done so for posterity.
		be.DenyCheck(t, true)

		retry.TillSuccessOrFail(t, func() error {
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
		})
	})
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
