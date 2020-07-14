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
	"time"

	"istio.io/istio/mixer/pkg/adapter"
	"istio.io/istio/mixer/pkg/perf"
	spyadapter "istio.io/istio/mixer/test/spyAdapter"
	"istio.io/istio/mixer/test/spyAdapter/template"
)

// Tests single report call into Mixer that dispatches report instances to multiple noop inproc adapters.
func Benchmark_Report_1Client_1Call(b *testing.B) {
	settings, spyAdapter := settingsWithAdapterAndTmpls()
	settings.RunMode = perf.InProcess

	setup := perf.Setup{
		Config: perf.Config{
			Global:  mixerGlobalCfg,
			Service: logentryToNoop + metricsToSpyAdapter + attrGenToSpyAdapter,
		},

		Loads: []perf.Load{{
			Multiplier: 1,
			Requests: []perf.Request{
				perf.BuildBasicReport(baseAttr),
			},
		}},
	}

	perf.Run(b, &setup, settings)

	validateReportBehavior(spyAdapter, b)
}

// Tests 5 synchronous identical report call into Mixer that dispatches report instances to multiple noop inproc adapters.
func Benchmark_Report_1Client_5SameCalls(b *testing.B) {
	settings, spyAdapter := settingsWithAdapterAndTmpls()
	settings.RunMode = perf.InProcess
	setup := perf.Setup{
		Config: perf.Config{
			Global:  mixerGlobalCfg,
			Service: logentryToNoop + metricsToSpyAdapter + attrGenToSpyAdapter,
		},

		Loads: []perf.Load{{
			Multiplier: 5,
			Requests: []perf.Request{
				perf.BuildBasicReport(baseAttr),
			},
		}},
	}

	perf.Run(b, &setup, settings)

	validateReportBehavior(spyAdapter, b)
}

// Tests 5 synchronous different report call into Mixer that dispatches report instances to multiple noop inproc adapters.
func Benchmark_Report_1Client_5DifferentCalls(b *testing.B) {
	settings, spyAdapter := settingsWithAdapterAndTmpls()
	settings.RunMode = perf.InProcess
	setup := perf.Setup{
		Config: perf.Config{
			Global:  mixerGlobalCfg,
			Service: logentryToNoop + metricsToSpyAdapter + attrGenToSpyAdapter,
		},

		Loads: []perf.Load{
			{
				Multiplier: 1,
				Requests: []perf.Request{
					perf.BuildBasicReport(attr1),
					perf.BuildBasicReport(attr2),
					perf.BuildBasicReport(attr3),
					perf.BuildBasicReport(attr4),
					perf.BuildBasicReport(attr5),
				},
			},
		},
	}

	perf.Run(b, &setup, settings)

	validateReportBehavior(spyAdapter, b)
}

// Tests 4 async client, each sending 5 synchronous identical report call into Mixer that dispatches report instances to
// multiple noop inproc adapters.
func Benchmark_Report_4Clients_5SameCallsEach(b *testing.B) {
	settings, spyAdapter := settingsWithAdapterAndTmpls()
	settings.RunMode = perf.InProcess
	setup := perf.Setup{
		Config: perf.Config{
			Global:  mixerGlobalCfg,
			Service: logentryToNoop + metricsToSpyAdapter + attrGenToSpyAdapter,
		},

		Loads: []perf.Load{
			{
				Multiplier: 5,
				Requests: []perf.Request{
					perf.BuildBasicReport(baseAttr),
				},
			},
			{
				Multiplier: 5,
				Requests: []perf.Request{
					perf.BuildBasicReport(baseAttr),
				},
			},
			{
				Multiplier: 5,
				Requests: []perf.Request{
					perf.BuildBasicReport(baseAttr),
				},
			},
			{
				Multiplier: 5,
				Requests: []perf.Request{
					perf.BuildBasicReport(baseAttr),
				},
			},
		},
	}

	perf.Run(b, &setup, settings)

	validateReportBehavior(spyAdapter, b)
}

