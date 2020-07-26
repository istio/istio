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

package data

import (
	"io/ioutil"
	"strings"
)

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
      destination.name:
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

// InstanceCheck1 is a standard testing instance for template tcheck.
var InstanceCheck1 = `
apiVersion: "config.istio.io/v1alpha2"
kind: tcheck
metadata:
  name: icheck1
  namespace: istio-system
spec:
`

// FqnI1 is the fully qualified name for I1.
var FqnI1 = "icheck1.tcheck.istio-system"

// InstanceCheck2 is another instance.
var InstanceCheck2 = `
apiVersion: "config.istio.io/v1alpha2"
kind: tcheck
metadata:
  name: icheck2
  namespace: istio-system
spec:
`

// InstanceCheck3 is another instance.
var InstanceCheck3 = `
apiVersion: "config.istio.io/v1alpha2"
kind: tcheck
metadata:
  name: icheck3
  namespace: istio-system
spec:
`

// InstanceCheck4NS2 is in the ns2 namespace
var InstanceCheck4NS2 = `
apiVersion: "config.istio.io/v1alpha2"
kind: tcheck
metadata:
  name: icheck4
  namespace: ns2
spec:
`

// InstanceCheck1WithSpec has a spec with expressions
var InstanceCheck1WithSpec = `
apiVersion: "config.istio.io/v1alpha2"
kind: tcheck
metadata:
  name: icheck1
  namespace: istio-system
spec:
  foo: attr.string
`

// InstanceCheckOutput1 is a standard testing instance for template tcheckoutput
var InstanceCheckOutput1 = `
apiVersion: config.istio.io/v1alpha2
kind: tcheckoutput
metadata:
  name: icheckoutput1
  namespace: istio-system
`

// InstanceHalt1 is a standard testing instance.
var InstanceHalt1 = `
apiVersion: "config.istio.io/v1alpha2"
kind: thalt
metadata:
  name: ihalt1
  namespace: istio-system
spec:
`

// InstanceReport1 is a standard testing instance for template treport.
var InstanceReport1 = `
apiVersion: "config.istio.io/v1alpha2"
kind: treport
metadata:
  name: ireport1
  namespace: istio-system
spec:
`

// InstanceReport1WithSpec has a spec with expressions
var InstanceReport1WithSpec = `
apiVersion: "config.istio.io/v1alpha2"
kind: treport
metadata:
  name: ireport1
  namespace: istio-system
spec:
  foo: attr.string
`

// InstanceReport2 is a standard testing instance for template treport.
var InstanceReport2 = `
apiVersion: "config.istio.io/v1alpha2"
kind: treport
metadata:
  name: ireport2
  namespace: istio-system
spec:
`

// InstanceQuota1 is a standard testing instance for template tquota.
var InstanceQuota1 = `
apiVersion: "config.istio.io/v1alpha2"
kind: tquota
metadata:
  name: iquota1
  namespace: istio-system
spec:
`

// InstanceQuota2 is a copy of InstanceQuota1 with a different name.
var InstanceQuota2 = `
apiVersion: "config.istio.io/v1alpha2"
kind: tquota
metadata:
  name: iquota2
  namespace: istio-system
spec:
`

// InstanceQuota1WithSpec has a spec with expressions
var InstanceQuota1WithSpec = `
apiVersion: "config.istio.io/v1alpha2"
kind: tquota
metadata:
  name: iquota1
  namespace: istio-system
spec:
  foo: attr.string
`

// InstanceAPA1 is an APA instance
var InstanceAPA1 = `
apiVersion: "config.istio.io/v1alpha2"
kind: tapa
metadata:
  name: iapa1
  namespace: istio-system
spec:
`

// InstanceAPA1WithSpec has a spec with expressions
var InstanceAPA1WithSpec = `
apiVersion: "config.istio.io/v1alpha2"
kind: tapa
metadata:
  name: iapa1
  namespace: istio-system
spec:
  foo: attr.string
  attribute_bindings:
    source.name: $out.generated.string | ""
`

