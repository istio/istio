//go:build integ
// +build integ

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
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/gogo/protobuf/jsonpb"
	"github.com/gogo/protobuf/proto"
	"google.golang.org/genproto/googleapis/devtools/cloudtrace/v1"
	loggingpb "google.golang.org/genproto/googleapis/logging/v2"
	monitoring "google.golang.org/genproto/googleapis/monitoring/v3"

	"istio.io/api/annotation"
	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/deployment"
	"istio.io/istio/pkg/test/framework/components/gcemetadata"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/framework/components/stackdriver"
	"istio.io/istio/pkg/test/framework/label"
	"istio.io/istio/pkg/test/framework/resource"
	"istio.io/istio/pkg/test/util/tmpl"
	"istio.io/istio/tests/integration/telemetry"
	sdtest "istio.io/istio/tests/integration/telemetry/stackdriver"
)

const (
	// testdata, including golden files
	stackdriverBootstrapOverride = "../testdata/custom_bootstrap.yaml.tmpl"
	serverRequestCount           = "testdata/server_request_count.json.tmpl"
	clientRequestCount           = "testdata/client_request_count.json.tmpl"
	serverLogEntry               = "testdata/server_access_log.json.tmpl"
	traceTmplFile                = "testdata/trace.prototext.tmpl"
	sdBootstrapConfigMap         = "stackdriver-bootstrap-config"
)

var (
	istioInst istio.Instance
	ns        namespace.Instance
	sdInst    stackdriver.Instance
	server    echo.Instance
	client    echo.Instance
	vmEnv     map[string]string
)

var (
	// golden values for tests
	wantServerReqs *monitoring.TimeSeries
	wantClientReqs *monitoring.TimeSeries
	wantLogEntry   *loggingpb.LogEntry
	wantTrace      *cloudtrace.Trace
)

var clientBuilder, serverBuilder deployment.Builder

var (
	proxyConfigAnnotation = echo.Annotation{
		Name: annotation.ProxyConfig.Name,
		Type: echo.WorkloadAnnotation,
	}

	envTagsProxyConfig = `
tracing:
  stackdriver:
    debug: true
  sampling: 100.0`
)

const enforceMTLS = `
apiVersion: security.istio.io/v1beta1
kind: PeerAuthentication
metadata:
  name: default
spec:
  mtls:
    mode: STRICT
---
apiVersion: networking.istio.io/v1alpha3
kind: DestinationRule
metadata:
  name: send-mtls
spec:
  host: "*.svc.cluster.local"
  trafficPolicy:
    tls:
      mode: ISTIO_MUTUAL
`

// Testing telemetry with VM mesh expansion on a simulated GCE instance.
// Rather than deal with the infra to get a real VM, we will use a pod
// with no Service, no DNS, no service account, etc to simulate a VM.
//
// This test setup borrows heavily from the following packages:
// - tests/integration/pilot/vm
// - tests/integration/telemetry/stackdriver
func TestMain(m *testing.M) {
	// nolint: staticcheck
	framework.
		NewSuite(m).
		// https://github.com/istio/istio/issues/35923. Since IPv6 has no external connectivity, we are "not on GCP"
		// in the sense that we cannot access the metadata server
		Label(label.IPv4).
		SkipIf("test requires VMs", func(ctx resource.Context) bool {
			return ctx.Settings().Skip(echo.VM)
		}).
		RequireMultiPrimary().
		Setup(func(ctx resource.Context) error {
			var err error
			// Unlike other tests, we use GCE metadata server unconditionally for VMs because they would not have
			// stable labels otherwise, unlike pods.
			if sdtest.GCEInst, err = gcemetadata.New(ctx, gcemetadata.Config{}); err != nil {
				return err
			}
			return nil
		}).
		Setup(istio.Setup(&istioInst, func(_ resource.Context, cfg *istio.Config) {
			cfg.ControlPlaneValues = `
meshConfig:
`
			cfg.Values["meshConfig.enableTracing"] = "true"
			cfg.Values["meshConfig.defaultConfig.tracing.sampling"] = "100.0"
			cfg.Values["global.meshID"] = "proj-test-mesh"
			cfg.Values["global.proxy.tracer"] = "stackdriver"
			cfg.Values["telemetry.v2.enabled"] = "true"
			cfg.Values["telemetry.v2.stackdriver.enabled"] = "true"
			cfg.Values["telemetry.v2.stackdriver.logging"] = "true"

			// conditionally use a fake metadata server for testing off of GCP
			if sdtest.GCEInst != nil {
				cfg.ControlPlaneValues = strings.Join([]string{cfg.ControlPlaneValues, sdtest.FakeGCEMetadataServerValues, sdtest.GCEInst.Address()}, "")
			}
		})).
		Setup(testSetup).
		Run()
}

