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
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	dto "github.com/prometheus/client_model/go"
	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/test/echo/common/response"
	"istio.io/istio/pkg/test/echo/common/scheme"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/echoboot"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/framework/resource"
	"istio.io/istio/pkg/test/util/retry"
	"istio.io/istio/pkg/test/util/structpath"
	"istio.io/istio/pkg/test/util/tmpl"
)

const (
	ServerSecretName = "test-server-cred"
	DnsCertsPath = "tests/testdata/certs/dns"

	ClientSecretName = "test-client-cred"
	DefaultCertsPath = "tests/testdata/certs/default"

	ExpectedXfccHeader = "Hash=868c862d13ce14ffe6f1ac398365f4af706d0bbd1c2e264b0f3b945f1dd4ae8e;Subject=\"CN=cluster.local\";URI=spiffe://cluster.local/ns/default/sa/default"
)

var (
	vmEnv     map[string]string
)

func TestClientToServiceTls(t *testing.T) {
	framework.NewTest(t).
		Features("security.peer.file-mounted-certs").
		Run(func(ctx framework.TestContext) {

			echoClient, echoServer, serviceNamespace := setupEcho(t, ctx)

			bufDestinationRule := createObject(t, ctx, serviceNamespace.Name(), DestinationRuleConfigMutual)
			defer ctx.Config().DeleteYAMLOrFail(ctx, serviceNamespace.Name(), bufDestinationRule)

			bufPeerAuthentication := createObject(t, ctx, "istio-system", PeerAuthenticationConfig)
			defer ctx.Config().DeleteYAMLOrFail(ctx, "istio-system", bufPeerAuthentication)

			bufEnvoyFilter := createObject(t, ctx, serviceNamespace.Name(), EnvoyFilterConfig)
			defer ctx.Config().DeleteYAMLOrFail(ctx, serviceNamespace.Name(), bufEnvoyFilter)

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
					if xfcc, f := r.RawResponse["X-Forwarded-Client-Cert"]; f {
						if xfcc != ExpectedXfccHeader {
							return fmt.Errorf("XFCC header's value doesn't match the expectations: %+v", r.RawResponse)
						}
					} else {
						return fmt.Errorf("expected to see XFCC header, but none found. response: %+v", r.RawResponse)
					}
				}
				return nil
			}, retry.Delay(5*time.Second), retry.Timeout(1*time.Minute))

			verifyUsingFileBasedCerts(t, echoClient, echoServer)
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
      caCertificates: /client-certs/root-cert.pem
      clientCertificate: /client-certs/cert-chain.pem
      privateKey: /client-certs/key.pem
      subjectAltNames:
        - server.default.svc

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

func createObject(t *testing.T, ctx framework.TestContext, serviceNamespace string, yamlManifest string) string {
	template := tmpl.EvaluateOrFail(ctx, yamlManifest, map[string]string{"AppNamespace": serviceNamespace})
	ctx.Config().ApplyYAMLOrFail(ctx, serviceNamespace, template)
	return template
}

// setupEcho creates an `istio-fd-sds` namespace and brings up two echo instances server and
// client in that namespace.
func setupEcho(t *testing.T, ctx resource.Context) (echo.Instance, echo.Instance, namespace.Instance) {

	appsNamespace := namespace.NewOrFail(t, ctx, namespace.Config{
		Prefix: "istio-fd-sds",
		Inject: true,
	})


	// Server certificate has "server.default.svc" in SANs; Same is expected in DestinationRule.subjectAltNames for the test Echo server
	// This cert is going to be used as a "server certificate" on Echo Server's side
	CreateCustomSecret(ctx, ServerSecretName, appsNamespace, DnsCertsPath)

	// test-istiod-client-cred will be used for connections to the control plane.
	CreateCustomSecret(ctx, PilotSecretName, appsNamespace, PilotCertsPath)

	// test-client-cred will be used for client connections from EchoClient to EchoServer
	CreateCustomSecret(ctx, ClientSecretName, appsNamespace, DefaultCertsPath)

	var internalClient, internalServer echo.Instance

	clientSidecarVolumes := `
		{
			"server-certs": {"secret": {"secretName":"` + ClientSecretName + `"}},
			"client-certs": {"secret": {"secretName":"` + ClientSecretName + `"}},
			"workload-certs": {"secret": {"secretName": "` + PilotSecretName + `"}}
		}
	`

	serverSidecarVolumes := `
		{
			"server-certs": {"secret": {"secretName":"` + ServerSecretName + `"}},
			"client-certs": {"secret": {"secretName":"` + ServerSecretName + `"}},
			"workload-certs": {"secret": {"secretName":"` + PilotSecretName + `"}}
		}
	`

	sidecarVolumeMounts := `
		{
			"server-certs": {
				"mountPath": "/server-certs"
			},
			"client-certs": {
				"mountPath": "/client-certs"
			},
			"workload-certs": {
				"mountPath": "/etc/certs"
			}
		}
	`

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
					Set(echo.SidecarVolume, clientSidecarVolumes).
					Set(echo.SidecarVolumeMount, sidecarVolumeMounts).
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
					Set(echo.SidecarVolume, serverSidecarVolumes).
					Set(echo.SidecarVolumeMount, sidecarVolumeMounts).
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
	err = destCluster.Exists("{.cluster.transportSocket.typedConfig.commonTlsContext.tlsCertificateSdsSecretConfigs[?(@.name == 'file-cert:/client-certs/cert-chain.pem~/client-certs/key.pem')]}}").Check()
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
	err = virtualInboundListener.Select("{.activeState.listener.filterChains[*].transportSocket.typedConfig.commonTlsContext.tlsCertificateSdsSecretConfigs[?(@.name == 'file-cert:/server-certs/cert-chain.pem~/server-certs/key.pem')]}").Check()
	if err != nil {
		t.Fatalf("Server is expected to use FileBased SDS configs with DownstreamTlsContext. %v", err)
	}
}