// HandlerACheck1 is a handler of type acheck with name hcheck1.
var HandlerACheck1 = `
apiVersion: "config.istio.io/v1alpha2"
kind: acheck
metadata:
  name: hcheck1
  namespace: istio-system
spec:
`

// FqnACheck1 is the fully qualified name of HandlerH1.
var FqnACheck1 = "hcheck1.acheck.istio-system"

// HandlerACheck2 is a handler of type acheck with name hcheck2.
var HandlerACheck2 = `
apiVersion: "config.istio.io/v1alpha2"
kind: acheck
metadata:
  name: hcheck2
  namespace: istio-system
spec:
`

// HandlerACheck3NS2 is a handler in namespace NS2.
var HandlerACheck3NS2 = `
apiVersion: "config.istio.io/v1alpha2"
kind: acheck
metadata:
  name: hcheck3
  namespace: ns2
spec:
`

// HandlerACheckOutput1 is a standard handler for type acheckoutput
var HandlerACheckOutput1 = `
apiVersion: config.istio.io/v1alpha2
kind: acheckoutput
metadata:
  name: hcheckoutput1
  namespace: istio-system
`

// HandlerAReport1 is a handler of type acheck with name hreport1.
var HandlerAReport1 = `
apiVersion: "config.istio.io/v1alpha2"
kind: areport
metadata:
  name: hreport1
  namespace: istio-system
spec:
`

// HandlerAQuota1 is a handler of type aquota with name hquota1.
var HandlerAQuota1 = `
apiVersion: "config.istio.io/v1alpha2"
kind: aquota
metadata:
  name: hquota1
  namespace: istio-system
spec:
`

// HandlerAPA1 is an APA handler.
var HandlerAPA1 = `
apiVersion: "config.istio.io/v1alpha2"
kind: apa
metadata:
  name: hapa1
  namespace: istio-system
spec:
`

// RuleCheck1 is a standard testing instance config with name R1. It references I1 and H1.
var RuleCheck1 = `
apiVersion: "config.istio.io/v1alpha2"
kind: rule
metadata:
  name: rcheck1
  namespace: istio-system
spec:
  actions:
  - handler: hcheck1.acheck
    instances:
    - icheck1.tcheck.istio-system
`

// RuleCheck1TrueCondition is a standard testing instance config with name R1. It references I1 and H1.
var RuleCheck1TrueCondition = `
apiVersion: "config.istio.io/v1alpha2"
kind: rule
metadata:
  name: rcheck1
  namespace: istio-system
spec:
  match: 'true'
  actions:
  - handler: hcheck1.acheck
    instances:
    - icheck1.tcheck.istio-system
`

// RuleCheck1WithBadCondition has a parseable but not compilable condition
var RuleCheck1WithBadCondition = `
apiVersion: "config.istio.io/v1alpha2"
kind: rule
metadata:
  name: rcheck1
  namespace: istio-system
spec:
  match: needmorecheese
  actions:
  - handler: hcheck1.acheck
    instances:
    - icheck1.tcheck.istio-system
`

// RuleCheck1WithInstance1And2 has instances icheck1 & icheck2.
var RuleCheck1WithInstance1And2 = `
apiVersion: "config.istio.io/v1alpha2"
kind: rule
metadata:
  name: rcheck1
  namespace: istio-system
spec:
  actions:
  - handler: hcheck1.acheck
    instances:
    - icheck1.tcheck.istio-system
    - icheck2.tcheck.istio-system
`