func testSetup(ctx resource.Context) error {
	var err error

	if ns, err = namespace.New(ctx, namespace.Config{
		Prefix: "istio-echo",
		Inject: true,
	}); err != nil {
		return err
	}
	sdtest.EchoNsInst = ns

	if sdInst, err = stackdriver.New(ctx, stackdriver.Config{}); err != nil {
		return err
	}
	sdtest.SDInst = sdInst

	if err = ctx.ConfigKube().EvalFile(map[string]interface{}{
		"StackdriverAddress": sdInst.Address(),
		"EchoNamespace":      ns.Name(),
	}, stackdriverBootstrapOverride).Apply(ns.Name()); err != nil {
		return err
	}

	vmLabelsJSON := "{\\\"service.istio.io/canonical-name\\\":\\\"vm-server\\\",\\\"service.istio.io/canonical-revision\\\":\\\"v1\\\"}"

	vmEnv = map[string]string{
		"ISTIO_META_INSECURE_STACKDRIVER_ENDPOINT":               sdInst.Address(),
		"ISTIO_META_STACKDRIVER_MONITORING_EXPORT_INTERVAL_SECS": "10",
		"ISTIO_META_MESH_ID":                                     "proj-test-mesh",
		"ISTIO_META_WORKLOAD_NAME":                               "vm-server-v1",
		"ISTIO_METAJSON_LABELS":                                  vmLabelsJSON,
		"CANONICAL_SERVICE":                                      "vm-server",
		"CANONICAL_REVISION":                                     "v1",
		// we must supply a bootstrap override to get the test endpoint uri into the tracing configuration
		"ISTIO_BOOTSTRAP_OVERRIDE": "/etc/istio/custom-bootstrap/custom_bootstrap.json",
	}
	if sdtest.GCEInst != nil {
		vmEnv["GCE_METADATA_HOST"] = sdtest.GCEInst.Address()
	}

	trustDomain := telemetry.GetTrustDomain(ctx.Clusters()[0], istioInst.Settings().SystemNamespace)
	// read expected values from testdata
	wantClientReqs, wantServerReqs, err = goldenRequestCounts(trustDomain)
	if err != nil {
		return fmt.Errorf("failed to get golden metrics from file: %v", err)
	}
	wantLogEntry, err = goldenLogEntry(trustDomain)
	if err != nil {
		return fmt.Errorf("failed to get golden log entry from file: %v", err)
	}
	wantTrace, err = goldenTrace(trustDomain)
	if err != nil {
		return fmt.Errorf("failed to get golden trace from file: %v", err)
	}

	// set up client and server
	ports := []echo.Port{
		{
			Name:     "http",
			Protocol: protocol.HTTP,
			// Due to a bug in WorkloadEntry, service port must equal target port for now
			WorkloadPort: 8090,
			ServicePort:  8090,
		},
	}

	// builder to build the instances iteratively
	clientBuilder = deployment.New(ctx).
		With(&client, echo.Config{
			Service:   "client",
			Namespace: ns,
			Ports:     ports,
			Subsets: []echo.SubsetConfig{
				{
					Annotations: echo.NewAnnotations().Set(echo.SidecarBootstrapOverride, sdBootstrapConfigMap),
				},
			},
		})

	serverBuilder = deployment.New(ctx).
		With(&server, echo.Config{
			Service:       "server",
			Namespace:     ns,
			Ports:         ports,
			DeployAsVM:    true,
			VMEnvironment: vmEnv,
			Subsets: []echo.SubsetConfig{
				{
					Annotations: echo.NewAnnotations().
						Set(proxyConfigAnnotation, envTagsProxyConfig).
						Set(echo.SidecarBootstrapOverride, sdBootstrapConfigMap),
				},
			},
		})

	return nil
}

func goldenRequestCounts(trustDomain string) (cltRequestCount, srvRequestCount *monitoring.TimeSeries, err error) {
	srvRequestCountTmpl, err := os.ReadFile(serverRequestCount)
	if err != nil {
		return
	}
	sr, err := tmpl.Evaluate(string(srvRequestCountTmpl), map[string]interface{}{
		"EchoNamespace": ns.Name(),
		"TrustDomain":   trustDomain,
	})
	if err != nil {
		return
	}
	cltRequestCount = &monitoring.TimeSeries{}
	srvRequestCount = &monitoring.TimeSeries{}
	if err = jsonpb.UnmarshalString(sr, srvRequestCount); err != nil {
		return
	}
	cltRequestCountTmpl, err := os.ReadFile(clientRequestCount)
	if err != nil {
		return
	}
	cr, err := tmpl.Evaluate(string(cltRequestCountTmpl), map[string]interface{}{
		"EchoNamespace": ns.Name(),
		"TrustDomain":   trustDomain,
	})
	if err != nil {
		return
	}
	err = jsonpb.UnmarshalString(cr, cltRequestCount)
	return
}

func goldenLogEntry(trustDomain string) (srvLogEntry *loggingpb.LogEntry, err error) {
	srvlogEntryTmpl, err := os.ReadFile(serverLogEntry)
	if err != nil {
		return
	}
	sr, err := tmpl.Evaluate(string(srvlogEntryTmpl), map[string]interface{}{
		"EchoNamespace": ns.Name(),
		"TrustDomain":   trustDomain,
	})
	if err != nil {
		return
	}
	srvLogEntry = &loggingpb.LogEntry{}
	if err = jsonpb.UnmarshalString(sr, srvLogEntry); err != nil {
		return
	}
	return
}

func goldenTrace(trustDomain string) (*cloudtrace.Trace, error) {
	traceTmpl, err := os.ReadFile(traceTmplFile)
	if err != nil {
		return nil, err
	}
	traceStr, err := tmpl.Evaluate(string(traceTmpl), map[string]interface{}{
		"EchoNamespace": ns.Name(),
		"TrustDomain":   trustDomain,
	})
	if err != nil {
		return nil, err
	}
	var trace cloudtrace.Trace
	if err = proto.UnmarshalText(traceStr, &trace); err != nil {
		return nil, err
	}

	return &trace, nil
}
