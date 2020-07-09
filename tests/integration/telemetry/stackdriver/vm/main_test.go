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

package vm

import (
	"io/ioutil"
	"testing"

	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/gcemetadata"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/framework/components/stackdriver"
	"istio.io/istio/pkg/test/util/tmpl"

	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/components/pilot"
	"istio.io/istio/pkg/test/framework/resource"
)

const (
	stackdriverBootstrapOverride = "../testdata/custom_bootstrap.yaml.tmpl"
	serverRequestCount           = "testdata/server_request_count.json.tmpl"
	clientRequestCount           = "testdata/client_request_count.json.tmpl"
	serverLogEntry               = "testdata/server_access_log.json.tmpl"
	sdBootstrapConfigMap         = "stackdriver-bootstrap-config"
)

var (
	i       istio.Instance
	p       pilot.Instance
	ns      namespace.Instance
	gceInst gcemetadata.Instance
	sdInst  stackdriver.Instance
	srv     echo.Instance
	clt     echo.Instance
	vmEnv   map[string]string
)

// Testing telemetry with VM mesh expansion on a simulated GCE instance.
// Rather than deal with the infra to get a real VM, we will use a pod
// with no Service, no DNS, no service account, etc to simulate a VM.
//
// This test setup borrows heavily from the following packages:
// - tests/integration/pilot/vm
// - tests/integration/telemetry/stackdriver
func TestMain(m *testing.M) {
	framework.
		NewSuite(m).
		RequireSingleCluster().
		Setup(istio.Setup(&i, func(cfg *istio.Config) {
			cfg.ControlPlaneValues = `
values:
  global:
    meshExpansion:
      enabled: true`
			cfg.Values["telemetry.enabled"] = "true"
			cfg.Values["telemetry.v1.enabled"] = "false"
			cfg.Values["telemetry.v2.enabled"] = "true"
			cfg.Values["telemetry.v2.stackdriver.enabled"] = "true"
			cfg.Values["telemetry.v2.stackdriver.logging"] = "true"
		})).
		Setup(func(ctx resource.Context) (err error) {
			if p, err = pilot.New(ctx, pilot.Config{}); err != nil {
				return err
			}
			return nil
		}).
		Setup(testSetup).
		Run()
}

func testSetup(ctx resource.Context) (err error) {
	ns, err = namespace.New(ctx, namespace.Config{
		Prefix: "istio-echo",
		Inject: true,
	})
	if err != nil {
		return
	}

	gceInst, err = gcemetadata.New(ctx, gcemetadata.Config{})
	if err != nil {
		return
	}

	sdInst, err = stackdriver.New(ctx, stackdriver.Config{})
	if err != nil {
		return
	}

	templateBytes, err := ioutil.ReadFile(stackdriverBootstrapOverride)
	if err != nil {
		return
	}
	sdBootstrap, err := tmpl.Evaluate(string(templateBytes), map[string]interface{}{
		"StackdriverNamespace": sdInst.GetStackdriverNamespace(),
		"EchoNamespace":        ns.Name(),
	})
	if err != nil {
		return
	}

	err = ctx.Config().ApplyYAML(ns.Name(), sdBootstrap)

	vmLabelsJSON := "{\\\"service.istio.io/canonical-name\\\":\\\"vm-server\\\",\\\"service.istio.io/canonical-revision\\\":\\\"v1\\\"}"

	vmEnv = map[string]string{
		"ISTIO_META_INSECURE_STACKDRIVER_ENDPOINT":               sdInst.Address(),
		"ISTIO_META_STACKDRIVER_MONITORING_EXPORT_INTERVAL_SECS": "10",
		"ISTIO_META_MESH_ID":                                     "test-mesh",
		"ISTIO_META_WORKLOAD_NAME":                               "vm-server-v1",
		"ISTIO_METAJSON_LABELS":                                  vmLabelsJSON,
		"GCE_METADATA_HOST":                                      gceInst.Address(),
	}

	return
}