// Tests 4 async client, each sending 5 different report call into Mixer that dispatches report instances to
// multiple noop inproc adapters.
func Benchmark_Report_4Clients_5DifferentCallsEach(b *testing.B) {
	settings, spyAdapter := settingsWithAdapterAndTmpls()
	settings.RunMode = perf.InProcess
	setup := perf.Setup{
		Config: perf.Config{
			Global:  mixerGlobalCfg,
			Service: logentryToNoop + metricsToSpyAdapter + attrGenToSpyAdapter,
		},

		Loads: []perf.Load{
			{
				Multiplier: 1,
				Requests: []perf.Request{
					perf.BuildBasicReport(attr1),
					perf.BuildBasicReport(attr2),
					perf.BuildBasicReport(attr3),
					perf.BuildBasicReport(attr4),
					perf.BuildBasicReport(attr5),
				},
			},
			{
				Multiplier: 1,
				Requests: []perf.Request{
					perf.BuildBasicReport(attr1),
					perf.BuildBasicReport(attr2),
					perf.BuildBasicReport(attr3),
					perf.BuildBasicReport(attr4),
					perf.BuildBasicReport(attr5),
				},
			},
			{
				Multiplier: 1,
				Requests: []perf.Request{
					perf.BuildBasicReport(attr1),
					perf.BuildBasicReport(attr2),
					perf.BuildBasicReport(attr3),
					perf.BuildBasicReport(attr4),
					perf.BuildBasicReport(attr5),
				},
			},
			{
				Multiplier: 1,
				Requests: []perf.Request{
					perf.BuildBasicReport(attr1),
					perf.BuildBasicReport(attr2),
					perf.BuildBasicReport(attr3),
					perf.BuildBasicReport(attr4),
					perf.BuildBasicReport(attr5),
				},
			},
		},
	}

	perf.Run(b, &setup, settings)

	validateReportBehavior(spyAdapter, b)
}

// Tests 4 async client, each sending 5 identical report call into Mixer that dispatches report instances to
// multiple noop inproc adapters. The APA in this case is a slow by 1ms.
func Benchmark_Report_4Clients_5SameCallsEach_1MilliSecSlowApa(b *testing.B) {
	settings, spyAdapter := settingsWith1milliSecApaAdapterAndTmpls()
	settings.RunMode = perf.InProcess
	setup := perf.Setup{
		Config: perf.Config{
			Global:  mixerGlobalCfg,
			Service: logentryToNoop + metricsToSpyAdapter + attrGenToSpyAdapter,
		},

		Loads: []perf.Load{
			{
				Multiplier: 5,
				Requests: []perf.Request{
					perf.BuildBasicReport(baseAttr),
				},
			},
			{
				Multiplier: 5,
				Requests: []perf.Request{
					perf.BuildBasicReport(baseAttr),
				},
			},
			{
				Multiplier: 5,
				Requests: []perf.Request{
					perf.BuildBasicReport(baseAttr),
				},
			},
			{
				Multiplier: 5,
				Requests: []perf.Request{
					perf.BuildBasicReport(baseAttr),
				},
			},
		},
	}

	perf.Run(b, &setup, settings)

	validateReportBehavior(spyAdapter, b)
}

