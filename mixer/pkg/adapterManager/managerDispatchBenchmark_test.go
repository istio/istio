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
	"context"
	"io/ioutil"
	"os"
	"testing"
	"time"

	google_rpc "github.com/googleapis/googleapis/google/rpc"

	"istio.io/mixer/adapter/noop"
	pkgAdapter "istio.io/mixer/pkg/adapter"
	"istio.io/mixer/pkg/aspect"
	"istio.io/mixer/pkg/attribute"
	"istio.io/mixer/pkg/config"
	"istio.io/mixer/pkg/expr"
	"istio.io/mixer/pkg/pool"
)

/*
Benchmark tests in this file measure time between start of adapterManager/manager.go:dispatchReport and
end of metricManager.Execute method (adapter is stubbed out). NOTE: The test does not measure the load config code
that happens for every call inside check/report/quota methods of adapterManager/manager.go.

How to run the test:
# make sure the code builds and works with go tools and not just with bazel.

Command:
go test -run XXXX -bench .

Output: (2017-04-27)
BenchmarkOneSimpleAspect-12     	   10000	    113880 ns/op
Benchmark50SimpleAspect-12      	    1000	   1410848 ns/op
BenchmarkOneComplexAspect-12    	   10000	    157771 ns/op
Benchmark50ComplexAspect-12     	     500	   2757074 ns/op
PASS
ok  	istio.io/mixer/pkg/adapterManager	6.202s
*/

const (
	minimalGlobalCnfg = `
subject: namespace:ns
revision: "2022"
adapters:
  - name: no-op
    kind: metrics
    impl: no-op

manifests:
  - name: istio-proxy
    revision: "1"
    attributes:
    - name: source.name
      value_type: STRING
    - name: target.name
      value_type: STRING
    - name: response.code
      value_type: INT64
    - name: api.name
      value_type: STRING
    - name: api.method
      value_type: STRING

metrics:
  - name: request_count
    kind: COUNTER
    value: INT64
    description: request count by source, target, service, and code
    labels:
      source: 1 # STRING
      target: 1 # STRING
      service: 1 # STRING
      method: 1 # STRING
      response_code: 2 # INT64
  - name: request_latency
    kind: COUNTER
    value: DURATION
    description: request latency by source, target, and service
    labels:
      source: 1 # STRING
      target: 1 # STRING
      service: 1 # STRING
      method: 1 # STRING
      response_code: 2 # INT64
`
	srvcCnfgConstInitialSection = `
subject: namespace:ns
rules:
- selector: true
  aspects:
`
	srvcCnfgSimpleAspect = `
  - kind: metrics
    adapter: no-op
    params:
      metrics:
      - descriptorName: request_count
        value: response.code | 100
        labels:
          source: source.name | "one"
          target: target.name | "one"
          service: api.name | "one"
          method: api.method | "one"
          response_code: response.code | 111
`

	srvcCnfgComplexAspect = `
  - kind: metrics
    adapter: no-op
    params:
      metrics:
      - descriptorName: request_count
        value: response.code | 100
        labels:
          source: source.name | target.name | source.name | target.name | "one"
          target: source.name | target.name | source.name | "one"
          service: target.name | api.name | target.name | "one"
          method: api.method | target.name | api.method | target.name | "one"
          response_code: response.code | response.code | response.code | response.code | response.code | response.code | response.code | response.code | 111
`
)

func createYamlConfigs(srvcCnfgAspect string, configRepeatCount int) (declarativeSrvcCnfg *os.File, declaredGlobalCnfg *os.File) {
	srvcCnfgFile, _ := ioutil.TempFile("", "managerDispatchBenchmarkTest")
	globalCnfgFile, _ := ioutil.TempFile("", "managerDispatchBenchmarkTest")

	_, _ = globalCnfgFile.Write([]byte(minimalGlobalCnfg))
	_ = globalCnfgFile.Close()

	var srvcCnfgBuffer bytes.Buffer
	srvcCnfgBuffer.WriteString(srvcCnfgConstInitialSection)
	for i := 0; i < configRepeatCount; i++ {
		srvcCnfgBuffer.WriteString(srvcCnfgAspect)
	}
	_, _ = srvcCnfgFile.Write([]byte(srvcCnfgBuffer.String()))
	_ = srvcCnfgFile.Close()

	return srvcCnfgFile, globalCnfgFile
}

