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
	"fmt"
	"reflect"
	"testing"

	"github.com/davecgh/go-spew/spew"
	"google.golang.org/grpc"

	istio_mixer_v1 "istio.io/api/mixer/v1"
	"istio.io/istio/mixer/pkg/config/storetest"
	testEnv "istio.io/istio/mixer/pkg/server"
	spyAdapter "istio.io/istio/mixer/test/spyAdapter"
	e2eTmpl "istio.io/istio/mixer/test/spyAdapter/template"
	apaTmpl "istio.io/istio/mixer/test/spyAdapter/template/apa"
	reportTmpl "istio.io/istio/mixer/test/spyAdapter/template/report"
)

const (
	apaGlobalCfg = `
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
      generated.string:
        value_type: STRING
      generated.int64:
        value_type: INT64
      generated.bool:
        value_type: BOOL
      generated.double:
        value_type: DOUBLE
      generated.strmap:
        value_type: STRING_MAP
---
`
	apaSvcCfg = `
apiVersion: "config.istio.io/v1alpha2"
kind: fakeHandler
metadata:
  name: fakeHandlerConfig
  namespace: istio-system

---
# YAML for apa
apiVersion: "config.istio.io/v1alpha2"
kind: sampleapa
metadata:
  name: apaInstance
  namespace: istio-system
spec:
  int64Primitive: "2"
  boolPrimitive: "true"
  doublePrimitive: "2.2"
  stringPrimitive: "\"mysrc\""
  attribute_bindings:
    generated.string: $out.stringPrimitive
    generated.bool: $out.boolPrimitive
    generated.double: $out.doublePrimitive
    generated.int64: $out.int64Primitive
    generated.strmap: $out.stringMap
---

apiVersion: "config.istio.io/v1alpha2"
kind: rule
metadata:
  name: rule2
  namespace: istio-system
spec:
  selector: match(target.name, "*")
  actions:
  - handler: fakeHandlerConfig.fakeHandler
    instances:
    - apaInstance.sampleapa

---
# YAML for report that depend on APA output
apiVersion: "config.istio.io/v1alpha2"
kind: samplereport
metadata:
  name: reportInstance
  namespace: istio-system
spec:
  value: "2"
  dimensions:
    genStr: generated.string
    genBool: generated.bool
    genDouble: generated.double
    genInt64: generated.int64
    genStrMapEntry: generated.strmap["k1"]

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

func TestApa(t *testing.T) {
	tests := []testData{
		{
			name: "Apa",
			cfg:  apaSvcCfg,
			behaviors: []spyAdapter.AdapterBehavior{
				{
					Name: "fakeHandler",
					Handler: spyAdapter.HandlerBehavior{
						GenerateSampleApaOutput: &apaTmpl.Output{
							StringPrimitive: "gen-str",
							Int64Primitive:  int64(1000),
							DoublePrimitive: float64(1000.1000),
							BoolPrimitive:   true,
							StringMap: map[string]string{
								"k1": "v1",
							},
						},
					},
				},
			},
			templates: e2eTmpl.SupportedTmplInfo,
			attrs:     map[string]interface{}{"target.name": "somesrvcname"},
			validate: func(t *testing.T, err error, spyAdpts []*spyAdapter.Adapter) {

				adptr := spyAdpts[0]

				// check if apa was called with right values
				want := &apaTmpl.Instance{
					Name:            "apaInstance.sampleapa.istio-system",
					BoolPrimitive:   true,
					DoublePrimitive: float64(2.2),
					Int64Primitive:  int64(2),
					StringPrimitive: "mysrc",
				}
				if !reflect.DeepEqual(adptr.HandlerData.GenerateSampleApaInstance, want) {
					t.Errorf(fmt.Sprintf("Not equal -> %s.\nActual :\n%s\n\nExpected :\n%s",
						"GenerateSampleApaAttributes",
						spew.Sdump(adptr.HandlerData.GenerateSampleApaInstance), spew.Sdump(want)))
					return
				}

				// The labes in report instance config use the values of attributes generated from APA. Validate
				// the values.
				CmpSliceAndErr(t, "HandleSampleReport input", adptr.HandlerData.HandleSampleReportInstances,
					[]*reportTmpl.Instance{
						{
							Name:  "reportInstance.samplereport.istio-system",
							Value: int64(2),
							Dimensions: map[string]interface{}{
								"genStr":         "gen-str",
								"genBool":        true,
								"genDouble":      float64(1000.1),
								"genInt64":       int64(1000),
								"genStrMapEntry": "v1",
							},
						},
					},
				)
			},
		},
	}
	for _, tt := range tests {
		adapterInfos, spyAdapters := ConstructAdapterInfos(tt.behaviors)

		args := testEnv.NewArgs()
		args.APIPort = 0
		args.MonitoringPort = 0
		args.Templates = e2eTmpl.SupportedTmplInfo
		args.Adapters = adapterInfos
		var cerr error
		if args.ConfigStore, cerr = storetest.SetupStoreForTest(apaGlobalCfg, tt.cfg); cerr != nil {
			t.Fatal(cerr)
		}

		env, err := testEnv.New(args)
		if err != nil {
			t.Fatalf("fail to create mixer: %v", err)
		}

		env.Run()

		defer closeHelper(env)

		conn, err := grpc.Dial(env.Addr().String(), grpc.WithInsecure())
		if err != nil {
			t.Fatalf("Unable to connect to gRPC server: %v", err)
		}

		client := istio_mixer_v1.NewMixerClient(conn)
		defer closeHelper(conn)

		req := istio_mixer_v1.ReportRequest{
			Attributes: []istio_mixer_v1.CompressedAttributes{
				getAttrBag(tt.attrs,
					args.ConfigIdentityAttribute,
					args.ConfigIdentityAttributeDomain)},
		}
		_, err = client.Report(context.Background(), &req)
		if err == nil {
			tt.validate(t, err, spyAdapters)
		} else {
			t.Errorf("Got error '%v', want success", err)
		}
	}
}
