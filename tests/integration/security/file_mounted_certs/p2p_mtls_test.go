// +build integ
//  Copyright Istio Authors
//
//  Licensed under the Apache License, Version 2.0 (the "License");
//  you may not use this file except in compliance with the License.
//  You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
//  Unless required by applicable law or agreed to in writing, software
//  distributed under the License is distributed on an "AS IS" BASIS,
//  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//  See the License for the specific language governing permissions and
//  limitations under the License.

package filemountedcerts

import (
	"bytes"
	"fmt"
	"html/template"
	"io/ioutil"
	"path"
	"reflect"
	"strings"
	"testing"
	"time"

	dto "github.com/prometheus/client_model/go"
	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/test/echo/common/response"
	"istio.io/istio/pkg/test/echo/common/scheme"
	"istio.io/istio/pkg/test/env"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/echoboot"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/framework/resource"
	"istio.io/istio/pkg/test/util/retry"
	"istio.io/istio/pkg/test/util/structpath"
)



var (
	vmEnv     map[string]string
)



func mustReadCert(t *testing.T, f string) string {
	b, err := ioutil.ReadFile(path.Join(env.IstioSrc, "tests/testdata/certs/dns", f))
	if err != nil {
		t.Fatalf("failed to read %v: %v", f, err)
	}
	return string(b)
}

func TestClientToServiceTls(t *testing.T) {
	framework.NewTest(t).
		Features("security.peer.file-mounted-certs").
		Run(func(ctx framework.TestContext) {

			echoClient, echoServer, serviceNamespace := setupEcho(t, ctx)

			bufDestinationRule := createObject(t, ctx, serviceNamespace.Name(), DestinationRuleConfigMutual)
			defer ctx.Config().DeleteYAMLOrFail(ctx, serviceNamespace.Name(), bufDestinationRule.String())

			bufVirtualService := createObject(t, ctx,serviceNamespace.Name(), VirtualServiceConfigMutual)
			defer ctx.Config().DeleteYAMLOrFail(ctx, serviceNamespace.Name(), bufVirtualService.String())

			bufPeerAuthentication := createObject(t, ctx, "istio-system", PeerAuthenticationConfig)
			defer ctx.Config().DeleteYAMLOrFail(ctx, "istio-system", bufPeerAuthentication.String())

			bufEnvoyFilter := createObject(t, ctx, serviceNamespace.Name(), EnvoyFilterConfig)
			defer ctx.Config().DeleteYAMLOrFail(ctx, serviceNamespace.Name(), bufEnvoyFilter.String())

			retry.UntilSuccessOrFail(t, func() error {
				resp, err := echoClient.Call(echo.CallOptions{
					Target:   echoServer,
					PortName: "http",
					Scheme:   scheme.HTTP,
				})

				if err != nil {
					return fmt.Errorf("request failed: %v", err)
				}

				codes := make([]string, 0, len(resp))
				for _, r := range resp {
					codes = append(codes, r.Code)
				}
				if !reflect.DeepEqual(codes, []string{response.StatusCodeOK}) {
					return fmt.Errorf("got codes %q, expected %q", codes, []string{response.StatusCodeOK})
				}
				for _, r := range resp {
					if _, f := r.RawResponse["X-Forwarded-Client-Cert"]; !f {
						return fmt.Errorf("expected to be see XFCC header, but none found. response: %+v", r.RawResponse)
					}
				}
				return nil
			}, retry.Delay(5*time.Second), retry.Timeout(1*time.Minute))

			verifyUsingFileBasedCerts(t, echoClient, echoServer)
			verifyStats(t, echoClient, echoServer)
	})
}

