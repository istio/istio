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
	"fmt"
	"io/ioutil"
	"path"
	"path/filepath"
	"runtime"
	"testing"

	adptModel "istio.io/api/mixer/adapter/model/v1beta1"
	"istio.io/istio/mixer/pkg/perf"
	spyadapter "istio.io/istio/mixer/test/spyAdapter"
	reportTmpl "istio.io/istio/mixer/test/spyAdapter/template/report"
	"istio.io/istio/mixer/test/spybackend"
)

// Test single report dispatch to IBP spybackend.
func Benchmark_IBP_Adapter_Report(b *testing.B) {
	s := initOOPAdapter()
	defer s.Close()
	oopAdapterCfg, err := getOOPAdapterAndTempls()
	setting := perf.Settings{
		RunMode: perf.InProcessBypassGrpc,
	}
	if err != nil {
		b.Errorf("cannot load spyadapter template config %v", err)
	}
	setup := perf.Setup{
		Config: perf.Config{
			Global:  mixerGlobalCfg,
			Service: oopAdapterCfg + metricsToOOPSpyAdapter,
		},

		Loads: reportLoad,
	}

	perf.Run(b, &setup, setting)
	validateOOPReportBehavior(s, b)
}

// Test single report dispatch to in process spyadapter.
func Benchmark_Inproc_Adapter_Report(b *testing.B) {
	settings, spyAdapter := settingsWithAdapterAndTmpls()
	settings.RunMode = perf.InProcessBypassGrpc
	setup := perf.Setup{
		Config: perf.Config{
			Global:  mixerGlobalCfg,
			Service: metricsToInprocSpyAdapter,
		},

		Loads: reportLoad,
	}

	perf.Run(b, &setup, settings)
	validateInprocReportBehavior(spyAdapter, b)
}

func getOOPAdapterAndTempls() (string, error) {
	_, filename, _, _ := runtime.Caller(0)
	dymanicTemplates := []string{
		"../spyAdapter/template/report/reporttmpl.yaml",
		"../spyAdapter/template/quota/quotatmpl.yaml",
		"../spyAdapter/template/check/tmpl.yaml",
		"../spybackend/nosession-perf.yaml",
	}

	data := ""
	for _, fileRelativePath := range dymanicTemplates {
		if f, err := filepath.Abs(path.Join(path.Dir(filename), fileRelativePath)); err != nil {
			return "", fmt.Errorf("cannot load attributes.yaml: %v", err)
		} else if f, err := ioutil.ReadFile(f); err != nil {
			return "", fmt.Errorf("cannot load attributes.yaml: %v", err)
		} else {
			data += string(f)
		}
	}
	return data, nil
}

func initOOPAdapter() spybackend.Server {
	a := &spybackend.Args{
		Behavior: &spybackend.Behavior{
			HandleSampleReportResult: &adptModel.ReportResult{},
		},
		Requests: &spybackend.Requests{
			HandleSampleReportRequest: []*reportTmpl.HandleSampleReportRequest{},
		},
	}
	s, _ := spybackend.NewNoSessionServer(a)
	s.Run()
	return s
}

func validateOOPReportBehavior(s spybackend.Server, b *testing.B) {
	cc := s.GetCapturedCalls()

	for _, e := range cc {
		if e.Name == "HandleSampleReport" && len(e.Instances) == 1 {
			return
		}
	}
	b.Errorf("got spy adapter calls %v; want calls HandleSampleReport:1", cc)
}

func validateInprocReportBehavior(spyAdapter *spyadapter.Adapter, b *testing.B) {
	for _, cc := range spyAdapter.HandlerData.CapturedCalls {
		if cc.Name == "HandleSampleReport" && len(cc.Instances) == 1 {
			return
		}
	}

	b.Errorf("got spy adapter calls %v; want calls with HandleSampleReport:1",
		spyAdapter.HandlerData.CapturedCalls)
}

var reportLoad = []perf.Load{
	{
		Multiplier: 1,
		Requests: []perf.Request{
			perf.BuildBasicReport(attr1),
		},
	},
}

const (
	metricsToOOPSpyAdapter = `
apiVersion: "config.istio.io/v1alpha2"
kind: instance
metadata:
  name: requestcount
  namespace: istio-system
spec:
  template: report
  params:
    value: "1"
    dimensions:
      source_service: source.service | "unknown"
      source_version: source.labels["version"] | "unknown"
      destination_service: destination.service | "unknown"
      destination_version: destination.labels["version"] | "unknown"
      response_code: response.code | 200
      connection_mtls: connection.mtls | false
      request_host: request.host | "unknown"
      request_method: request.method | "unknown"
      request_path: request.path | "unknown"
      request_scheme: request.scheme | "unknown"
      request_useragent: request.useragent | "unknown"
      context_protocol: context.protocol | "unknown"
      destination_uid: destination.uid | "unknown"
---
apiVersion: "config.istio.io/v1alpha2"
kind: handler
metadata:
  name: spyadapter
  namespace: istio-system
spec:
  adapter: spybackend-nosession
  connection:
    address: "localhost:50051"
---
apiVersion: "config.istio.io/v1alpha2"
kind: rule
metadata:
  name: spyadapter-rule
  namespace: istio-system
spec:
  match: context.protocol == "http"
  actions:
  - handler: spyadapter.handler
    instances:
    - requestcount.instance
---
`

	metricsToInprocSpyAdapter = `
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
    request_host: request.host | "unknown"
    request_method: request.method | "unknown"
    request_path: request.path | "unknown"
    request_scheme: request.scheme | "unknown"
    request_useragent: request.useragent | "unknown"
    context_protocol: context.protocol | "unknown"
    destination_uid: destination.uid | "unknown"
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
---
`
)
