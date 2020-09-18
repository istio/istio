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
	"io/ioutil"
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
	"istio.io/istio/pkg/test/framework/components/echo/echoboot"
	"istio.io/istio/pkg/test/framework/components/gcemetadata"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/framework/components/stackdriver"
	edgespb "istio.io/istio/pkg/test/framework/components/stackdriver/edges"
	"istio.io/istio/pkg/test/framework/resource"
	"istio.io/istio/pkg/test/util/tmpl"
)

const (
	// testdata, including golden files
	stackdriverBootstrapOverride = "../testdata/custom_bootstrap.yaml.tmpl"
	serverRequestCount           = "testdata/server_request_count.json.tmpl"
	clientRequestCount           = "testdata/client_request_count.json.tmpl"
	serverLogEntry               = "testdata/server_access_log.json.tmpl"
	serverEdgeFile               = "testdata/server_edge.prototext.tmpl"
	traceTmplFile                = "testdata/trace.prototext.tmpl"
	sdBootstrapConfigMap         = "stackdriver-bootstrap-config"
)

var (
	istioInst istio.Instance
	ns        namespace.Instance
	gceInst   gcemetadata.Instance
	sdInst    stackdriver.Instance
	server    echo.Instance
	client    echo.Instance
	vmEnv     map[string]string
)

var (
	// golden values for tests
	wantServerReqs       monitoring.TimeSeries
	wantClientReqs       monitoring.TimeSeries
	wantLogEntry         loggingpb.LogEntry
	wantTrafficAssertion *edgespb.TrafficAssertion
	wantTrace            cloudtrace.Trace
)

var (
	clientBuilder, serverBuilder echo.Builder
)

var (
	proxyConfigAnnotation = echo.Annotation{
		Name: annotation.ProxyConfig.Name,
		Type: echo.WorkloadAnnotation,
	}

	envTagsProxyConfig = `|0
tracing:
  custom_tags:
    canonical_service_name:
      environment:
        name: CANONICAL_SERVICE
        defaultValue: "unknown"
    canonical_service_revision:
      environment:
        name: CANONICAL_REVISION
        defaultValue: "earliest"`

	envTagsJSON = `{\"tracing\":{\"custom_tags\":{\"hello\":{\"literal\":{\"value\":\"test\"},\"canonical_service_name\":{\"environment\":{\"name\":\"CANONICAL_SERVICE\",\"defaultValue\":\"unknown\"}},\"canonical_service_revision\":{\"environment\":{\"name\":\"CANONICAL_REVISION\",\"defaultValue\":\"earliest\"}}}}}`
)