const (
	DestinationRuleConfigMutual = `
apiVersion: networking.istio.io/v1alpha3
kind: DestinationRule
metadata:
  name: server
  namespace: {{.AppNamespace}}
spec:
  host: "server.{{.AppNamespace}}.svc.cluster.local"
  trafficPolicy:
    tls:
      mode: MUTUAL
      caCertificates: /client-certs/ca.pem
      clientCertificate: /client-certs/cert.pem
      privateKey: /client-certs/key.pem
      subjectAltNames:
        - server.default.svc

`
	VirtualServiceConfigMutual = `
apiVersion: networking.istio.io/v1beta1
kind: VirtualService
metadata:
  name: server
  namespace: {{.AppNamespace}}
spec:
  hosts:
  - server.{{.AppNamespace}}.svc.cluster.local
  http:
  - retries:
      attempts: 5
      retryOn: cancelled,connect-failure,gateway-error,refused-stream,resource-exhausted,unavailable,retriable-status-codes
    route:
    - destination:
        host: server.{{.AppNamespace}}.svc.cluster.local
    timeout: 15s
`


	PeerAuthenticationConfig = `
apiVersion: security.istio.io/v1beta1
kind: PeerAuthentication
metadata:
  name: default
  namespace: "istio-system"
spec:
  mtls:
    mode: STRICT
`

	EnvoyFilterConfig = `
apiVersion: networking.istio.io/v1alpha3
kind: EnvoyFilter
metadata:
  name: test
  namespace: {{.AppNamespace}}
spec:
  workloadSelector:
    labels:
      app: server
  configPatches:
  - applyTo: HTTP_FILTER
    match:
      context: SIDECAR_INBOUND
      listener:
        filterChain:
          filter:
            name: "envoy.filters.network.http_connection_manager"
            subFilter:
              name: "istio_authn"
    patch:
      operation: REMOVE
`
)


func createObject(t *testing.T, ctx framework.TestContext, serviceNamespace string, yamlManifest string) bytes.Buffer {
	var destinationRuleToParse string
	destinationRuleToParse = DestinationRuleConfigMutual

	tmpl, err := template.New("DestinationRule").Parse(destinationRuleToParse)
	if err != nil {
		t.Errorf("failed to create template: %v", err)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, map[string]string{"AppNamespace": serviceNamespace}); err != nil {
		t.Fatalf("failed to create template: %v", err)
	}

	ctx.Config().ApplyYAMLOrFail(ctx, serviceNamespace, buf.String())

	return buf
}