// RuleCheck1WithInstance1And2Operation has instances icheck1 & icheck2 and a header operation
var RuleCheck1WithInstance1And2Operation = `
apiVersion: "config.istio.io/v1alpha2"
kind: rule
metadata:
  name: rcheck1
  namespace: istio-system
spec:
  actions:
  - handler: hcheck1.acheck
    instances:
    - icheck1.tcheck.istio-system
    - icheck2.tcheck.istio-system
  requestHeaderOperations:
  - name: a-header
    values:
    - '"test"'
  responseHeaderOperations:
  - name: b-header
    values:
    - '"test"'
`

// RuleCheck1WithMatchClause is Rule Check1 with a conditional.
var RuleCheck1WithMatchClause = `
apiVersion: "config.istio.io/v1alpha2"
kind: rule
metadata:
  name: rcheck1
  namespace: istio-system
spec:
  match: match(destination.name, "foo*")
  actions:
  - handler: hcheck1.acheck
    instances:
    - icheck1.tcheck.istio-system
`

// RuleCheck1WithInstance1And2WithMatchClause is Rule Check1 with a conditional.
var RuleCheck1WithInstance1And2WithMatchClause = `
apiVersion: "config.istio.io/v1alpha2"
kind: rule
metadata:
  name: rcheck1
  namespace: istio-system
spec:
  match: match(destination.name, "foo*")
  actions:
  - handler: hcheck1.acheck
    instances:
    - icheck1.tcheck.istio-system
    - icheck2.tcheck.istio-system
`

// RuleCheck1WithBadHandler is a standard testing rule config with a bad handler name.
var RuleCheck1WithBadHandler = `
apiVersion: "config.istio.io/v1alpha2"
kind: rule
metadata:
  name: rcheck1
  namespace: istio-system
spec:
  actions:
  - handler: hcheck1.inspector-gadget
    instances:
    - icheck1.tcheck.istio-system
`

// RuleCheck1WithNonBooleanCondition has a non-boolean condition.
var RuleCheck1WithNonBooleanCondition = `
apiVersion: "config.istio.io/v1alpha2"
kind: rule
metadata:
  name: rcheck1
  namespace: istio-system
spec:
  match: destination.name
  actions:
  - handler: hcheck1.acheck
    instances:
    - icheck1.tcheck.istio-system
`

// RuleCheck2WithInstance1AndHandler is Rule Check1 with a different name.
var RuleCheck2WithInstance1AndHandler = `
apiVersion: "config.istio.io/v1alpha2"
kind: rule
metadata:
  name: rcheck2
  namespace: istio-system
spec:
  actions:
  - handler: hcheck1.acheck
    instances:
    - icheck1.tcheck.istio-system
`

// RuleCheck2WithInstance2And3 references instances icheck2 and icheck2.
var RuleCheck2WithInstance2And3 = `
apiVersion: "config.istio.io/v1alpha2"
kind: rule
metadata:
  name: rcheck2
  namespace: istio-system
spec:
  actions:
  - handler: hcheck1.acheck
    instances:
    - icheck2.tcheck.istio-system
    - icheck3.tcheck.istio-system
`

// RuleCheck2WithInstance2And3WithMatchClause is RuleCheck2WithInstance2And3 with a conditional.
var RuleCheck2WithInstance2And3WithMatchClause = `
apiVersion: "config.istio.io/v1alpha2"
kind: rule
metadata:
  name: rcheck2
  namespace: istio-system
spec:
  match: destination.name.startsWith("foo")
  actions:
  - handler: hcheck1.acheck
    instances:
    - icheck2.tcheck.istio-system
    - icheck3.tcheck.istio-system
`

// RuleCheck2WithHandler2AndInstance2 references hcheck2 and icheck2.
var RuleCheck2WithHandler2AndInstance2 = `
apiVersion: "config.istio.io/v1alpha2"
kind: rule
metadata:
  name: rcheck2
  namespace: istio-system
spec:
  actions:
  - handler: hcheck2.acheck
    instances:
    - icheck2.tcheck.istio-system
`

