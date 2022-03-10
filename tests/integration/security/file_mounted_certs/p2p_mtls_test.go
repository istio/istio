//go:build integ
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
	"strings"
	"testing"
	"time"

	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/test/echo/check"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/deployment"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/util/retry"
)

const (
	ServerSecretName = "test-server-cred"
	ServerCertsPath  = "tests/testdata/certs/mountedcerts-server"

	ClientSecretName = "test-client-cred"
	ClientCertsPath  = "tests/testdata/certs/mountedcerts-client"

	// nolint: lll
	ExpectedXfccHeader = `By=spiffe://cluster.local/ns/mounted-certs/sa/server;Hash=8ab5e491f91ab6970049bb1f032d53f4594279d38f381b1416ae10816f900c15;Subject="CN=cluster.local";URI=spiffe://cluster.local/ns/mounted-certs/sa/client;DNS=client.mounted-certs.svc`
)

func TestClientToServiceTls(t *testing.T) {
	framework.NewTest(t).
		Features("security.peer.file-mounted-certs").
		Run(func(t framework.TestContext) {
			client, server, serviceNamespace := setupEcho(t)

			createObject(t, serviceNamespace.Name(), DestinationRuleConfigMutual)
			createObject(t, "istio-system", PeerAuthenticationConfig)

			opts := echo.CallOptions{
				To: server,
				Port: echo.Port{
					Name: "http",
				},
				Check: check.And(
					check.OK(),
					check.RequestHeader("X-Forwarded-Client-Cert", ExpectedXfccHeader)),
				Retry: echo.Retry{
					Options: []retry.Option{retry.Delay(5 * time.Second), retry.Timeout(1 * time.Minute)},
				},
			}

			client.CallOrFail(t, opts)
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
	args := map[string]string{"AppNamespace": serviceNamespace}
	ctx.ConfigIstio().Eval(args, yamlManifest).ApplyOrFail(ctx, serviceNamespace)
}

// setupEcho creates an `istio-fd-sds` namespace and brings up two echo instances server and
// client in that namespace.
func setupEcho(t framework.TestContext) (echo.Instance, echo.Instance, namespace.Instance) {
	appsNamespace := namespace.NewOrFail(t, t, namespace.Config{
		Prefix: "istio-fd-sds",
		Inject: true,
	})

	// Server certificate has "server.file-mounted.svc" in SANs; Same is expected in DestinationRule.subjectAltNames for the test Echo server
	// This cert is going to be used as a server and "client" certificate on the "Echo Server"'s side
	err := CreateCustomSecret(t, ServerSecretName, appsNamespace, ServerCertsPath)
	if err != nil {
		t.Fatalf("Unable to create server secret. %v", err)
	}

	// Pilot secret will be used for xds connections from echo-server & echo-client to the control plane.
	err = CreateCustomSecret(t, PilotSecretName, appsNamespace, PilotCertsPath)
	if err != nil {
		t.Fatalf("Unable to create pilot secret. %v", err)
	}

	// Client secret will be used as a "server" and client certificate on the "Echo Client"'s side.
	// ie. it is going to be used for connections from EchoClient to EchoServer
	err = CreateCustomSecret(t, ClientSecretName, appsNamespace, ClientCertsPath)
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

	deployment.New(t).
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
					WorkloadPort: 8443,
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