// setupEcho creates an `istio-fd-sds` namespace and brings up two echo instances server and
// client in that namespace.
func setupEcho(t *testing.T, ctx resource.Context) (echo.Instance, echo.Instance, namespace.Instance) {

	appsNamespace := namespace.NewOrFail(t, ctx, namespace.Config{
		Prefix: "istio-fd-sds",
		Inject: true,
	})


	// test-server-cred's certificate has "server.default.svc" in SANs; Same is expected in DestinationRule.subjectAltNames for the test Echo server
	// This cert is going to be used as a "server certificate" on Echo Server's side
	CreateCustomSecret(ctx, "test-server-cred", appsNamespace, "tests/testdata/certs/dns")

	// test-istiod-client-cred will be used for connections to the control plane.
	CreateCustomSecret(ctx, "test-istiod-client-cred", appsNamespace, "tests/testdata/certs/pilot")

	// test-client-cred will be used for client connections from EchoClient to EchoServer
	CreateCustomSecret(ctx, "test-client-cred", appsNamespace, "tests/testdata/certs/pilot")

	var internalClient, internalServer echo.Instance

	echoboot.NewBuilder(ctx).
		With(&internalClient, echo.Config{
			Service:   "client",
			Namespace: appsNamespace,
			Ports:     []echo.Port{},
			Subsets: []echo.SubsetConfig{{
				Version: "v1",
				// Set up custom annotations to mount the certs.
				Annotations: echo.NewAnnotations().
					// workload-certs are needed in order to load the "default" SDS resource, which will be used for the xds-grpc mTLS (tls_certificate_sds_secret_configs.name == "default")
					// the default bootstrap template does not support reusing values from the `ISTIO_META_TLS_CLIENT_*` environment variables
					// see security/pkg/nodeagent/cache/secretcache.go:generateFileSecret() for details
					Set(echo.SidecarVolume, `{"server-certs":{"secret":{"secretName":"test-server-cred"}},"client-certs":{"secret":{"secretName":"test-client-cred"}},"workload-certs":{"secret":{"secretName":"test-istiod-client-cred","items":[{"key":"ca.pem","path":"root-cert.pem"},{"key":"cert.pem","path":"cert-chain.pem"},{"key":"key.pem","path":"key.pem"}]}}}`).
					Set(echo.SidecarVolumeMount, `{"server-certs":{"mountPath":"/server-certs"},"client-certs":{"mountPath":"/client-certs"},"workload-certs":{"mountPath":"/etc/certs"}}`).
					Set(echo.SidecarConfig, `{"controlPlaneAuthPolicy":"MUTUAL_TLS","proxyMetadata":` + strings.Replace(ProxyMetadataJson, "\n", "", -1) + `}`),
			}},
		}).
		With(&internalServer, echo.Config{
			Service:   "server",
			Namespace: appsNamespace,
			Ports: []echo.Port{
				{
					Name:         "http",
					Protocol:     protocol.HTTP,
					ServicePort:  8443,
					InstancePort: 8443,
					TLS:          false,
				},
			},
			Subsets: []echo.SubsetConfig{{
				Version:     "v1",
				// Set up custom annotations to mount the certs.
				Annotations: echo.NewAnnotations().
					// workload-certs are needed in order to load the "default" SDS resource, which will be used for the xds-grpc mTLS (tls_certificate_sds_secret_configs.name == "default")
					// the default bootstrap template does not support reusing values from the `ISTIO_META_TLS_CLIENT_*` environment variables
					// see security/pkg/nodeagent/cache/secretcache.go:generateFileSecret() for details
					Set(echo.SidecarVolume, `{"server-certs":{"secret":{"secretName":"test-server-cred"}},"client-certs":{"secret":{"secretName":"test-client-cred"}},"workload-certs":{"secret":{"secretName":"test-istiod-client-cred","items":[{"key":"ca.pem","path":"root-cert.pem"},{"key":"cert.pem","path":"cert-chain.pem"},{"key":"key.pem","path":"key.pem"}]}}}`).
					Set(echo.SidecarVolumeMount, `{"server-certs":{"mountPath":"/server-certs"},"client-certs":{"mountPath":"/client-certs"},"workload-certs":{"mountPath":"/etc/certs"}}`).
					Set(echo.SidecarConfig, `{"controlPlaneAuthPolicy":"MUTUAL_TLS","proxyMetadata":` + strings.Replace(ProxyMetadataJson, "\n", "", -1) + `}`),
			}},
		}).
		BuildOrFail(t)

	return internalClient, internalServer, appsNamespace
}


func clusterName(target echo.Instance, port echo.Port) string {
	cfg := target.Config()
	return fmt.Sprintf("outbound|%d||%s.%s.svc.%s", port.ServicePort, cfg.Service, cfg.Namespace.Name(), cfg.Domain)
}


func getMetricForCluster(metrics []*dto.Metric, clusterName string) *dto.Metric {
	for _, metric := range metrics {
		for _, label := range metric.Label {
			if (*label.Name == "cluster_name" && *label.Value == clusterName) {
				return metric
			}
		}
	}
	return nil
}

func getMetricForListener(metrics []*dto.Metric, listenerName string) *dto.Metric {
	for _, metric := range metrics {
		for _, label := range metric.Label {
			if (*label.Name == "listener_address" && *label.Value == listenerName) {
				return metric
			}
		}
	}
	return nil
}