var rpcStatus google_rpc.Status

func benchmarkAdapterManagerDispatch(b *testing.B, declarativeSrvcCnfgFilePath string, declaredGlobalCnfgFilePath string) {
	apiPoolSize := 1024
	adapterPoolSize := 1024
	identityAttribute := "target.service"
	identityDomainAttribute := "svc.cluster.local"
	loopDelay := time.Second * time.Duration(5)
	singleThreadedGoRoutinePool := false

	gp := pool.NewGoroutinePool(apiPoolSize, singleThreadedGoRoutinePool)
	gp.AddWorkers(apiPoolSize)
	gp.AddWorkers(apiPoolSize)
	defer gp.Close()

	adapterGP := pool.NewGoroutinePool(adapterPoolSize, singleThreadedGoRoutinePool)
	adapterGP.AddWorkers(adapterPoolSize)
	defer adapterGP.Close()

	eval := expr.NewCEXLEvaluator()
	adapterMgr := NewManager([]pkgAdapter.RegisterFn{
		noop.Register,
	}, aspect.Inventory(), eval, gp, adapterGP)
	store, _ := config.NewCompatFSStore(declaredGlobalCnfgFilePath, declarativeSrvcCnfgFilePath)

	cnfgMgr := config.NewManager(eval, adapterMgr.AspectValidatorFinder, adapterMgr.BuilderValidatorFinder,
		adapterMgr.SupportedKinds, store,
		loopDelay,
		identityAttribute, identityDomainAttribute)
	cnfgMgr.Register(adapterMgr)
	cnfgMgr.Start()

	requestBag := attribute.GetMutableBag(nil)
	requestBag.Set(identityAttribute, identityDomainAttribute)
	configs, _ := adapterMgr.loadConfigs(requestBag, adapterMgr.reportKindSet, false, false)

	b.ResetTimer()
	var r google_rpc.Status
	for n := 0; n < b.N; n++ {
		r = adapterMgr.dispatchReport(context.Background(), configs, requestBag, attribute.GetMutableBag(nil))
	}
	rpcStatus = r
}

func BenchmarkOneSimpleAspect(b *testing.B) {
	sc, gsc := createYamlConfigs(srvcCnfgSimpleAspect, 1)
	benchmarkAdapterManagerDispatch(b, sc.Name(), gsc.Name())
	_ = os.Remove(sc.Name())
	_ = os.Remove(gsc.Name())
}

func Benchmark50SimpleAspect(b *testing.B) {
	sc, gsc := createYamlConfigs(srvcCnfgSimpleAspect, 50)
	benchmarkAdapterManagerDispatch(b, sc.Name(), gsc.Name())
	_ = os.Remove(sc.Name())
	_ = os.Remove(gsc.Name())
}

func BenchmarkOneComplexAspect(b *testing.B) {
	sc, gsc := createYamlConfigs(srvcCnfgComplexAspect, 1)
	benchmarkAdapterManagerDispatch(b, sc.Name(), gsc.Name())
	_ = os.Remove(sc.Name())
	_ = os.Remove(gsc.Name())
}

func Benchmark50ComplexAspect(b *testing.B) {
	sc, gsc := createYamlConfigs(srvcCnfgComplexAspect, 50)
	benchmarkAdapterManagerDispatch(b, sc.Name(), gsc.Name())
	_ = os.Remove(sc.Name())
	_ = os.Remove(gsc.Name())
}
