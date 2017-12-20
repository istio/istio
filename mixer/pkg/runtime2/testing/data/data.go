// Copyright 2017 Istio Authors
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

package data

import "strings"

// ServiceConfig is a standard service config.
var ServiceConfig = `
apiVersion: "config.istio.io/v1alpha2"
kind: attributemanifest
metadata:
  name: istio-proxy
  namespace: default
spec:
    attributes:
      source.name:
        value_type: STRING
      target.name:
        value_type: STRING
      response.count:
        value_type: INT64
      attr.bool:
        value_type: BOOL
      attr.string:
        value_type: STRING
      attr.double:
        value_type: DOUBLE
      attr.int64:
        value_type: INT64
---
`

// HandlerH1 is a standard testing handler config with name H1.
var HandlerH1 = `
apiVersion: "config.istio.io/v1alpha2"
kind: a1
metadata:
  name: h1
  namespace: istio-system
`

// HandlerH1NS2 is a standard testing handler config with name H1, in namespace 'ns2'.
var HandlerH1NS2 = `
apiVersion: "config.istio.io/v1alpha2"
kind: a1
metadata:
  name: h1
  namespace: ns2
`

// HandlerH2BadBuilder is a standard testing handler config with name h2-bad-builder. It's builder will always return error.
var HandlerH2BadBuilder = `
apiVersion: "config.istio.io/v1alpha2"
kind: a2-bad-builder
metadata:
  name: h2-bad-builder
  namespace: istio-system
`

// HandlerH3HandlerDoesNotSupportTemplate is a standard testing handler config with name h3-handler-does-not-supports-template. It's handler
// does not support the template.
var HandlerH3HandlerDoesNotSupportTemplate = `
apiVersion: "config.istio.io/v1alpha2"
kind: a3-handler-does-not-support-template
metadata:
  name: h3-handler-does-not-support-template
  namespace: istio-system
`

// HandlerH4AnotherHandler is a standard testing handler config with name H4. It is just a different handler that is similar to H1.
var HandlerH4AnotherHandler = `
apiVersion: "config.istio.io/v1alpha2"
kind: a1
metadata:
  name: h4
  namespace: istio-system
`

// InstanceI1 is a standard testing instance config with name I1.
var InstanceI1 = `
apiVersion: "config.istio.io/v1alpha2"
kind: t1
metadata:
  name: i1
  namespace: istio-system
spec:
`

// InstanceI1NS2 is a standard testing instance config with name I1, in namespace NS2.
var InstanceI1NS2 = `
apiVersion: "config.istio.io/v1alpha2"
kind: t1
metadata:
  name: i1
  namespace: ns2
spec:
`

// InstanceI2 is a standard testing instance config with name I2.
var InstanceI2 = `
apiVersion: "config.istio.io/v1alpha2"
kind: t1
metadata:
  name: i2
  namespace: istio-system
spec:
`

// InstanceI3 is a standard testing instance config with name I3.
var InstanceI3 = `
apiVersion: "config.istio.io/v1alpha2"
kind: t1
metadata:
  name: i2
  namespace: istio-system
spec:
`

// RuleR1I1 is a standard testing rule config with name R1 which references I1.
var RuleR1I1 = `
apiVersion: "config.istio.io/v1alpha2"
kind: rule
metadata:
  name: r1
  namespace: istio-system
spec:
  actions:
  - handler: h1.a1
    instances:
    - i1.t1.istio-system
`

// RuleR1I1NS2 is a standard testing rule config with name R1 which references I1 in namespace NS2.
var RuleR1I1NS2 = `
apiVersion: "config.istio.io/v1alpha2"
kind: rule
metadata:
  name: r1
  namespace: ns2
spec:
  actions:
  - handler: h1.a1
    instances:
    - i1.t1.ns2
`

// RuleR2I1I2 is a standard testing rule config with name R2 which references I1 and I2.
var RuleR2I1I2 = `
apiVersion: "config.istio.io/v1alpha2"
kind: rule
metadata:
  name: r2
  namespace: istio-system
spec:
  actions:
  - handler: h1.a1
    instances:
    - i1.t1.istio-system
    - i2.t1.istio-system
`

