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
	"testing"

	"istio.io/istio/mixer/pkg/perf"
	spyadapter "istio.io/istio/mixer/test/spyAdapter"
)

// Tests single check call into Mixer that dispatches instances to multiple noop inproc adapters.
func Benchmark_Check_1Client_1Call(b *testing.B) {
	settings, spyAdapter := settingsWithAdapterAndTmpls()
	settings.RunMode = perf.InProcess

	setup := perf.Setup{
		Config: perf.Config{
			Global:  mixerGlobalCfg,
			Service: checkInstToSpyAdapter + attrGenToSpyAdapter,
		},

		Loads: []perf.Load{{
			Multiplier: 1,
			Requests: []perf.Request{
				perf.BuildBasicCheck(baseAttr, nil),
			},
		}},
	}

	perf.Run(b, &setup, settings)
	validateCheckBehavior(spyAdapter, b)
}

// Tests 5 synchronous identical check call into Mixer that dispatches instances to multiple noop inproc adapters.
func Benchmark_Check_1Client_5SameCalls(b *testing.B) {
	settings, spyAdapter := settingsWithAdapterAndTmpls()
	settings.RunMode = perf.InProcess

	setup := perf.Setup{
		Config: perf.Config{
			Global:  mixerGlobalCfg,
			Service: checkInstToSpyAdapter + attrGenToSpyAdapter,
		},

		Loads: []perf.Load{{
			Multiplier: 5,
			Requests: []perf.Request{
				perf.BuildBasicCheck(baseAttr, nil),
			},
		}},
	}

	perf.Run(b, &setup, settings)
	validateCheckBehavior(spyAdapter, b)
}

// Tests 5 synchronous different check call into Mixer that dispatches instances to multiple noop inproc adapters.
func Benchmark_Check_1Client_5DifferentCalls(b *testing.B) {
	settings, spyAdapter := settingsWithAdapterAndTmpls()
	settings.RunMode = perf.InProcess

	setup := perf.Setup{
		Config: perf.Config{
			Global:  mixerGlobalCfg,
			Service: checkInstToSpyAdapter + attrGenToSpyAdapter,
		},

		Loads: []perf.Load{
			{
				Multiplier: 1,
				Requests: []perf.Request{
					perf.BuildBasicCheck(attr1, nil),
					perf.BuildBasicCheck(attr2, nil),
					perf.BuildBasicCheck(attr3, nil),
					perf.BuildBasicCheck(attr4, nil),
					perf.BuildBasicCheck(attr5, nil),
				},
			},
		},
	}

	perf.Run(b, &setup, settings)
	validateCheckBehavior(spyAdapter, b)
}

// Tests 4 async client, each sending 5 identical check call into Mixer that dispatches instances to
// multiple noop inproc adapters.
func Benchmark_Check_4Clients_5SameCallsEach(b *testing.B) {
	settings, spyAdapter := settingsWithAdapterAndTmpls()
	settings.RunMode = perf.InProcess
	setup := perf.Setup{
		Config: perf.Config{
			Global:  mixerGlobalCfg,
			Service: checkInstToSpyAdapter + attrGenToSpyAdapter,
		},

		Loads: []perf.Load{
			{
				Multiplier: 5,
				Requests: []perf.Request{
					perf.BuildBasicCheck(baseAttr, nil),
				},
			},
			{
				Multiplier: 5,
				Requests: []perf.Request{
					perf.BuildBasicCheck(baseAttr, nil),
				},
			},
			{
				Multiplier: 5,
				Requests: []perf.Request{
					perf.BuildBasicCheck(baseAttr, nil),
				},
			},
			{
				Multiplier: 5,
				Requests: []perf.Request{
					perf.BuildBasicCheck(baseAttr, nil),
				},
			},
		},
	}

	perf.Run(b, &setup, settings)
	validateCheckBehavior(spyAdapter, b)
}

