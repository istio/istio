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

	istio_mixer_v1 "istio.io/api/mixer/v1"
	"istio.io/istio/mixer/pkg/perf"
	spyadapter "istio.io/istio/mixer/test/spyAdapter"
)

// Tests single quota call into Mixer that dispatches instances to multiple noop inproc adapters.
func Benchmark_Quota_1Client_1Call(b *testing.B) {
	settings, spyAdapter := settingsWithAdapterAndTmpls()
	settings.RunMode = perf.InProcess

	setup := perf.Setup{
		Config: perf.Config{
			Global:  mixerGlobalCfg,
			Service: quotaInstToSpyAdapter + attrGenToSpyAdapter,
		},

		Loads: []perf.Load{{
			Multiplier: 1,
			Requests: []perf.Request{
				perf.BuildBasicCheck(
					baseAttr,
					map[string]istio_mixer_v1.CheckRequest_QuotaParams{
						"requestcount": {Amount: 1},
					}),
			},
		}},
	}

	perf.Run(b, &setup, settings)
	validateQuotaBehavior(spyAdapter, b)
}

// Tests 5 synchronous identical quota call into Mixer that dispatches instances to multiple noop inproc adapters.
func Benchmark_Quota_1Client_5SameCalls(b *testing.B) {
	settings, spyAdapter := settingsWithAdapterAndTmpls()
	settings.RunMode = perf.InProcess

	setup := perf.Setup{
		Config: perf.Config{
			Global:  mixerGlobalCfg,
			Service: quotaInstToSpyAdapter + attrGenToSpyAdapter,
		},

		Loads: []perf.Load{{
			Multiplier: 5,
			Requests: []perf.Request{
				perf.BuildBasicCheck(
					baseAttr,
					map[string]istio_mixer_v1.CheckRequest_QuotaParams{
						"requestcount": {Amount: 1},
					}),
			},
		}},
	}

	perf.Run(b, &setup, settings)
	validateQuotaBehavior(spyAdapter, b)
}

// Tests 5 synchronous different quota call into Mixer that dispatches instances to multiple noop inproc adapters.
func Benchmark_Quota_1Client_5DifferentCalls(b *testing.B) {
	settings, spyAdapter := settingsWithAdapterAndTmpls()
	settings.RunMode = perf.InProcess

	setup := perf.Setup{
		Config: perf.Config{
			Global:  mixerGlobalCfg,
			Service: quotaInstToSpyAdapter + attrGenToSpyAdapter,
		},

		Loads: []perf.Load{
			{
				Multiplier: 1,
				Requests: []perf.Request{
					perf.BuildBasicCheck(
						attr1,
						map[string]istio_mixer_v1.CheckRequest_QuotaParams{
							"requestcount": {Amount: 1},
						}),
					perf.BuildBasicCheck(
						attr2,
						map[string]istio_mixer_v1.CheckRequest_QuotaParams{
							"requestcount": {Amount: 1},
						}),
					perf.BuildBasicCheck(
						attr3,
						map[string]istio_mixer_v1.CheckRequest_QuotaParams{
							"requestcount": {Amount: 1},
						}),
					perf.BuildBasicCheck(
						attr4,
						map[string]istio_mixer_v1.CheckRequest_QuotaParams{
							"requestcount": {Amount: 1},
						}),
					perf.BuildBasicCheck(
						attr5,
						map[string]istio_mixer_v1.CheckRequest_QuotaParams{
							"requestcount": {Amount: 1},
						}),
				},
			},
		},
	}

	perf.Run(b, &setup, settings)
	validateQuotaBehavior(spyAdapter, b)
}

// Tests 4 async client, each sending 5 identical quota call into Mixer that dispatches instances to
// multiple noop inproc adapters.
func Benchmark_Quota_4Clients_5SameCallsEach(b *testing.B) {
	settings, spyAdapter := settingsWithAdapterAndTmpls()
	settings.RunMode = perf.InProcess
	setup := perf.Setup{
		Config: perf.Config{
			Global:  mixerGlobalCfg,
			Service: quotaInstToSpyAdapter + attrGenToSpyAdapter,
		},

		Loads: []perf.Load{
			{
				Multiplier: 5,
				Requests: []perf.Request{
					perf.BuildBasicCheck(
						baseAttr,
						map[string]istio_mixer_v1.CheckRequest_QuotaParams{
							"requestcount": {Amount: 1},
						}),
				},
			},
			{
				Multiplier: 5,
				Requests: []perf.Request{
					perf.BuildBasicCheck(
						baseAttr,
						map[string]istio_mixer_v1.CheckRequest_QuotaParams{
							"requestcount": {Amount: 1},
						}),
				},
			},
			{
				Multiplier: 5,
				Requests: []perf.Request{
					perf.BuildBasicCheck(
						baseAttr,
						map[string]istio_mixer_v1.CheckRequest_QuotaParams{
							"requestcount": {Amount: 1},
						}),
				},
			},
			{
				Multiplier: 5,
				Requests: []perf.Request{
					perf.BuildBasicCheck(
						baseAttr,
						map[string]istio_mixer_v1.CheckRequest_QuotaParams{
							"requestcount": {Amount: 1},
						}),
				},
			},
		},
	}

	perf.Run(b, &setup, settings)
	validateQuotaBehavior(spyAdapter, b)
}