// RuleR3I1I2 is a standard testing rule config with name R3 which references I1 and I2 and has a conditional.
var RuleR3I1I2 = `
apiVersion: "config.istio.io/v1alpha2"
kind: rule
metadata:
  name: r3
  namespace: istio-system
spec:
  selector: match(target.name, "*")
  actions:
  - handler: h1.a1
    instances:
    - i1.t1.istio-system
    - i2.t1.istio-system
`

// RuleR4I1BadCondition is a standard testing rule config with a bad match expression.
var RuleR4I1BadCondition = `
apiVersion: "config.istio.io/v1alpha2"
kind: rule
metadata:
  name: r4
  namespace: istio-system
spec:
  selector: send-more-cheese
  actions:
  - handler: h1.a1
    instances:
    - i1.t1.istio-system
`

// RuleR5I1BadHandlerName is a standard testing rule config with a bad handler name.
var RuleR5I1BadHandlerName = `
apiVersion: "config.istio.io/v1alpha2"
kind: rule
metadata:
  name: r5
  namespace: istio-system
spec:
  actions:
  - handler: h1.inspector-gadget
    instances:
    - i1.t1.istio-system
`

// RuleR6I1BadHandlerBuilder is a standard testing rule config that references adapter a2-bad-builder.
var RuleR6I1BadHandlerBuilder = `
apiVersion: "config.istio.io/v1alpha2"
kind: rule
metadata:
  name: r6
  namespace: istio-system
spec:
  actions:
  - handler: h2-bad-builder.a2-bad-builder
    instances:
    - i1.t1.istio-system
`

// RuleR7I1HandlerDoesNotSupportTemplate is a standard testing rule config that references adapter h3-handler-does-not-support-template.
var RuleR7I1HandlerDoesNotSupportTemplate = `
apiVersion: "config.istio.io/v1alpha2"
kind: rule
metadata:
  name: r6
  namespace: istio-system
spec:
  actions:
  - handler: h3-handler-does-not-support-template.a3-handler-does-not-support-template
    instances:
    - i1.t1.istio-system
`

// RuleR8I1I2AnotherConditional is a standard testing rule config with name R8 which references I1 and I2 and has a conditional.
// different version of RuleR3I1I2.
var RuleR8I1I2AnotherConditional = `
apiVersion: "config.istio.io/v1alpha2"
kind: rule
metadata:
  name: r8
  namespace: istio-system
spec:
  selector: target.name.startsWith("foo")
  actions:
  - handler: h1.a1
    instances:
    - i1.t1.istio-system
    - i2.t1.istio-system
`

// RuleR9H4AnotherHandler is a standard testing rule config with name R9 which references I1 and H4.
var RuleR9H4AnotherHandler = `
apiVersion: "config.istio.io/v1alpha2"
kind: rule
metadata:
  name: r9
  namespace: istio-system
spec:
  actions:
  - handler: h4.a1
    instances:
    - i1.t1.istio-system
`

// GlobalConfig is the default GlobalConfig that consists of combination of various default config entries.
var GlobalConfig = JoinConfigs(HandlerH1, InstanceI1, RuleR1I1)

// GlobalConfigI2 is the an alternate GlobalConfig instance.
var GlobalConfigI2 = JoinConfigs(HandlerH1, InstanceI1, InstanceI2, RuleR1a)

// RuleR1a is a modification of the R1 rule.
var RuleR1a = `
apiVersion: "config.istio.io/v1alpha2"
kind: rule
metadata:
  name: r1
  namespace: istio-system
spec:
  selector: target.name == "*"
  actions:
  - handler: h1.a1
    instances:
    - i1.t1.istio-system
    - i2.t1.istio-system
`

// JoinConfigs is a utility for joining various pieces of config for consumption by store code.
func JoinConfigs(configs ...string) string {
	return strings.Join(configs, "\n---\n")
}
