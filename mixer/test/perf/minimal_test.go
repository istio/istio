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

// Package test supplies a fake Mixer server for use in testing. It should NOT
// be used outside of testing contexts.
package perftests

import (
	"testing"

	"istio.io/istio/mixer/adapter"
	"istio.io/istio/mixer/pkg/perf"
	generatedTmplRepo "istio.io/istio/mixer/template"
)

const (
	globalCfg = `
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
	reportTestCfg = `
apiVersion: "config.istio.io/v1alpha2"
kind: fakeHandler
metadata:
  name: fakeHandlerConfig
  namespace: istio-system

---

apiVersion: "config.istio.io/v1alpha2"
kind: samplereport
metadata:
  name: reportInstance
  namespace: istio-system
spec:
  value: "2"
  dimensions:
    source: source.name | "mysrc"
    target_ip: target.name | "mytarget"

---

apiVersion: "config.istio.io/v1alpha2"
kind: rule
metadata:
  name: rule1
  namespace: istio-system
spec:
  selector: match(target.name, "*")
  actions:
  - handler: fakeHandlerConfig.fakeHandler
    instances:
    - reportInstance.samplereport

---
`
)

var setup = perf.Setup{
	Config: perf.Config{
		Global:                  globalCfg,
		Service:                 reportTestCfg,
		IdentityAttribute:       "destination.service",
		IdentityAttributeDomain: "svc.cluster.local",
	},

	Load: perf.Load{
		Multiplier: 1000,
		Requests: []perf.Request{
			perf.BasicReport{
				Attributes: map[string]interface{}{"target.name": "somesrvcname"},
			},
			perf.BasicReport{
				Attributes: map[string]interface{}{"target.name": "cvd"},
			},
		},
	},
}

const executableSearchSuffix = "bazel-bin/mixer/test/perf/perfclient/perfclient"

var env = perf.NewEnv(
	generatedTmplRepo.SupportedTmplInfo,
	adapter.Inventory(),
)

// Benchmark_Minimal_ExternalProcess is a proof of concept test, and is not meant as a real perf test case.
func Benchmark_Minimal_ExternalProcess(b *testing.B) {
	perf.RunCoprocess(b, &setup, env, executableSearchSuffix)
}

// TODO: Currently, the mixer service does multiple registrations for its /metrics path. Once we can reliable create/close
// mixer instances, we can run more than one benchmark side-by-side. Until then, commenting out the test below.

//func Benchmark_Minimal_InternallProcess(b *testing.B) {
//	perf.RunInprocess(b, &setup)
//}
