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

var GlobalConfig = `
apiVersion: "config.istio.io/v1alpha2"
kind: a1
metadata:
  name: h1
  namespace: istio-system
---
apiVersion: "config.istio.io/v1alpha2"
kind: t1
metadata:
  name: i1
  namespace: istio-system
spec:
---
apiVersion: "config.istio.io/v1alpha2"
kind: rule
metadata:
  name: r1
  namespace: istio-system
spec:
  selector: match(target.name, "*")
  actions:
  - handler: h1.a1
    instances:
    - i1.t1.istio-system
---
`

var GlobalConfigI2 = `
apiVersion: "config.istio.io/v1alpha2"
kind: a1
metadata:
  name: h1
  namespace: istio-system
---
apiVersion: "config.istio.io/v1alpha2"
kind: t1
metadata:
  name: i1
  namespace: istio-system
spec:
---
apiVersion: "config.istio.io/v1alpha2"
kind: t1
metadata:
  name: i2
  namespace: istio-system
spec:
---
apiVersion: "config.istio.io/v1alpha2"
kind: rule
metadata:
  name: r1
  namespace: istio-system
spec:
  selector: match(target.name, "*")
  actions:
  - handler: h1.a1
    instances:
    - i1.t1.istio-system
    - i2.t1.istio-system
---
`