var envoyFilterTpml = `
apiVersion: networking.istio.io/v1alpha3
kind: EnvoyFilter
metadata:
  name: stackdriver-tracing
  namespace: {{ .EchoNamespace }}
spec:
  workloadSelector:
    labels:
      service.istio.io/canonical-name: "vm-server"
  configPatches:
  - applyTo: NETWORK_FILTER
    match:
      context: ANY
      listener:
        filterChain:
          filter:
            name: "envoy.http_connection_manager"
    patch:
      operation: MERGE
      value:
        typed_config:
          "@type": "type.googleapis.com/envoy.extensions.filters.network.http_connection_manager.v3.HttpConnectionManager"
          tracing:
            random_sampling:
              value: 100.0
            custom_tags:
            - tag: "canonical_service_name"
              environment:
                name: "CANONICAL_SERVICE"
                default_value: "unknown"
            - tag: "canonical_service_name"
              environment:
                name: "CANONICAL_REVISION"
                default_value: "earliest"
`

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
	framework.
		NewSuite(m).
		RequireSingleCluster().
		Setup(istio.Setup(&istioInst, func(_ resource.Context, cfg *istio.Config) {
			cfg.Values["meshConfig.enableTracing"] = "true"
			// 			cfg.ControlPlaneValues = `
			// meshConfig:
			//   enableTracing: true
			//   defaultConfig:
			//     tracing:
			//       sampling: "100.0"
			//       customTags:
			//         hello:
			//           literal:
			//             value: "test"
			//         canonical_service_name:
			//           environment:
			//             name: "CANONICAL_SERVICE"
			//             defaultValue: "unknown"
			//         canonical_service_revision:
			//           environment:
			//             name: "CANONICAL_REVISION"
			//             defaultValue: "latest"`
			cfg.Values["meshConfig.defaultConfig.tracing.sampling"] = "100.0"
			// cfg.Values["meshConfig.defaultConfig.tracing.custom_tags.canonical_service_name.environment.name"] = "CANONICAL_SERVICE"
			// cfg.Values["meshConfig.defaultConfig.tracing.custom_tags.canonical_service_name.environment.defaultValue"] = "unknown"
			// cfg.Values["meshConfig.defaultConfig.tracing.custom_tags.canonical_service_revision.environment.name"] = "CANONICAL_REVISION"
			// cfg.Values["meshConfig.defaultConfig.tracing.custom_tags.canonical_service_revision.environment.defaultValue"] = "latest"
			cfg.Values["global.proxy.tracer"] = "stackdriver"
			// cfg.Values["pilot.traceSampling"] = "100.0"
			cfg.Values["telemetry.enabled"] = "true"
			cfg.Values["telemetry.v2.enabled"] = "true"
			cfg.Values["telemetry.v2.stackdriver.enabled"] = "true"
			cfg.Values["telemetry.v2.stackdriver.logging"] = "true"
			cfg.Values["telemetry.v2.stackdriver.configOverride.meshEdgesReportingDuration"] = "5s"
			cfg.Values["telemetry.v2.stackdriver.configOverride.enable_mesh_edges_reporting"] = "true"
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

	if gceInst, err = gcemetadata.New(ctx, gcemetadata.Config{}); err != nil {
		return err
	}

	if sdInst, err = stackdriver.New(ctx, stackdriver.Config{}); err != nil {
		return err
	}

	templateBytes, err := ioutil.ReadFile(stackdriverBootstrapOverride)
	if err != nil {
		return err
	}
	sdBootstrap, err := tmpl.Evaluate(string(templateBytes), map[string]interface{}{
		"StackdriverNamespace": sdInst.GetStackdriverNamespace(),
		"StackdriverAddress":   sdInst.Address(),
		"EchoNamespace":        ns.Name(),
	})
	if err != nil {
		return err
	}

	if err = ctx.Config().ApplyYAML(ns.Name(), sdBootstrap); err != nil {
		return err
	}

	sdFilter, err := tmpl.Evaluate(envoyFilterTpml, map[string]interface{}{
		"StackdriverAddress": sdInst.Address(),
		"EchoNamespace":      ns.Name(),
	})
	if err != nil {
		return err
	}

	if err = ctx.Config().ApplyYAML(ns.Name(), sdFilter); err != nil {
		return err
	}

	vmLabelsJSON := "{\\\"service.istio.io/canonical-name\\\":\\\"vm-server\\\",\\\"service.istio.io/canonical-revision\\\":\\\"v1\\\"}"

	vmEnv = map[string]string{
		"ISTIO_META_INSECURE_STACKDRIVER_ENDPOINT":               sdInst.Address(),
		"ISTIO_META_STACKDRIVER_MONITORING_EXPORT_INTERVAL_SECS": "10",
		"ISTIO_META_MESH_ID":                                     "proj-test-mesh",
		"ISTIO_META_WORKLOAD_NAME":                               "vm-server-v1",
		"ISTIO_METAJSON_LABELS":                                  vmLabelsJSON,
		"GCE_METADATA_HOST":                                      gceInst.Address(),
		"CANONICAL_SERVICE":                                      "vm-server",
		"CANONICAL_REVISION":                                     "v1",
		"ISTIO_BOOTSTRAP_OVERRIDE":                               "/etc/istio/custom-bootstrap/custom_bootstrap.json",
	}

	// read expected values from testdata
	wantClientReqs, wantServerReqs, err = goldenRequestCounts()
	if err != nil {
		return fmt.Errorf("failed to get golden metrics from file: %v", err)
	}
	wantLogEntry, err = goldenLogEntry()
	if err != nil {
		return fmt.Errorf("failed to get golden log entry from file: %v", err)
	}
	wantTrafficAssertion, err = goldenTrafficAssertion()
	if err != nil {
		return fmt.Errorf("failed to get golden traffic assertion from file: %v", err)
	}
	wantTrace, err = goldenTrace()
	if err != nil {
		return fmt.Errorf("failed to get golden trace from file: %v", err)
	}

	// set up client and server
	ports := []echo.Port{
		{
			Name:     "http",
			Protocol: protocol.HTTP,
			// Due to a bug in WorkloadEntry, service port must equal target port for now
			InstancePort: 8090,
			ServicePort:  8090,
		},
	}

	// builder to build the instances iteratively
	clientBuilder = echoboot.NewBuilder(ctx).
		With(&client, echo.Config{
			Service:   "client",
			Namespace: ns,
			Ports:     ports,
			Subsets: []echo.SubsetConfig{
				{
					Annotations: map[echo.Annotation]*echo.AnnotationValue{
						echo.SidecarBootstrapOverride: {
							Value: sdBootstrapConfigMap,
						},
					},
				},
			},
		})

	serverBuilder = echoboot.NewBuilder(ctx).
		With(&server, echo.Config{
			Service:       "server",
			Namespace:     ns,
			Ports:         ports,
			DeployAsVM:    true,
			VMEnvironment: vmEnv,
			// Subsets: []echo.SubsetConfig{
			// 	{
			// 		Annotations: echo.NewAnnotations().Set(proxyConfigAnnotation, envTagsProxyConfig),
			// 	},
			// },
		})

	return nil
}

func goldenRequestCounts() (cltRequestCount, srvRequestCount monitoring.TimeSeries, err error) {
	srvRequestCountTmpl, err := ioutil.ReadFile(serverRequestCount)
	if err != nil {
		return
	}
	sr, err := tmpl.Evaluate(string(srvRequestCountTmpl), map[string]interface{}{
		"EchoNamespace": ns.Name(),
	})
	if err != nil {
		return
	}
	if err = jsonpb.UnmarshalString(sr, &srvRequestCount); err != nil {
		return
	}
	cltRequestCountTmpl, err := ioutil.ReadFile(clientRequestCount)
	if err != nil {
		return
	}
	cr, err := tmpl.Evaluate(string(cltRequestCountTmpl), map[string]interface{}{
		"EchoNamespace": ns.Name(),
	})
	if err != nil {
		return
	}
	err = jsonpb.UnmarshalString(cr, &cltRequestCount)
	return
}

func goldenLogEntry() (srvLogEntry loggingpb.LogEntry, err error) {
	srvlogEntryTmpl, err := ioutil.ReadFile(serverLogEntry)
	if err != nil {
		return
	}
	sr, err := tmpl.Evaluate(string(srvlogEntryTmpl), map[string]interface{}{
		"EchoNamespace": ns.Name(),
	})
	if err != nil {
		return
	}
	if err = jsonpb.UnmarshalString(sr, &srvLogEntry); err != nil {
		return
	}
	return
}

func goldenTrafficAssertion() (*edgespb.TrafficAssertion, error) {
	taTmpl, err := ioutil.ReadFile(serverEdgeFile)
	if err != nil {
		return nil, err
	}

	taString, err := tmpl.Evaluate(string(taTmpl), map[string]interface{}{
		"EchoNamespace": ns.Name(),
	})
	if err != nil {
		return nil, err
	}

	var ta edgespb.TrafficAssertion
	err = proto.UnmarshalText(taString, &ta)
	if err != nil {
		return nil, err
	}
	return &ta, nil
}

func goldenTrace() (cloudtrace.Trace, error) {
	traceTmpl, err := ioutil.ReadFile(traceTmplFile)
	if err != nil {
		return cloudtrace.Trace{}, err
	}
	traceStr, err := tmpl.Evaluate(string(traceTmpl), map[string]interface{}{
		"EchoNamespace": ns.Name(),
	})
	if err != nil {
		return cloudtrace.Trace{}, err
	}
	var trace cloudtrace.Trace
	err = proto.UnmarshalText(traceStr, &trace)
	if err != nil {
		return cloudtrace.Trace{}, err
	}
	return trace, nil
}
