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

package server

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"testing"

	"google.golang.org/grpc"

	mixerpb "istio.io/api/mixer/v1"
	"istio.io/istio/mixer/pkg/adapter"
	"istio.io/istio/mixer/pkg/config/store"
	"istio.io/istio/mixer/pkg/config/storetest"
	"istio.io/istio/mixer/pkg/expr"
	"istio.io/istio/mixer/pkg/il/evaluator"
	"istio.io/istio/mixer/pkg/pool"
	mixerRuntime "istio.io/istio/mixer/pkg/runtime"
	mixerRuntime2 "istio.io/istio/mixer/pkg/runtime2"
	"istio.io/istio/mixer/pkg/template"
	generatedTmplRepo "istio.io/istio/mixer/template"
	"istio.io/istio/pkg/log"
	"istio.io/istio/pkg/tracing"
	"istio.io/istio/pkg/version"
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
	serviceCfg = `
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
    target_ip: destination.name | "mytarget"

---

apiVersion: "config.istio.io/v1alpha2"
kind: rule
metadata:
  name: rule1
  namespace: istio-system
spec:
  selector: match(destination.name, "*")
  actions:
  - handler: fakeHandlerConfig.fakeHandler
    instances:
    - reportInstance.samplereport

---
`
)

// createClient returns a Mixer gRPC client, useful for tests
func createClient(addr net.Addr) (mixerpb.MixerClient, error) {
	conn, err := grpc.Dial(addr.String(), grpc.WithInsecure())
	if err != nil {
		return nil, err
	}

	return mixerpb.NewMixerClient(conn), nil
}

func newTestServer(globalCfg, serviceCfg string, useNewRuntime bool) (*Server, error) {
	a := DefaultArgs()
	a.UseNewRuntime = useNewRuntime
	a.APIPort = 0
	a.MonitoringPort = 0
	a.LoggingOptions.LogGrpc = false // Avoid introducing a race to the server tests.
	a.EnableProfiling = true
	a.Templates = generatedTmplRepo.SupportedTmplInfo
	a.LivenessProbeOptions.Path = "abc"
	a.LivenessProbeOptions.UpdateInterval = 2
	a.ReadinessProbeOptions.Path = "def"
	a.ReadinessProbeOptions.UpdateInterval = 3
	var err error
	if a.ConfigStore, err = storetest.SetupStoreForTest(globalCfg, serviceCfg); err != nil {
		return nil, err
	}
	return New(a)
}

func TestBasic(t *testing.T) {
	runtimes := []struct {
		name         string
		useNewRutime bool
	}{
		{"old runtime", false},
		{"new runtime", true},
	}

	for _, runtime := range runtimes {
		t.Run(runtime.name, func(t *testing.T) {
			s, err := newTestServer(globalCfg, serviceCfg, runtime.useNewRutime)
			if err != nil {
				t.Fatalf("Unable to create server: %v", err)
			}

			d := s.Dispatcher()
			if d != s.dispatcher {
				t.Fatalf("returned dispatcher is incorrect")
			}

			err = s.Close()
			if err != nil {
				t.Errorf("Got error during Close: %v", err)
			}
		})
	}
}

func TestClient(t *testing.T) {
	s, err := newTestServer(globalCfg, serviceCfg, true)
	if err != nil {
		t.Fatalf("Unable to create server: %v", err)
	}

	s.Run()

	c, err := createClient(s.Addr())
	if err != nil {
		t.Errorf("Creating client failed: %v", err)
	}

	req := &mixerpb.ReportRequest{}
	_, err = c.Report(context.Background(), req)

	if err != nil {
		t.Errorf("Got error during Report: %v", err)
	}

	err = s.Close()
	if err != nil {
		t.Errorf("Got error during Close: %v", err)
	}

	err = s.Wait()
	if err == nil {
		t.Errorf("Got success, expecting failure")
	}
}