// RuleCheck3NS2 is check rule in namespace NS2
var RuleCheck3NS2 = `
apiVersion: "config.istio.io/v1alpha2"
kind: rule
metadata:
  name: rcheck1
  namespace: ns2
spec:
  actions:
  - handler: hcheck3.acheck
    instances:
    - icheck4.tcheck.ns2
`

// Rule4CheckAndHalt has two instances that goes to the same adapter, but have different templates.
var Rule4CheckAndHalt = `
apiVersion: "config.istio.io/v1alpha2"
kind: rule
metadata:
  name: rcheckandhalt4
  namespace: istio-system
spec:
  actions:
  - handler: hcheck1.acheck
    instances:
    - icheck1.tcheck.istio-system
    - ihalt1.thalt.istio-system
`

// RuleCheckOutput1 is a standard testing rule for check output template
var RuleCheckOutput1 = `
apiVersion: config.istio.io/v1alpha2
kind: rule
metadata:
  name: rcheckoutput1
  namespace: istio-system
spec:
  actions:
  - handler: hcheckoutput1.acheckoutput
    instances:
    - icheckoutput1.tcheckoutput.istio-system
    name: sample
  requestHeaderOperations:
  - name: user
    values:
    - sample.output.value
  - name: empty-header
    values:
    - '""'
`

// RuleCheckOutput2 is a testing rule for check output template with multiple outputs
var RuleCheckOutput2 = `
apiVersion: config.istio.io/v1alpha2
kind: rule
metadata:
  name: rcheckoutput2
  namespace: istio-system
spec:
  actions:
  - handler: hcheckoutput1.acheckoutput
    instances:
    - icheckoutput1.tcheckoutput.istio-system
    name: a
  - handler: hcheckoutput1.acheckoutput
    instances:
    - icheckoutput1.tcheckoutput.istio-system
    name: b
  requestHeaderOperations:
  - name: a-header
    values:
    - a.output.value
    operation: REPLACE
  - name: user
    operation: REMOVE
  responseHeaderOperations:
  - name: b-header
    values:
    - b.output.value
    - prefix.generated.string
    operation: APPEND
`

// RuleCheckNoActionsOrHeaderOps has no actions and no responseHeaderOperations. Should be elided.
var RuleCheckNoActionsOrHeaderOps = `
apiVersion: config.istio.io/v1alpha2
kind: rule
metadata:
  name: noactions
  namespace: istio-system
spec:
  actions: []
  responseHeaderOperations: []
`

// RuleCheckHeaderOpWithNoActions has a responseHeaderOperation, but no actions. Should not be elided.
var RuleCheckHeaderOpWithNoActions = `
apiVersion: config.istio.io/v1alpha2
kind: rule
metadata:
  name: noactions
  namespace: istio-system
spec:
  actions: []
  responseHeaderOperations:
  - name: b-header
    values:
    - '"test"'
    operation: APPEND
`

// RuleReport1 is a standard testing instance config with name rreport1. It references I1 and H1.
var RuleReport1 = `
apiVersion: "config.istio.io/v1alpha2"
kind: rule
metadata:
  name: rreport1
  namespace: istio-system
spec:
  actions:
  - handler: hreport1.areport
    instances:
    - ireport1.treport.istio-system
`

// RuleReport1And2 is a standard testing instance config with name rreport1.
var RuleReport1And2 = `
apiVersion: "config.istio.io/v1alpha2"
kind: rule
metadata:
  name: rreport1
  namespace: istio-system
spec:
  actions:
  - handler: hreport1.areport
    instances:
    - ireport1.treport.istio-system
    - ireport2.treport.istio-system
`

// RuleQuota1 is a standard testing instance config with name rquota1. It references I1 and H1.
var RuleQuota1 = `
apiVersion: "config.istio.io/v1alpha2"
kind: rule
metadata:
  name: rquota1
  namespace: istio-system
spec:
  actions:
  - handler: hquota1.aquota
    instances:
    - iquota1.tquota.istio-system
`