const (
	// contains 2 rules that pass logentry instances to a noop handler.
	logentryToNoop = `
apiVersion: "config.istio.io/v1alpha2"
kind: noop
metadata:
  name: handler
  namespace: istio-system
spec:
---
apiVersion: "config.istio.io/v1alpha2"
kind: logentry
metadata:
  name: accesslog
  namespace: istio-system
spec:
  severity: '"Default"'
  # timestamp: request.time
  variables:
    sourceIp: source.ip | ip("0.0.0.0")
    destinationIp: destination.ip | ip("0.0.0.0")
    sourceUser: source.user | ""
    method: request.method | ""
    url: request.path | ""
    protocol: request.scheme | "http"
    responseCode: response.code | 0
    responseSize: response.size | 0
    requestSize: request.size | 0
    latency: response.duration | "0ms"
    connectionMtls: connection.mtls | false
---
apiVersion: "config.istio.io/v1alpha2"
kind: rule
metadata:
  name: stdio
  namespace: istio-system
spec:
  match: "true" # If omitted match is true.
  actions:
  - handler: handler.noop
    instances:
    - accesslog.logentry
---

# Configuration for logentry instances
apiVersion: "config.istio.io/v1alpha2"
kind: logentry
metadata:
  name: newlog
  namespace: istio-system
spec:
  severity: '"warning"'
  #timestamp: request.time
  variables:
    source: source.labels["app"] | source.service | "unknown"
    user: source.user | "unknown"
    destination: destination.labels["app"] | destination.service | "unknown"
    responseCode: response.code | 0
    responseSize: response.size | 0
    latency: response.duration | "0ms"
---
# Configuration for a stdio handler
apiVersion: "config.istio.io/v1alpha2"
kind: noop
metadata:
  name: newhandler
  namespace: istio-system
spec:
---
# Rule to send logentry instances to a stdio handler
apiVersion: "config.istio.io/v1alpha2"
kind: rule
metadata:
  name: newlogstdio
  namespace: istio-system
spec:
  match: "true" # match for all requests
  actions:
   - handler: newhandler.noop
     instances:
     - newlog.logentry
---
`

	// contains 2 rules that pass instances to the same handler.
	metricsToSpyAdapter = `
apiVersion: "config.istio.io/v1alpha2"
kind: samplereport
metadata:
  name: requestcount
  namespace: istio-system
spec:
  value: "1"
  dimensions:
    source_service: source.service | "unknown"
    source_version: source.labels["version"] | "unknown"
    destination_service: destination.service | "unknown"
    destination_version: destination.labels["version"] | "unknown"
    response_code: response.code | 200
    connection_mtls: connection.mtls | false
---
apiVersion: "config.istio.io/v1alpha2"
kind: samplereport
metadata:
  name: requestduration
  namespace: istio-system
spec:
  value: response.duration | "0ms"
  dimensions:
    source_service: source.service | "unknown"
    source_version: source.labels["version"] | "unknown"
    destination_service: destination.service | "unknown"
    destination_version: destination.labels["version"] | "unknown"
    response_code: response.code | 200
    connection_mtls: connection.mtls | false
---
apiVersion: "config.istio.io/v1alpha2"
kind: samplereport
metadata:
  name: requestsize
  namespace: istio-system
spec:
  value: request.size | 0
  dimensions:
    source_service: source.service | "unknown"
    source_version: source.labels["version"] | "unknown"
    destination_service: destination.service | "unknown"
    destination_version: destination.labels["version"] | "unknown"
    response_code: response.code | 200
    connection_mtls: connection.mtls | false
---
apiVersion: "config.istio.io/v1alpha2"
kind: samplereport
metadata:
  name: responsesize
  namespace: istio-system
spec:
  value: response.size | 0
  dimensions:
    source_service: source.service | "unknown"
    source_version: source.labels["version"] | "unknown"
    destination_service: destination.service | "unknown"
    destination_version: destination.labels["version"] | "unknown"
    response_code: response.code | 200
    connection_mtls: connection.mtls | false
---
apiVersion: "config.istio.io/v1alpha2"
kind: samplereport
metadata:
  name: tcpbytesent
  namespace: istio-system
spec:
  value: connection.sent.bytes | 0
  dimensions:
    source_service: source.service | "unknown"
    source_version: source.labels["version"] | "unknown"
    destination_service: destination.service | "unknown"
    destination_version: destination.labels["version"] | "unknown"
    connection_mtls: connection.mtls | false
---
apiVersion: "config.istio.io/v1alpha2"
kind: samplereport
metadata:
  name: tcpbytereceived
  namespace: istio-system
spec:
  value: connection.received.bytes | 0
  dimensions:
    source_service: source.service | "unknown"
    source_version: source.labels["version"] | "unknown"
    destination_service: destination.service | "unknown"
    destination_version: destination.labels["version"] | "unknown"
    connection_mtls: connection.mtls | false
---
apiVersion: "config.istio.io/v1alpha2"
kind: spyadapter
metadata:
  name: handler
  namespace: istio-system
spec:
---
apiVersion: "config.istio.io/v1alpha2"
kind: rule
metadata:
  name: promhttp
  namespace: istio-system
spec:
  match: context.protocol == "http"
  actions:
  - handler: handler.spyadapter
    instances:
    - requestcount.samplereport
    - requestduration.samplereport
    - requestsize.samplereport
    - responsesize.samplereport
---
apiVersion: "config.istio.io/v1alpha2"
kind: rule
metadata:
  name: promtcp
  namespace: istio-system
spec:
  match: context.protocol == "tcp"
  actions:
  - handler: handler.spyadapter
    instances:
    - tcpbytesent.samplereport
    - tcpbytereceived.samplereport
---

# Configuration for metric instances
apiVersion: "config.istio.io/v1alpha2"
kind: samplereport
metadata:
  name: doublerequestcount
  namespace: istio-system
spec:
  value: "2" # count each request twice
  dimensions:
    source: source.service | "unknown"
    destination: destination.service | "unknown"
    message: '"twice the fun!"'
---
# Configuration for a Prometheus handler
apiVersion: "config.istio.io/v1alpha2"
kind: spyadapter
metadata:
  name: doublehandler
  namespace: istio-system
spec:
---
# Rule to send metric instances to a Prometheus handler
apiVersion: "config.istio.io/v1alpha2"
kind: rule
metadata:
  name: doubleprom
  namespace: istio-system
spec:
  actions:
  - handler: doublehandler.spyadapter
    instances:
    - doublerequestcount.samplereport
---
`

	// contains 1 rules that pass instances to a apa adapter
	attrGenToSpyAdapter = `
apiVersion: "config.istio.io/v1alpha2"
kind: spyadapter
metadata:
  name: handler
  namespace: istio-system
spec:

---
apiVersion: "config.istio.io/v1alpha2"
kind: rule
metadata:
  name: kubeattrgenrulerule
  namespace: istio-system
spec:
  actions:
  - handler: handler.spyadapter
    instances:
    - attributes.sampleapa
---
apiVersion: "config.istio.io/v1alpha2"
kind: sampleapa
metadata:
  name: attributes
  namespace: istio-system
spec:
  # Pass the required attribute data to the adapter
  boolPrimitive: connection.mtls | true
  stringPrimitive: source.service | "unknown"
  attribute_bindings:
    connection.mtls: $out.boolPrimitive | true
    origin.uid: $out.stringPrimitive | "unknown"
---
`

	mixerGlobalCfg = `
apiVersion: "config.istio.io/v1alpha2"
kind: attributemanifest
metadata:
  name: istioproxy
  namespace: istio-system
spec:
  attributes:
    origin.ip:
      valueType: IP_ADDRESS
    origin.uid:
      valueType: STRING
    origin.user:
      valueType: STRING
    request.headers:
      valueType: STRING_MAP
    request.id:
      valueType: STRING
    request.host:
      valueType: STRING
    request.method:
      valueType: STRING
    request.path:
      valueType: STRING
    request.reason:
      valueType: STRING
    request.referer:
      valueType: STRING
    request.scheme:
      valueType: STRING
    request.size:
      valueType: INT64
    request.time:
      valueType: TIMESTAMP
    request.useragent:
      valueType: STRING
    response.code:
      valueType: INT64
    response.duration:
      valueType: DURATION
    response.headers:
      valueType: STRING_MAP
    response.size:
      valueType: INT64
    response.time:
      valueType: TIMESTAMP
    source.uid:
      valueType: STRING
    source.user:
      valueType: STRING
    destination.uid:
      valueType: STRING
    connection.id:
      valueType: STRING
    connection.received.bytes:
      valueType: INT64
    connection.received.bytes_total:
      valueType: INT64
    connection.sent.bytes:
      valueType: INT64
    connection.sent.bytes_total:
      valueType: INT64
    connection.duration:
      valueType: DURATION
    connection.mtls:
      valueType: BOOL
    context.protocol:
      valueType: STRING
    context.timestamp:
      valueType: TIMESTAMP
    context.time:
      valueType: TIMESTAMP
    api.service:
      valueType: STRING
    api.version:
      valueType: STRING
    api.operation:
      valueType: STRING
    api.protocol:
      valueType: STRING
    request.auth.principal:
      valueType: STRING
    request.auth.audiences:
      valueType: STRING
    request.auth.presenter:
      valueType: STRING
    request.api_key:
      valueType: STRING

---
apiVersion: "config.istio.io/v1alpha2"
kind: attributemanifest
metadata:
  name: kubernetes
  namespace: istio-system
spec:
  attributes:
    source.ip:
      valueType: IP_ADDRESS
    source.labels:
      valueType: STRING_MAP
    source.name:
      valueType: STRING
    source.namespace:
      valueType: STRING
    source.service:
      valueType: STRING
    source.serviceAccount:
      valueType: STRING
    destination.ip:
      valueType: IP_ADDRESS
    destination.labels:
      valueType: STRING_MAP
    destination.name:
      valueType: STRING
    destination.namespace:
      valueType: STRING
    destination.service:
      valueType: STRING
    destination.serviceAccount:
      valueType: STRING
---
`
)

