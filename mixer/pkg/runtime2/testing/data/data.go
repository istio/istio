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

// InstanceI1 is a standard testing instance config with name I1.
var InstanceI1 = `
apiVersion: "config.istio.io/v1alpha2"
kind: t1
metadata:
  name: i1
  namespace: istio-system
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
