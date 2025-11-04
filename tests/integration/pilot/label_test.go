//go:build integ

// Copyright Istio Authors
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

package pilot

import (
	"testing"

	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/check"
	"istio.io/istio/pkg/test/util/assert"
)

func TestLabelChanges(t *testing.T) {
	framework.
		NewTest(t).
		Run(func(t framework.TestContext) {
			cfg := `apiVersion: networking.istio.io/v1
kind: VirtualService
metadata:
  name: {{.Destination}}
spec:
  hosts:
  - {{.Destination}}
  http:
  - route:
    - destination:
        host: {{.Destination}}
        subset: my-subset
---
apiVersion: networking.istio.io/v1
kind: DestinationRule
metadata:
  name: {{.Destination}}
spec:
  host: {{.Destination}}
  subsets:
  - name: my-subset
    labels:
      subset: my-subset`
			t.ConfigIstio().Eval(apps.Namespace.Name(), map[string]string{
				"Destination": "a",
			}, cfg).ApplyOrFail(t)
			from := apps.B[0]
			to := apps.A

			// First, expect to fail. Subset does not match
			_ = from.CallOrFail(t, echo.CallOptions{
				To: to,
				Port: echo.Port{
					Name: "http",
				},
				Count: 1,
				Check: check.BodyContains("no healthy upstream"),
			})

			lbls := map[string]string{"subset": "my-subset"}
			assert.NoError(t, to[0].UpdateWorkloadLabel(lbls, nil))
			t.Cleanup(func() {
				assert.NoError(t, to[0].UpdateWorkloadLabel(nil, []string{"subset"}))
			})

			// Now it should succeed
			_ = from.CallOrFail(t, echo.CallOptions{
				To: to,
				Port: echo.Port{
					Name: "http",
				},
				Count: 1,
				Check: check.OK(),
			})
		})
}