// Tests 4 async client, each sending 5 different check call into Mixer that dispatches instances to
// multiple noop inproc adapters.
func Benchmark_Check_4Clients_5DifferentCallsEach(b *testing.B) {
	settings, spyAdapter := settingsWithAdapterAndTmpls()
	settings.RunMode = perf.InProcess
	setup := perf.Setup{
		Config: perf.Config{
			Global:  mixerGlobalCfg,
			Service: checkInstToSpyAdapter + attrGenToSpyAdapter,
		},

		Loads: []perf.Load{
			{
				Multiplier: 1,
				Requests: []perf.Request{
					perf.BuildBasicCheck(attr1, nil),
					perf.BuildBasicCheck(attr2, nil),
					perf.BuildBasicCheck(attr3, nil),
					perf.BuildBasicCheck(attr4, nil),
					perf.BuildBasicCheck(attr5, nil),
				},
			},
			{
				Multiplier: 1,
				Requests: []perf.Request{
					perf.BuildBasicCheck(attr1, nil),
					perf.BuildBasicCheck(attr2, nil),
					perf.BuildBasicCheck(attr3, nil),
					perf.BuildBasicCheck(attr4, nil),
					perf.BuildBasicCheck(attr5, nil),
				},
			},
			{
				Multiplier: 1,
				Requests: []perf.Request{
					perf.BuildBasicCheck(attr1, nil),
					perf.BuildBasicCheck(attr2, nil),
					perf.BuildBasicCheck(attr3, nil),
					perf.BuildBasicCheck(attr4, nil),
					perf.BuildBasicCheck(attr5, nil),
				},
			},
			{
				Multiplier: 1,
				Requests: []perf.Request{
					perf.BuildBasicCheck(attr1, nil),
					perf.BuildBasicCheck(attr2, nil),
					perf.BuildBasicCheck(attr3, nil),
					perf.BuildBasicCheck(attr4, nil),
					perf.BuildBasicCheck(attr5, nil),
				},
			},
		},
	}

	perf.Run(b, &setup, settings)
	validateCheckBehavior(spyAdapter, b)
}

// Tests 4 async client, each sending 5 identical check call into Mixer that dispatches instances to
// multiple noop inproc adapters. The APA in this case is a slow by 1ms.
func Benchmark_Check_4Clients_5SameCallsEach_1MilliSecSlowApa(b *testing.B) {
	settings, spyAdapter := settingsWith1milliSecApaAdapterAndTmpls()
	settings.RunMode = perf.InProcess
	setup := perf.Setup{
		Config: perf.Config{
			Global:  mixerGlobalCfg,
			Service: checkInstToSpyAdapter + attrGenToSpyAdapter,
		},

		Loads: []perf.Load{
			{
				Multiplier: 5,
				Requests: []perf.Request{
					perf.BuildBasicCheck(baseAttr, nil),
				},
			},
			{
				Multiplier: 5,
				Requests: []perf.Request{
					perf.BuildBasicCheck(baseAttr, nil),
				},
			},
			{
				Multiplier: 5,
				Requests: []perf.Request{
					perf.BuildBasicCheck(baseAttr, nil),
				},
			},
			{
				Multiplier: 5,
				Requests: []perf.Request{
					perf.BuildBasicCheck(baseAttr, nil),
				},
			},
		},
	}

	perf.Run(b, &setup, settings)
	validateCheckBehavior(spyAdapter, b)
}

func validateCheckBehavior(spyAdapter *spyadapter.Adapter, b *testing.B) {
	// validate all went as expected.
	//
	// based on the config, there must be, for each Check call from client,
	// * single attribute generation call
	// * single list check call
	foundAttrGenCall := false
	foundCheckCall := false
	for _, cc := range spyAdapter.HandlerData.CapturedCalls {
		if cc.Name == "HandleSampleApaAttributes" && len(cc.Instances) == 1 {
			foundAttrGenCall = true
		}
		if cc.Name == "HandleSampleCheck" && len(cc.Instances) == 1 {
			foundCheckCall = true
		}
	}

	if !foundAttrGenCall || !foundCheckCall {
		b.Errorf("got spy adapter calls %v; want calls  with HandleSampleApaAttributes:1 & HandleSampleCheck:1",
			spyAdapter.HandlerData.CapturedCalls)
	}
}

const (
	// contains 1 rules that pass 1 instance to a check adapter
	checkInstToSpyAdapter = `
apiVersion: "config.istio.io/v1alpha2"
kind: spyadapter
metadata:
  name: spyadapterHandler
  namespace: istio-system
spec:

---
apiVersion: "config.istio.io/v1alpha2"
kind: samplecheck
metadata:
  name: samplecheckInst
  namespace: istio-system
spec:
  stringPrimitive: source.labels["version"] | "unknown"
---
apiVersion: "config.istio.io/v1alpha2"
kind: rule
metadata:
  name: listEntryRule
  namespace: istio-system
spec:
  match: context.protocol == "http"
  actions:
  - handler: spyadapterHandler.spyadapter
    instances:
    - samplecheckInst.samplecheck
---
`
)
