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

	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/test/echo/common/response"
	"istio.io/istio/pkg/test/echo/common/scheme"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/echoboot"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/framework/resource"
	"istio.io/istio/pkg/test/util/retry"
	"istio.io/istio/pkg/test/util/tmpl"
)

const (
	ServerSecretName = "test-server-cred"
	ServerCertsPath  = "tests/testdata/certs/mountedcerts-server"

	ClientSecretName = "test-client-cred"
	ClientCertsPath  = "tests/testdata/certs/mountedcerts-client"

	// nolint: lll
	ExpectedXfccHeader = "By=spiffe://cluster.local/ns/mounted-certs/sa/server;Hash=865a56be3583d64bb9dc447da34e39e45d9314313310c879a35f7be6e391ac3e;Subject=\"CN=cluster.local\";URI=spiffe://cluster.local/ns/mounted-certs/sa/client;DNS=client.mounted-certs.svc"
)

func TestClientToServiceTls(t *testing.T) {
	framework.NewTest(t).
		Features("security.peer.file-mounted-certs").
		Run(func(ctx framework.TestContext) {
			echoClient, echoServer, serviceNamespace := setupEcho(t, ctx)

			createObject(ctx, serviceNamespace.Name(), DestinationRuleConfigMutual)
			createObject(ctx, "istio-system", PeerAuthenticationConfig)

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
							return fmt.Errorf("XFCC header's value is incorrect. Expected [%s], received [%s]", ExpectedXfccHeader, r.RawResponse)
						}
					} else {
						return fmt.Errorf("expected to see XFCC header, but none found. response: %+v", r.RawResponse)
					}
				}
				return nil
			}, retry.Delay(5*time.Second), retry.Timeout(1*time.Minute))
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
        - server.mounted-certs.svc

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
)

func createObject(ctx framework.TestContext, serviceNamespace string, yamlManifest string) {
	template := tmpl.EvaluateOrFail(ctx, yamlManifest, map[string]string{"AppNamespace": serviceNamespace})
	ctx.Config().ApplyYAMLOrFail(ctx, serviceNamespace, template)
}

// setupEcho creates an `istio-fd-sds` namespace and brings up two echo instances server and
// client in that namespace.
func setupEcho(t *testing.T, ctx resource.Context) (echo.Instance, echo.Instance, namespace.Instance) {
	appsNamespace := namespace.NewOrFail(t, ctx, namespace.Config{
		Prefix: "istio-fd-sds",
		Inject: true,
	})

	// Server certificate has "server.file-mounted.svc" in SANs; Same is expected in DestinationRule.subjectAltNames for the test Echo server
	// This cert is going to be used as a server and "client" certificate on the "Echo Server"'s side
	err := CreateCustomSecret(ctx, ServerSecretName, appsNamespace, ServerCertsPath)
	if err != nil {
		t.Fatalf("Unable to create server secret. %v", err)
	}

	// Pilot secret will be used for xds connections from echo-server & echo-client to the control plane.
	err = CreateCustomSecret(ctx, PilotSecretName, appsNamespace, PilotCertsPath)
	if err != nil {
		t.Fatalf("Unable to create pilot secret. %v", err)
	}

	// Client secret will be used as a "server" and client certificate on the "Echo Client"'s side.
	// ie. it is going to be used for connections from EchoClient to EchoServer
	err = CreateCustomSecret(ctx, ClientSecretName, appsNamespace, ClientCertsPath)
	if err != nil {
		t.Fatalf("Unable to create client secret. %v", err)
	}

	var internalClient, internalServer echo.Instance

	clientSidecarVolumes := `
		{
			"server-certs": {"secret": {"secretName":"` + ClientSecretName + `"}},
			"client-certs": {"secret": {"secretName":"` + ClientSecretName + `"}},
			"workload-certs": {"secret": {"secretName": "` + ClientSecretName + `"}}
		}
	`

	serverSidecarVolumes := `
		{
			"server-certs": {"secret": {"secretName":"` + ServerSecretName + `"}},
			"client-certs": {"secret": {"secretName":"` + ServerSecretName + `"}},
			"workload-certs": {"secret": {"secretName":"` + ServerSecretName + `"}}
		}
	`

	// workload-certs are needed in order to load the "default" SDS resource, which
	// will be used for the xds-grpc mTLS (tls_certificate_sds_secret_configs.name == "default")
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
					Set(echo.SidecarVolume, clientSidecarVolumes).
					Set(echo.SidecarVolumeMount, sidecarVolumeMounts).
					// the default bootstrap template does not support reusing values from the `ISTIO_META_TLS_CLIENT_*` environment variables
					// see security/pkg/nodeagent/cache/secretcache.go:generateFileSecret() for details
					Set(echo.SidecarConfig, `{"controlPlaneAuthPolicy":"MUTUAL_TLS","proxyMetadata":`+strings.Replace(ProxyMetadataJSON, "\n", "", -1)+`}`),
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
				Version: "v1",
				// Set up custom annotations to mount the certs.
				Annotations: echo.NewAnnotations().
					Set(echo.SidecarVolume, serverSidecarVolumes).
					Set(echo.SidecarVolumeMount, sidecarVolumeMounts).
					// the default bootstrap template does not support reusing values from the `ISTIO_META_TLS_CLIENT_*` environment variables
					// see security/pkg/nodeagent/cache/secretcache.go:generateFileSecret() for details
					Set(echo.SidecarConfig, `{"controlPlaneAuthPolicy":"MUTUAL_TLS","proxyMetadata":`+strings.Replace(ProxyMetadataJSON, "\n", "", -1)+`}`),
			}},
		}).
		BuildOrFail(t)

	return internalClient, internalServer, appsNamespace
}