func settingsWithAdapterAndTmpls() (perf.Settings, *spyadapter.Adapter) {
	setting := baseSettings
	a := spyadapter.NewSpyAdapter(spyadapter.AdapterBehavior{Name: "spyadapter", Handler: spyadapter.HandlerBehavior{
		HandleSampleCheckResult: adapter.CheckResult{ValidUseCount: 10000, ValidDuration: 5 * time.Minute}}})
	setting.Adapters = append(setting.Adapters, a.GetAdptInfoFn())
	for k, v := range template.SupportedTmplInfo {
		setting.Templates[k] = v
	}
	return setting, a
}

func settingsWith1milliSecApaAdapterAndTmpls() (perf.Settings, *spyadapter.Adapter) {
	setting := baseSettings

	a := spyadapter.NewSpyAdapter(spyadapter.AdapterBehavior{Name: "spyadapter",
		Handler: spyadapter.HandlerBehavior{GenerateSampleApaSleep: time.Millisecond,
			HandleSampleCheckResult: adapter.CheckResult{ValidUseCount: 10000, ValidDuration: 5 * time.Minute}}})
	setting.Adapters = append(setting.Adapters, a.GetAdptInfoFn())
	for k, v := range template.SupportedTmplInfo {
		setting.Templates[k] = v
	}
	return setting, a
}

