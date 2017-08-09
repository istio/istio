// Copyright 2016 Istio Authors
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

package adapterManager

import (
	"bytes"
	"io/ioutil"
	"os"
	"testing"
	"time"

	"github.com/gogo/protobuf/types"
	"github.com/golang/protobuf/proto"

	"istio.io/mixer/adapter/noop"
	"istio.io/mixer/pkg/adapter"
	"istio.io/mixer/pkg/aspect"
	"istio.io/mixer/pkg/config"
	"istio.io/mixer/pkg/expr"
	"istio.io/mixer/pkg/pool"
	"istio.io/mixer/pkg/template"
	sample_report "istio.io/mixer/template/sample/report"
)

// The adapter implementation fills this data and test can verify what was called.
// To use this variable across tests, every test should clean this variable value.
var globalActualHandlerCallInfoToValidate map[string]interface{}

type (
	fakeHndlr     struct{}
	fakeHndlrBldr struct{}
)

func (fakeHndlrBldr) Build(cnfg proto.Message, _ adapter.Env) (adapter.Handler, error) {
	globalActualHandlerCallInfoToValidate["Build"] = cnfg
	fakeHndlrObj := fakeHndlr{}
	return fakeHndlrObj, nil
}
func (fakeHndlrBldr) ConfigureSampleHandler(typeParams map[string]*sample_report.Type) error {
	globalActualHandlerCallInfoToValidate["ConfigureSample"] = typeParams
	return nil
}
func (fakeHndlr) HandleSample(instances []*sample_report.Instance) error {
	globalActualHandlerCallInfoToValidate["ReportSample"] = instances
	return nil
}
func (fakeHndlr) Close() error {
	globalActualHandlerCallInfoToValidate["Close"] = nil
	return nil
}
func GetFakeHndlrBuilderInfo() adapter.BuilderInfo {
	return adapter.BuilderInfo{
		Name:                   "fakeHandler",
		Description:            "",
		SupportedTemplates:     []string{sample_report.TemplateName},
		CreateHandlerBuilderFn: func() adapter.HandlerBuilder { return fakeHndlrBldr{} },
		DefaultConfig:          &types.Empty{},
		ValidateConfig: func(msg proto.Message) error {
			return nil
		},
	}
}

const (
	globalCnfg = `
subject: namespace:ns
revision: "2022"
handlers:
  - name: fooHandler
    adapter: fakeHandler

manifests:
  - name: istio-proxy
    revision: "1"
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
`
	// TODO : If a value is a literal, does it have to be in quotes ?
	// Seems like if I do not put in quotes, decoding this yaml fails.
	srvConfig = `
subject: namespace:ns
action_rules:
- selector: target.name == "*"
  actions:
  - handler: fooHandler
    instances:
    - fooInstance

instances:
- name: fooInstance
  template: "istio.mixer.adapter.sample.report.Sample"
  params:
    value: "2"
    int64Primitive: attr.int64 | 2
    boolPrimitive: attr.bool | true
    doublePrimitive: attr.double | 12.4
    stringPrimitive: "\"\""
    dimensions:
      source: source.name
      target_ip: target.name
`
)

func getCnfgs() (declarativeSrvcCnfg *os.File, declaredGlobalCnfg *os.File) {
	srvcCnfgFile, _ := ioutil.TempFile("", "e2eConfigTest")
	globalCnfgFile, _ := ioutil.TempFile("", "e2eConfigTest")

	_, _ = globalCnfgFile.Write([]byte(globalCnfg))
	_ = globalCnfgFile.Close()

	var srvcCnfgBuffer bytes.Buffer
	srvcCnfgBuffer.WriteString(srvConfig)

	_, _ = srvcCnfgFile.Write([]byte(srvcCnfgBuffer.String()))
	_ = srvcCnfgFile.Close()

	return srvcCnfgFile, globalCnfgFile
}

func testConfigFlow(t *testing.T, declarativeSrvcCnfgFilePath string, declaredGlobalCnfgFilePath string) {
	globalActualHandlerCallInfoToValidate = make(map[string]interface{})
	apiPoolSize := 1024
	adapterPoolSize := 1024
	identityAttribute := "target.service"
	identityDomainAttribute := "svc.cluster.local"
	loopDelay := time.Second * 5
	singleThreadedGoRoutinePool := false

	gp := pool.NewGoroutinePool(apiPoolSize, singleThreadedGoRoutinePool)
	gp.AddWorkers(apiPoolSize)
	gp.AddWorkers(apiPoolSize)
	defer gp.Close()

	adapterGP := pool.NewGoroutinePool(adapterPoolSize, singleThreadedGoRoutinePool)
	adapterGP.AddWorkers(adapterPoolSize)
	defer adapterGP.Close()

	eval, err := expr.NewCEXLEvaluator(expr.DefaultCacheSize)
	if err != nil {
		t.Errorf("Failed to create expression evaluator: %v", err)
	}
	adapterMgr := NewManager([]adapter.RegisterFn{
		noop.Register,
	}, aspect.Inventory(), eval, gp, adapterGP)
	store, err := config.NewCompatFSStore(declaredGlobalCnfgFilePath, declarativeSrvcCnfgFilePath)
	if err != nil {
		t.Errorf("NewCompatFSStore failed: %v", err)
		return
	}

	cnfgMgr := config.NewManager(eval, adapterMgr.AspectValidatorFinder, adapterMgr.BuilderValidatorFinder, []adapter.GetBuilderInfoFn{GetFakeHndlrBuilderInfo},
		adapterMgr.SupportedKinds, template.NewRepository(template.SupportedTmplInfo), store,
		loopDelay,
		identityAttribute, identityDomainAttribute)
	cnfgMgr.Register(adapterMgr)
	cnfgMgr.Start()

	// validate globalActualHandlerCallInfoToValidate
	if len(globalActualHandlerCallInfoToValidate) != 2 {
		t.Errorf("got call count %d\nwant %d", len(globalActualHandlerCallInfoToValidate), 2)
	}

	if globalActualHandlerCallInfoToValidate["ConfigureSample"] == nil || globalActualHandlerCallInfoToValidate["Build"] == nil {
		t.Errorf("got call info as : %v. \nwant calls %s and %s to have been called", globalActualHandlerCallInfoToValidate, "ConfigureSample", "Build")
	}
}

func TestConfigFlow(t *testing.T) {
	sc, gsc := getCnfgs()
	testConfigFlow(t, sc.Name(), gsc.Name())
	_ = os.Remove(sc.Name())
	_ = os.Remove(gsc.Name())
}