// RuleQuota1Conditional conditionally selects iquota1
var RuleQuota1Conditional = `
apiVersion: "config.istio.io/v1alpha2"
kind: rule
metadata:
  name: rquota1
  namespace: istio-system
spec:
  match: attr.string == "select"
  actions:
  - handler: hquota1.aquota
    instances:
    - iquota1.tquota.istio-system
`

// RuleQuota2 references iquota1 and hquota1.
var RuleQuota2 = `
apiVersion: "config.istio.io/v1alpha2"
kind: rule
metadata:
  name: rquota2
  namespace: istio-system
spec:
  actions:
  - handler: hquota1.aquota
    instances:
    - iquota2.tquota.istio-system
`

// RuleApa1 is a rule that target APA.
var RuleApa1 = `
apiVersion: "config.istio.io/v1alpha2"
kind: rule
metadata:
  name: rapa1
  namespace: istio-system
spec:
  actions:
  - handler: hapa1.apa
    instances:
    - iapa1.tapa.istio-system
`

// ListHandler1 is a dynamic handler of listentry template named as hdynlist1
var ListHandler1 = `
apiVersion: config.istio.io/v1alpha2
kind: handler
metadata:
  name: hdynlist1
  namespace: istio-system
spec:
  adapter: listbackend-nosession
  connection:
    address: 127.0.0.1:8080
  params:
    provider_url: google.com
    overrides:
    - a
    - b
    caching_interval: 5s
`

// ListHandler2 is a dynamic handler of listentry template named as hdynlist2
var ListHandler2 = `
apiVersion: config.istio.io/v1alpha2
kind: handler
metadata:
  name: hdynlist2
  namespace: istio-system
spec:
  adapter: listbackend-nosession
  connection:
    address: 127.0.0.1:8080
  params:
    refresh_interval: 5s
    caching_use_count: 50
`

// ListHandler3 is a dynamic handler of listentry template named as hdynlist3
var ListHandler3 = `
apiVersion: config.istio.io/v1alpha2
kind: handler
metadata:
  name: hdynlist3
  namespace: istio-system
spec:
  adapter: listbackend-nosession
  connection:
    address: 127.0.0.1:8080
  params:
    blacklist: true
`

// ListHandler3Addr is a dynamic handler that is same as ListHandler3 but has different backend address
var ListHandler3Addr = `
apiVersion: config.istio.io/v1alpha2
kind: handler
metadata:
  name: hdynlist3
  namespace: istio-system
spec:
  adapter: listbackend-nosession
  connection:
    address: 127.0.0.1:8081
  params:
    blacklist: true
`

// FqdnListHandler3 is fqdn of ListHandler3
var FqdnListHandler3 = "hdynlist3.handler.istio-system"

// InstanceDynamic is the instance of the template listentry
var InstanceDynamic = `
apiVersion: "config.istio.io/v1alpha2"
kind: instance
metadata:
  name: idynlistentry1
  namespace: istio-system
spec:
  template: listentry
  params: {}
`

// RuleDynamic is a testing rule of dynamic instance idynlistentry1 and dynamic handler hdynlist3
var RuleDynamic = `
apiVersion: "config.istio.io/v1alpha2"
kind: rule
metadata:
  name: rdynlist1
  namespace: istio-system
spec:
  actions:
  - handler: hdynlist3.handler
    instances:
    - idynlistentry1.istio-system
`

// JoinConfigs is a utility for joining various pieces of config for consumption by store code.
func JoinConfigs(configs ...string) string {
	return strings.Join(configs, "\n---\n")
}

// ReadConfigs reads config from the given path list and returns them as a string.
func ReadConfigs(paths ...string) (string, error) {
	cfg := ""
	for _, p := range paths {
		fc, err := ioutil.ReadFile(p)
		if err != nil {
			return "", err
		}
		cfg = JoinConfigs(cfg, string(fc))
	}

	return cfg, nil
}