func TestErrors(t *testing.T) {
	a := DefaultArgs()
	a.APIWorkerPoolSize = -1
	a.LoggingOptions.LogGrpc = false // Avoid introducing a race to the server tests.
	configStore, cerr := storetest.SetupStoreForTest(globalCfg, serviceCfg)
	if cerr != nil {
		t.Fatal(cerr)
	}
	a.ConfigStore = configStore

	s, err := New(a)
	if s != nil || err == nil {
		t.Errorf("Got success, expecting error")
	}

	a = DefaultArgs()
	a.UseNewRuntime = false
	a.APIPort = 0
	a.MonitoringPort = 0
	a.TracingOptions.LogTraceSpans = true
	a.LoggingOptions.LogGrpc = false // Avoid introducing a race to the server tests.

	// This test is designed to exercise the many failure paths in the server creation
	// code. This is mostly about replacing methods in the patch table with methods that
	// return failures in order to make sure the failure recovery code is working right.
	// There are also some cases that tweak some parameters to tickle particular execution paths.
	// So for all these cases, we expect to get a failure when trying to create the server instance.

	for i := 0; i < 20; i++ {
		t.Run(strconv.Itoa(i), func(t *testing.T) {
			a.ConfigStore = configStore
			a.ConfigStoreURL = ""
			pt := newPatchTable()
			switch i {
			case 0:
				pt.newILEvaluator = func(cacheSize int) (*evaluator.IL, error) {
					return nil, errors.New("BAD")
				}
			case 1:
				// TODO: Runtime2 does not return error on the new path. Once we switch over, we can remove this case.
				pt.newRuntime = func(eval expr.Evaluator, typeChecker expr.TypeChecker, vocab mixerRuntime.VocabularyChangeListener, gp *pool.GoroutinePool,
					handlerPool *pool.GoroutinePool,
					identityAttribute string, defaultConfigNamespace string, s store.Store, adapterInfo map[string]*adapter.Info,
					templateInfo map[string]template.Info) (mixerRuntime.Dispatcher, error) {
					return nil, errors.New("BAD")
				}
			case 2:
				a.ConfigStore = nil
				pt.newStore = func(reg *store.Registry, configURL string) (store.Store, error) {
					return nil, errors.New("BAD")
				}
			case 3:
				pt.configTracing = func(_ string, _ *tracing.Options) (io.Closer, error) {
					return nil, errors.New("BAD")
				}
			case 4:
				pt.startMonitor = func(port uint16, enableProfiling bool, lf listenFunc) (*monitor, error) {
					return nil, errors.New("BAD")
				}
			case 5:
				a.MonitoringPort = 1234
				pt.listen = func(network string, address string) (net.Listener, error) {
					// fail any net.Listen call that's not for the monitoring port.
					if address != ":1234" {
						return nil, errors.New("BAD")
					}
					return net.Listen(network, address)
				}
			case 6:
				a.MonitoringPort = 1234
				pt.listen = func(network string, address string) (net.Listener, error) {
					// fail the net.Listen call that's for the monitoring port.
					if address == ":1234" {
						return nil, errors.New("BAD")
					}
					return net.Listen(network, address)
				}
			case 7:
				a.ConfigStoreURL = "http://abogusurl.com"
			case 8:
				pt.configLog = func(options *log.Options) error {
					return errors.New("BAD")
				}
			case 9:
				a.UseNewRuntime = true
				pt.runtimeListen = func(rt *mixerRuntime2.Runtime) error {
					return errors.New("BAD")
				}
			default:
				return
			}

			s, err = newServer(a, pt)
			if s != nil || err == nil {
				t.Errorf("Got success, expecting error")
			}
		})
	}
}

func TestMonitoringMux(t *testing.T) {
	configStore, _ := storetest.SetupStoreForTest(globalCfg, serviceCfg)

	a := DefaultArgs()
	a.ConfigStore = configStore
	a.MonitoringPort = 0
	a.APIPort = 0
	s, err := New(a)
	if err != nil {
		t.Fatalf("Got %v, expecting success", err)
	}

	r := &http.Request{}
	r.Method = "GET"
	r.URL, _ = url.Parse("http://localhost/version")
	rw := &responseWriter{}

	// this is exercising the mux handler code in monitoring.go. The supplied rw is used to return
	// an error which causes all code paths in the mux handler code to be visited.
	s.monitor.monitoringServer.Handler.ServeHTTP(rw, r)

	v := string(rw.payload)
	if v != version.Info.String() {
		t.Errorf("Got version %v, expecting %v", v, version.Info.String())
	}

	_ = s.Close()
}

type responseWriter struct {
	payload []byte
}

func (rw *responseWriter) Header() http.Header {
	return nil
}

func (rw *responseWriter) Write(b []byte) (int, error) {
	rw.payload = b
	return -1, errors.New("BAD")
}

func (rw *responseWriter) WriteHeader(int) {
}
