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

var HandlerH1 = `
apiVersion: "config.istio.io/v1alpha2"
kind: a1
metadata:
  name: h1
  namespace: istio-system
`

var InstanceI1 = `
apiVersion: "config.istio.io/v1alpha2"
kind: t1
metadata:
  name: i1
  namespace: istio-system
spec:
`

var InstanceI2 = `
apiVersion: "config.istio.io/v1alpha2"
kind: t1
metadata:
  name: i2
  namespace: istio-system
spec:
`
var InstanceI3 = `
apiVersion: "config.istio.io/v1alpha2"
kind: t1
metadata:
  name: i2
  namespace: istio-system
spec:
`

var RuleR1_I1 = `
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

var RuleR2_I1_I2 = `
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

var RuleR3_I1_I2_Conditional = `
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

var GlobalConfig = JoinConfigs(HandlerH1, InstanceI1, RuleR1_I1)

var GlobalConfigI2 = JoinConfigs(HandlerH1, InstanceI1, InstanceI2, RuleR1a)

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

func JoinConfigs(configs ...string) string {
	return strings.Join(configs, "\n---\n")
}