func verifyUsingFileBasedCerts(t *testing.T, client echo.Instance, server echo.Instance) {
	// Verify client side configs
	clientWorkloads, _ := client.Workloads()
	if clientWorkloads[0].Sidecar() == nil {
		t.Fatalf("Sidcecar is expected, but was not found")
	}

	configDump, err := clientWorkloads[0].Sidecar().Config()
	if err != nil {
		t.Fatalf("Unable to retrieve config_dump from the client. %v", err)
	}

	clusterName := clusterName(server, echo.Port{ServicePort: 8443})

	validator := structpath.ForProto(configDump)
	destCluster := validator.Select("{.configs[*].dynamicActiveClusters[?(@.cluster.name == '%s')]}", clusterName)
	// Ensure that the Destination/Server cluster is using file based tls configs
	err = destCluster.Exists("{.cluster.transportSocket.typedConfig.commonTlsContext.tlsCertificateSdsSecretConfigs[?(@.name == 'file-cert:/client-certs/cert.pem~/client-certs/key.pem')]}}").Check()
	if err != nil {
		t.Fatalf("Destination cluster on the Client side is expected to use FileBased SDS configs. %v", err)
	}


	// Verify server side configs
	serverWorkloads, _ := server.Workloads()
	if serverWorkloads[0].Sidecar() == nil {
		t.Fatalf("Sidcecar is expected, but was not found")
	}

	configDump, err = serverWorkloads[0].Sidecar().Config()
	if err != nil {
		t.Fatalf("Unable to retrieve config_dump from the server. %v", err)
	}

	validator = structpath.ForProto(configDump)
	virtualInboundListener := validator.Select("{.configs[*].dynamicListeners[?(@.name == 'virtualInbound')]}")
	err = virtualInboundListener.Select("{.activeState.listener.filterChains[*].transportSocket.typedConfig.commonTlsContext.tlsCertificateSdsSecretConfigs[?(@.name == 'file-cert:/server-certs/cert.pem~/server-certs/key.pem')]}").Check()
	if err != nil {
		t.Fatalf("Server is expected to use FileBased SDS configs with DownstreamTlsContext. %v", err)
	}
}

func verifyStats(t *testing.T, client echo.Instance, server echo.Instance) {
	// Verify client side stats
	clientWorkloads, _ := client.Workloads()
	if clientWorkloads[0].Sidecar() == nil {
		t.Fatalf("Sidcecar is expected, but was not found")
	}

	stats := clientWorkloads[0].Sidecar().StatsOrFail(t)

	clusterName := clusterName(server, echo.Port{ServicePort: 8443})
	var metric *dto.Metric

	clientTestCases := []struct {
		metricName string
		expectedValue float64
	}{
		{
			metricName: "envoy_cluster_ssl_handshake",
			expectedValue: 1.0,
		},
		{
			metricName: "envoy_cluster_upstream_rq_200",
			expectedValue: 1.0,
		},
	}

	for _, tc := range clientTestCases {
		metric = getMetricForCluster(stats[tc.metricName].Metric, clusterName)
		if *metric.Counter.Value < tc.expectedValue {
			t.Fatalf("%s expected, but not found. Expected - %v, Got - %v", tc.metricName,  tc.expectedValue, metric.Counter.Value)
		}
	}


	// Verify server side stats
	serverWorkloads, _ := server.Workloads()
	if serverWorkloads[0].Sidecar() == nil {
		t.Fatalf("Sidcecar is expected, but was not found")
	}

	stats = serverWorkloads[0].Sidecar().StatsOrFail(t)

	serverTestCases := []struct {
		metricName string
		expectedValue float64
	}{
		{
			metricName: "envoy_listener_ssl_handshake",
			expectedValue: 1.0,
		},
		{
			metricName: "envoy_listener_ssl_versions_TLSv1_2",
			expectedValue: 1.0,
		},
		{
			metricName: "envoy_listener_http_inbound_0_0_0_0_8443_downstream_rq_completed",
			expectedValue: 1.0,
		},
	}

	for _, tc := range serverTestCases {
		metric = getMetricForListener(stats[tc.metricName].Metric, "0.0.0.0_15006") // Check metrics on VirtualInbound listener
		if *metric.Counter.Value < tc.expectedValue {
			t.Fatalf("%s expected, but not found. Expected - %v, Got - %v", tc.metricName,  tc.expectedValue, metric.Counter.Value)
		}
	}
}