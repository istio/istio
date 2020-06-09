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

	"go.opencensus.io/stats/view"
	"google.golang.org/grpc"

	mixerpb "istio.io/api/mixer/v1"
	"istio.io/istio/mixer/pkg/config/storetest"
	"istio.io/istio/mixer/pkg/runtime"
	generatedTmplRepo "istio.io/istio/mixer/template"
	"istio.io/istio/pkg/tracing"
	"istio.io/pkg/log"
	"istio.io/pkg/version"
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

// defaultTestArgs returns result of DefaultArgs(), except with a modification to the LoggingOptions
// to avoid a data race between gRpc and the logging code.
func defaultTestArgs() *Args {
	a := DefaultArgs()
	a.LoggingOptions.LogGrpc = false // Avoid introducing a race to the server tests.
	return a
}

// createClient returns a Mixer gRPC client, useful for tests
// nolint: interfacer
func createClient(addr net.Addr) (mixerpb.MixerClient, error) {
	conn, err := grpc.Dial(addr.String(), grpc.WithInsecure())
	if err != nil {
		return nil, err
	}

	return mixerpb.NewMixerClient(conn), nil
}

func newTestServer(globalCfg, serviceCfg string) (*Server, error) {
	a := defaultTestArgs()
	a.APIPort = 0
	a.MonitoringPort = 0
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
	s, err := newTestServer(globalCfg, serviceCfg)
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
}

func TestClient(t *testing.T) {
	s, err := newTestServer(globalCfg, serviceCfg)
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
	a := defaultTestArgs()
	a.APIWorkerPoolSize = -1
	s, err := New(a)
	if s != nil || err == nil {
		t.Errorf("Got success, expecting error")
	}

	testcases := []struct {
		name    string
		setupFn func(a *Args, pt *patchTable)
	}{
		{"empty config store URL",
			func(a *Args, pt *patchTable) { a.ConfigStore = nil; a.ConfigStoreURL = "" }},
		{"bad config store URL",
			func(a *Args, pt *patchTable) { a.ConfigStore = nil; a.ConfigStoreURL = "DEADBEEF" }},
		{"non-existent store URL",
			func(a *Args, pt *patchTable) { a.ConfigStoreURL = "http://abogusurl.com" }},
		{"failed tracing setup",
			func(a *Args, pt *patchTable) {
				pt.configTracing = func(_ string, _ *tracing.Options) (io.Closer, error) { return nil, errors.New("BAD") }
			},
		},
		{"failed monitoring setup",
			func(a *Args, pt *patchTable) {
				pt.startMonitor = func(port uint16, enableProfiling bool, lf listenFunc) (*monitor, error) {
					return nil, errors.New("BAD")
				}
			},
		},
		{"duplicate port registration",
			func(a *Args, pt *patchTable) {
				a.MonitoringPort = 1234
				pt.listen = func(network string, address string) (net.Listener, error) {
					// fail any net.Listen call that's not for the monitoring port.
					if address != ":1234" {
						return nil, errors.New("BAD")
					}
					return net.Listen(network, address)
				}
			},
		},
		{"listener failure",
			func(a *Args, pt *patchTable) {
				a.MonitoringPort = 1234
				pt.listen = func(network string, address string) (net.Listener, error) {
					// fail any net.Listen call that's not for the monitoring port.
					if address == ":1234" {
						return nil, errors.New("BAD")
					}
					return net.Listen(network, address)
				}
			},
		},
		{"failed logging setup",
			func(a *Args, pt *patchTable) {
				pt.configLog = func(options *log.Options) error {
					return errors.New("BAD")
				}
			},
		},
		{"failed runtime setup",
			func(a *Args, pt *patchTable) {
				pt.runtimeListen = func(rt *runtime.Runtime) error {
					return errors.New("BAD")
				}
			},
		},
		{"failed OpenCensus Exporter setup",
			func(a *Args, pt *patchTable) {
				pt.newOpenCensusExporter = func() (view.Exporter, error) {
					return nil, errors.New("BAD")
				}
			},
		},
		{"failed OpenCensus view registration",
			func(a *Args, pt *patchTable) {
				pt.registerOpenCensusViews = func(...*view.View) error {
					// panic(nil)
					return errors.New("BAD")
				}
			},
		},
		{"unix socket removal",
			func(a *Args, pt *patchTable) {
				a.APIAddress = "unix:///dev"
			},
		},
		{"Unix API address failed",
			func(a *Args, pt *patchTable) {
				a.APIAddress = "unix://a/b/c"
				pt.remove = func(name string) error { return errors.New("BAD") }
			},
		},
	}

	// This test is designed to exercise the many failure paths in the server creation
	// code. This is mostly about replacing methods in the patch table with methods that
	// return failures in order to make sure the failure recovery code is working right.
	// There are also some cases that tweak some parameters to tickle particular execution paths.
	// So for all these cases, we expect to get a failure when trying to create the server instance.

	for _, v := range testcases {
		t.Run(v.name, func(t *testing.T) {

			// setup
			configStore, cerr := storetest.SetupStoreForTest(globalCfg, serviceCfg)
			if cerr != nil {
				t.Fatal(cerr)
			}
			a = defaultTestArgs()
			a.APIPort = 0
			a.MonitoringPort = 0
			a.TracingOptions.LogTraceSpans = true
			a.ConfigStore = configStore
			a.ConfigStoreURL = ""
			pt := newPatchTable()

			// test
			v.setupFn(a, pt)
			s, err = newServer(a, pt)
			if s != nil || err == nil {
				s.Close()
				t.Errorf("Got success, expecting error")
			}
		})
	}
}

func TestExtractNetAddress(t *testing.T) {
	cases := []struct {
		apiPort       uint16
		apiAddress    string
		resultNetwork string
		resultAddress string
	}{
		{0, "tcp://127.0.0.1:0", "tcp", "127.0.0.1:0"},
		{0, ":0", "tcp", ":0"},
		{0, "unix://a/b/c", "unix", "a/b/c"},
		{132, "", "tcp", ":132"},
	}

	for i, c := range cases {
		t.Run(strconv.Itoa(i), func(t *testing.T) {
			n, a := extractNetAddress(c.apiPort, c.apiAddress)

			if n != c.resultNetwork || a != c.resultAddress {
				t.Errorf("Expecting %s:%s, got %s:%s", c.resultNetwork, c.resultAddress, n, a)
			}
		})
	}
}

func TestMonitoringMux(t *testing.T) {
	configStore, _ := storetest.SetupStoreForTest(globalCfg, serviceCfg)

	a := defaultTestArgs()
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
