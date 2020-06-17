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

package perftests

import (
	"strings"

	"istio.io/istio/mixer/adapter"
	"istio.io/istio/mixer/pkg/perf"
	generatedTmplRepo "istio.io/istio/mixer/template"
)

// ExecutableSearchSuffix is used to find the co-process executable that hosts the driver Mixer client.
const ExecutableSearchSuffix = "bazel-bin/mixer/test/perf/perfclient/perfclient"

var baseSettings = perf.Settings{
	Templates:            generatedTmplRepo.SupportedTmplInfo,
	Adapters:             adapter.Inventory(),
	ExecutablePathSuffix: ExecutableSearchSuffix,
}

// A minimal service config that can be used by other systems.
var minimalServiceConfig = `
apiVersion: "config.istio.io/v1alpha2"
kind: attributemanifest
metadata:
  name: istio-proxy
  namespace: default
spec:
    attributes:
      source.name:
        value_type: STRING
      source.service:
        value_type: STRING
      source.labels:
        value_type: STRING_MAP
      destination.service:
        value_type: STRING
      destination.labels:
        value_type: STRING_MAP
      target.name:
        value_type: STRING
      response.code:
        value_type: INT64
      request.size:
        value_type: INT64
      response_code:
        value_type: INT64

      attr.bool:
        value_type: BOOL
      attr.string:
        value_type: STRING
      attr.double:
        value_type: DOUBLE
      attr.int64:
        value_type: INT64
---`

// h1Noop is the handler configuration using the noop adapter.
var h1Noop = `
apiVersion: "config.istio.io/v1alpha2"
kind: noop
metadata:
  name: h1
  namespace: istio-system
`

// i1ReportNothing is an instance that uses the reportnothing template.
var i1ReportNothing = `
apiVersion: "config.istio.io/v1alpha2"
kind: reportnothing
metadata:
  name: i1
  namespace: istio-system
spec:
`

// i2CheckNothing is an instance that uses the checknothing template.
var i2CheckNothing = `
apiVersion: "config.istio.io/v1alpha2"
kind: checknothing
metadata:
  name: i2
  namespace: istio-system
spec:
`

// i3Metric is an instance that uses the metric template.
var i3Metric = `
apiVersion: "config.istio.io/v1alpha2"
kind: metric
metadata:
  name: i3
  namespace: istio-system
spec:
   value: request.size | 0
   dimensions:
     source_service: source.service | "unknown"
     source_version: source.labels["version"] | "unknown"
     destination_service: destination.service | "unknown"
     destination_version: destination.labels["version"] | "unknown"
     response_code: response.code | 200
   monitored_resource_type: '"UNSPECIFIED"'
`

// i4Metric is an instance that uses the metric template.
var i4Metric = `
apiVersion: "config.istio.io/v1alpha2"
kind: metric
metadata:
  name: i4
  namespace: istio-system
spec:
   value: request.size | 0
   dimensions:
     source_service: source.service | "unknown"
     source_version: source.labels["version"] | "unknown"
     destination_service: destination.service | "unknown"
     destination_version: destination.labels["version"] | "unknown"
     response_code: response.code | 200
   monitored_resource_type: '"UNSPECIFIED"'
`

// i5Metric is an instance that uses the metric template.
var i5Metric = `
apiVersion: "config.istio.io/v1alpha2"
kind: metric
metadata:
  name: i5
  namespace: istio-system
spec:
   value: request.size | 0
   dimensions:
     source_service: source.service | "unknown"
     source_version: source.labels["version"] | "unknown"
     destination_service: destination.service | "unknown"
     destination_version: destination.labels["version"] | "unknown"
     response_code: response.code | 200
   monitored_resource_type: '"UNSPECIFIED"'
`

// i6Metric is an instance that uses the metric template.
var i6Metric = `
apiVersion: "config.istio.io/v1alpha2"
kind: metric
metadata:
  name: i6
  namespace: istio-system
spec:
   value: request.size | 0
   dimensions:
     source_service: source.service | "unknown"
     source_version: source.labels["version"] | "unknown"
     destination_service: destination.service | "unknown"
     destination_version: destination.labels["version"] | "unknown"
     response_code: response.code | 200
   monitored_resource_type: '"UNSPECIFIED"'
`

// i7Metric is an instance that uses the metric template.
var i7Metric = `
apiVersion: "config.istio.io/v1alpha2"
kind: metric
metadata:
  name: i7
  namespace: istio-system
spec:
   value: request.size | 0
   dimensions:
     source_service: source.service | "unknown"
     source_version: source.labels["version"] | "unknown"
     destination_service: destination.service | "unknown"
     destination_version: destination.labels["version"] | "unknown"
     response_code: response.code | 200
   monitored_resource_type: '"UNSPECIFIED"'
`

// r1UsingH1AndI1 is a simple no-condition rule that calls into the noop adapter with reportnothing template.
var r1UsingH1AndI1 = `
apiVersion: "config.istio.io/v1alpha2"
kind: rule
metadata:
  name: r1-h1-i1
  namespace: istio-system
spec:
  actions:
  - handler: h1.noop
    instances:
    - i1.reportnothing.istio-system
`

// r2UsingH1AndI2 is a simple no-condition rule that calls into the noop adapter with checknothing template.
var r2UsingH1AndI2 = `
apiVersion: "config.istio.io/v1alpha2"
kind: rule
metadata:
  name: r2-h1-i2
  namespace: istio-system
spec:
  actions:
  - handler: h1.noop
    instances:
    - i2.checknothing.istio-system
`

// r3UsingH1AndI1Conditional is a simple conditional rule that calls into the noop adapter with reportnothing template.
var r3UsingH1AndI1Conditional = `
apiVersion: "config.istio.io/v1alpha2"
kind: rule
metadata:
  name: r1-h1-i1
  namespace: istio-system
spec:
  match: attr.int64 == 42
  actions:
  - handler: h1.noop
    instances:
    - i1.reportnothing.istio-system
`

var r5UsingH1AndI3 = `
apiVersion: "config.istio.io/v1alpha2"
kind: rule
metadata:
  name: r5-h1-i3
  namespace: istio-system
spec:
  actions:
  - handler: h1.noop
    instances:
    - i3.metric.istio-system
`

var r6UsingH1AndI3To7 = `
apiVersion: "config.istio.io/v1alpha2"
kind: rule
metadata:
  name: r6-h1-i3to7
  namespace: istio-system
spec:
  actions:
  - handler: h1.noop
    instances:
    - i3.metric.istio-system
    - i4.metric.istio-system
    - i5.metric.istio-system
    - i6.metric.istio-system
    - i7.metric.istio-system
`

func joinConfigs(configs ...string) string {
	return strings.Join(configs, "\n---\n")
}