// Tests 4 async client, each sending 5 different quota call into Mixer that dispatches instances to
// multiple noop inproc adapters.
func Benchmark_Quota_4Clients_5DifferentCallsEach(b *testing.B) {
	settings, spyAdapter := settingsWithAdapterAndTmpls()
	settings.RunMode = perf.InProcess
	setup := perf.Setup{
		Config: perf.Config{
			Global:  mixerGlobalCfg,
			Service: quotaInstToSpyAdapter + attrGenToSpyAdapter,
		},

		Loads: []perf.Load{
			{
				Multiplier: 1,
				Requests: []perf.Request{
					perf.BuildBasicCheck(
						attr1,
						map[string]istio_mixer_v1.CheckRequest_QuotaParams{
							"requestcount": {Amount: 1},
						}),
					perf.BuildBasicCheck(
						attr2,
						map[string]istio_mixer_v1.CheckRequest_QuotaParams{
							"requestcount": {Amount: 1},
						}),
					perf.BuildBasicCheck(
						attr3,
						map[string]istio_mixer_v1.CheckRequest_QuotaParams{
							"requestcount": {Amount: 1},
						}),
					perf.BuildBasicCheck(
						attr4,
						map[string]istio_mixer_v1.CheckRequest_QuotaParams{
							"requestcount": {Amount: 1},
						}),
					perf.BuildBasicCheck(
						attr5,
						map[string]istio_mixer_v1.CheckRequest_QuotaParams{
							"requestcount": {Amount: 1},
						}),
				},
			},
			{
				Multiplier: 1,
				Requests: []perf.Request{
					perf.BuildBasicCheck(
						attr1,
						map[string]istio_mixer_v1.CheckRequest_QuotaParams{
							"requestcount": {Amount: 1},
						}),
					perf.BuildBasicCheck(
						attr2,
						map[string]istio_mixer_v1.CheckRequest_QuotaParams{
							"requestcount": {Amount: 1},
						}),
					perf.BuildBasicCheck(
						attr3,
						map[string]istio_mixer_v1.CheckRequest_QuotaParams{
							"requestcount": {Amount: 1},
						}),
					perf.BuildBasicCheck(
						attr4,
						map[string]istio_mixer_v1.CheckRequest_QuotaParams{
							"requestcount": {Amount: 1},
						}),
					perf.BuildBasicCheck(
						attr5,
						map[string]istio_mixer_v1.CheckRequest_QuotaParams{
							"requestcount": {Amount: 1},
						}),
				},
			},
			{
				Multiplier: 1,
				Requests: []perf.Request{
					perf.BuildBasicCheck(
						attr1,
						map[string]istio_mixer_v1.CheckRequest_QuotaParams{
							"requestcount": {Amount: 1},
						}),
					perf.BuildBasicCheck(
						attr2,
						map[string]istio_mixer_v1.CheckRequest_QuotaParams{
							"requestcount": {Amount: 1},
						}),
					perf.BuildBasicCheck(
						attr3,
						map[string]istio_mixer_v1.CheckRequest_QuotaParams{
							"requestcount": {Amount: 1},
						}),
					perf.BuildBasicCheck(
						attr4,
						map[string]istio_mixer_v1.CheckRequest_QuotaParams{
							"requestcount": {Amount: 1},
						}),
					perf.BuildBasicCheck(
						attr5,
						map[string]istio_mixer_v1.CheckRequest_QuotaParams{
							"requestcount": {Amount: 1},
						}),
				},
			},
			{
				Multiplier: 1,
				Requests: []perf.Request{
					perf.BuildBasicCheck(
						attr1,
						map[string]istio_mixer_v1.CheckRequest_QuotaParams{
							"requestcount": {Amount: 1},
						}),
					perf.BuildBasicCheck(
						attr2,
						map[string]istio_mixer_v1.CheckRequest_QuotaParams{
							"requestcount": {Amount: 1},
						}),
					perf.BuildBasicCheck(
						attr3,
						map[string]istio_mixer_v1.CheckRequest_QuotaParams{
							"requestcount": {Amount: 1},
						}),
					perf.BuildBasicCheck(
						attr4,
						map[string]istio_mixer_v1.CheckRequest_QuotaParams{
							"requestcount": {Amount: 1},
						}),
					perf.BuildBasicCheck(
						attr5,
						map[string]istio_mixer_v1.CheckRequest_QuotaParams{
							"requestcount": {Amount: 1},
						}),
				},
			},
		},
	}

	perf.Run(b, &setup, settings)
	validateQuotaBehavior(spyAdapter, b)
}

