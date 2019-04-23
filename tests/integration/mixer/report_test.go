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

package mixer

import (
	"fmt"
	"testing"
	"time"

	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/galley"
	"istio.io/istio/pkg/test/framework/components/mixer"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/framework/components/policybackend"
	"istio.io/istio/pkg/test/util/retry"
	"istio.io/istio/pkg/test/util/tmpl"
)

func TestMixer_Report_Direct(t *testing.T) {
	ctx := framework.NewContext(t)
	defer ctx.Done(t)

	g := galley.NewOrFail(t, ctx, galley.Config{})
	mxr := mixer.NewOrFail(t, ctx, mixer.Config{Galley: g})
	be := policybackend.NewOrFail(t, ctx)

	ns := namespace.NewOrFail(t, ctx, "mixreport", false)

	g.ApplyConfigOrFail(t,
		ns,
		testReportConfig,
		be.CreateConfigSnippet("handler1", ns.Name()))

	expected := tmpl.EvaluateOrFail(t, `
{
  "name": "metric1.instance.{{.TestNamespace}}",
  "value": {
    "int64Value": "2"
  },
  "dimensions": {
    "destination_name": {
      "stringValue": "somesrvcname"
    },
    "origin_ip": {
      "ipAddressValue": {
        "value": "AQIDBA=="
      }
    }
  }
}
`, map[string]string{"TestNamespace": ns.Name()})

	retry.UntilSuccessOrFail(t, func() error {
		mxr.Report(t, map[string]interface{}{
			"context.protocol":      "http",
			"destination.uid":       "somesrvcname",
			"destination.namespace": ns.Name(),
			"response.time":         time.Now(),
			"request.time":          time.Now(),
			"destination.service":   "svc." + ns.Name(),
			"origin.ip":             []byte{1, 2, 3, 4},
		})

		reports := be.GetReports(t)

		if !policybackend.ContainsReportJSON(t, reports, expected) {
			return fmt.Errorf("expected report not found in current reports: %v", reports)
		}

		return nil
	})
}

var testReportConfig = `
apiVersion: "config.istio.io/v1alpha2"
kind: instance
metadata:
  name: metric1
spec:
  compiledTemplate: metric
  params:
    value: "2"
    dimensions:
      destination_name: destination.uid | "unknown"
      origin_ip: origin.ip | ip("4.5.6.7")
---
apiVersion: "config.istio.io/v1alpha2"
kind: rule
metadata:
  name: rule1
spec:
  actions:
  - handler: handler1
    instances:
    - metric1
`
