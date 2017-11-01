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

package e2e

import (
	"context"
	"io"
	"log"
	"os"
	"testing"

	istio_mixer_v1 "istio.io/api/mixer/v1"
	pb "istio.io/api/mixer/v1/config/descriptor"
	"istio.io/istio/mixer/pkg/template"
	spyAdapter "istio.io/istio/mixer/test/spyAdapter"
	e2eTmpl "istio.io/istio/mixer/test/template"
	reportTmpl "istio.io/istio/mixer/test/template/report"
	testEnv "istio.io/istio/mixer/test/testenv"
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

type testData struct {
	name      string
	cfg       string
	behaviors []spyAdapter.AdapterBehavior
	templates map[string]template.Info
	attrs     map[string]interface{}
	validate  func(t *testing.T, err error, sypAdpts []*spyAdapter.Adapter)
}

func closeHelper(c io.Closer) {
	err := c.Close()
	if err != nil {
		log.Fatal(err)
	}
}

func TestReport(t *testing.T) {
	tests := []testData{
		{
			name:      "Report",
			cfg:       reportTestCfg,
			behaviors: []spyAdapter.AdapterBehavior{{Name: "fakeHandler"}},
			templates: e2eTmpl.SupportedTmplInfo,
			attrs:     map[string]interface{}{"target.name": "somesrvcname"},
			validate: func(t *testing.T, err error, spyAdpts []*spyAdapter.Adapter) {

				adptr := spyAdpts[0]

				CmpMapAndErr(t, "SetSampleReportTypes input", adptr.BuilderData.SetSampleReportTypesTypes,
					map[string]interface{}{
						"reportInstance.samplereport.istio-system": &reportTmpl.Type{
							Value:      pb.INT64,
							Dimensions: map[string]pb.ValueType{"source": pb.STRING, "target_ip": pb.STRING},
						},
					},
				)

				CmpSliceAndErr(t, "HandleSampleReport input", adptr.HandlerData.HandleSampleReportInstances,
					[]*reportTmpl.Instance{
						{
							Name:       "reportInstance.samplereport.istio-system",
							Value:      int64(2),
							Dimensions: map[string]interface{}{"source": "mysrc", "target_ip": "somesrvcname"},
						},
					},
				)
			},
		},
	}
	for _, tt := range tests {
		configDir := GetCfgs(tt.cfg, globalCfg)
		defer func() {
			if !t.Failed() {
				_ = os.RemoveAll(configDir)
			} else {
				t.Logf("The configs are located at %s", configDir)
			}
		}() // nolint: gas

		var args = testEnv.Args{
			// Start Mixer server on a free port on loop back interface
			MixerServerAddr:               `127.0.0.1:0`,
			ConfigStoreURL:                `fs://` + configDir,
			ConfigStore2URL:               `fs://` + configDir,
			ConfigDefaultNamespace:        "istio-system",
			ConfigIdentityAttribute:       "destination.service",
			ConfigIdentityAttributeDomain: "svc.cluster.local",
			UseAstEvaluator:               false,
		}

		adapterInfos, spyAdapters := ConstructAdapterInfos(tt.behaviors)
		env, err := testEnv.NewEnv(&args, e2eTmpl.SupportedTmplInfo, adapterInfos)
		if err != nil {
			t.Fatalf("fail to create testenv: %v", err)
		}

		defer closeHelper(env)

		client, conn, err := env.CreateMixerClient()
		if err != nil {
			t.Fatalf("fail to create client connection: %v", err)
		}
		defer closeHelper(conn)

		req := istio_mixer_v1.ReportRequest{
			Attributes: []istio_mixer_v1.CompressedAttributes{
				testEnv.GetAttrBag(tt.attrs,
					args.ConfigIdentityAttribute,
					args.ConfigIdentityAttributeDomain)},
		}
		_, err = client.Report(context.Background(), &req)

		tt.validate(t, err, spyAdapters)
	}
}