// Tests 4 async client, each sending 5 identical quota call into Mixer that dispatches instances to
// multiple noop inproc adapters. The APA in this case is a slow by 1ms.
func Benchmark_Quota_4Clients_5SameCallsEach_1MilliSecSlowApa(b *testing.B) {
	settings, spyAdapter := settingsWith1milliSecApaAdapterAndTmpls()
	settings.RunMode = perf.InProcess
	setup := perf.Setup{
		Config: perf.Config{
			Global:  mixerGlobalCfg,
			Service: quotaInstToSpyAdapter + attrGenToSpyAdapter,
		},

		Loads: []perf.Load{
			{
				Multiplier: 5,
				Requests: []perf.Request{
					perf.BuildBasicCheck(
						baseAttr,
						map[string]istio_mixer_v1.CheckRequest_QuotaParams{
							"requestcount": {Amount: 1},
						}),
				},
			},
			{
				Multiplier: 5,
				Requests: []perf.Request{
					perf.BuildBasicCheck(
						baseAttr,
						map[string]istio_mixer_v1.CheckRequest_QuotaParams{
							"requestcount": {Amount: 1},
						}),
				},
			},
			{
				Multiplier: 5,
				Requests: []perf.Request{
					perf.BuildBasicCheck(
						baseAttr,
						map[string]istio_mixer_v1.CheckRequest_QuotaParams{
							"requestcount": {Amount: 1},
						}),
				},
			},
			{
				Multiplier: 5,
				Requests: []perf.Request{
					perf.BuildBasicCheck(
						baseAttr,
						map[string]istio_mixer_v1.CheckRequest_QuotaParams{
							"requestcount": {Amount: 1},
						}),
				},
			},
		},
	}

	perf.Run(b, &setup, settings)
	validateQuotaBehavior(spyAdapter, b)
}

func validateQuotaBehavior(spyAdapter *spyadapter.Adapter, b *testing.B) {
	// validate all went as expected.
	//
	// based on the config, there must be, for each quota check call from client,
	// * single attribute generation call
	// * single quota check call
	foundAttrGenCall := false
	foundQuotaCall := false
	for _, cc := range spyAdapter.HandlerData.CapturedCalls {
		if cc.Name == "HandleSampleApaAttributes" && len(cc.Instances) == 1 {
			foundAttrGenCall = true
		}
		if cc.Name == "HandleSampleQuota" && len(cc.Instances) == 1 {
			foundQuotaCall = true
		}
	}

	if !foundAttrGenCall || !foundQuotaCall {
		b.Errorf("got spy adapter calls %v; want calls  with HandleSampleApaAttributes:1 & HandleSampleQuota:1",
			spyAdapter.HandlerData.CapturedCalls)
	}
}

const (
	// contains 1 rules that pass 1 instance to a quota adapter
	quotaInstToSpyAdapter = `
apiVersion: "config.istio.io/v1alpha2"
kind: spyadapter
metadata:
  name: spyadapterHandler
  namespace: istio-system
spec:
---
apiVersion: "config.istio.io/v1alpha2"
kind: samplequota
metadata:
  name: requestcount
  namespace: istio-system
spec:
  dimensions:
    source: source.labels["app"] | source.service | "unknown"
    sourceVersion: source.labels["version"] | "unknown"
    destination: destination.labels["app"] | destination.service | "unknown"
    destinationVersion: destination.labels["version"] | "unknown"
---
apiVersion: "config.istio.io/v1alpha2"
kind: rule
metadata:
  name: quota
  namespace: istio-system
spec:
  actions:
  - handler: spyadapterHandler.spyadapter
    instances:
    - requestcount.samplequota
---
`
)