func validateReportBehavior(spyAdapter *spyadapter.Adapter, b *testing.B) {
	// validate all went as expected. Note: logentry goes to the noop adapter so we cannot inspect that. However,
	// anything that is going to spy adapter, based on the config below, can be inspected.
	//
	// based on the config, there must be, for each Report call from client,
	// * single attribute generation call
	// * two metric handle call
	//   * with 4 instances.
	//   * with 1 instance.

	foundAttrGenCall := false
	foundReport1InstCall := false
	foundReport4InstCall := false

	for _, cc := range spyAdapter.HandlerData.CapturedCalls {
		if cc.Name == "HandleSampleApaAttributes" && len(cc.Instances) == 1 {
			foundAttrGenCall = true
		}
		if cc.Name == "HandleSampleReport" && len(cc.Instances) == 1 {
			foundReport1InstCall = true
		}
		if cc.Name == "HandleSampleReport" && len(cc.Instances) == 4 {
			foundReport4InstCall = true
		}
	}

	if !foundAttrGenCall || !foundReport1InstCall || !foundReport4InstCall {
		b.Errorf("got spy adapter calls %v; want calls  with HandleSampleApaAttributes:1 & HandleSampleReport:1"+
			"& HandleSampleReport:4",
			spyAdapter.HandlerData.CapturedCalls)
	}
}
