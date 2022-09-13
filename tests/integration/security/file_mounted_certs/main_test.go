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
	"context"
	"fmt"
	"os"
	"path"
	"strings"
	"testing"

	v1 "k8s.io/api/core/v1"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/test/env"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/common/deployment"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/framework/label"
	"istio.io/istio/pkg/test/framework/resource"
)

var (
	inst         istio.Instance
	apps         deployment.SingleNamespaceView
	client       echo.Instances
	server       echo.Instances
	echo1NS      namespace.Instance
	customConfig []echo.Config
)

const (
	PilotCertsPath  = "tests/testdata/certs/pilot"
	PilotSecretName = "test-istiod-server-cred"

	ProxyMetadataJSON = `
	{
      "FILE_MOUNTED_CERTS": "true",
      "ISTIO_META_TLS_CLIENT_CERT_CHAIN": "/client-certs/cert-chain.pem",
      "ISTIO_META_TLS_CLIENT_KEY": "/client-certs/key.pem",
      "ISTIO_META_TLS_CLIENT_ROOT_CERT": "/client-certs/root-cert.pem",
      "ISTIO_META_TLS_SERVER_CERT_CHAIN": "/server-certs/cert-chain.pem",
      "ISTIO_META_TLS_SERVER_KEY": "/server-certs/key.pem",
      "ISTIO_META_TLS_SERVER_ROOT_CERT": "/server-certs/root-cert.pem",
	}
`
)

func TestMain(m *testing.M) {
	// nolint: staticcheck
	framework.
		NewSuite(m).
		Label(label.CustomSetup).
		RequireSingleCluster().
		RequireMultiPrimary().
		Label("CustomSetup").
		Setup(istio.Setup(&inst, setupConfig, CreateCustomIstiodSecret)).
		Setup(namespace.Setup(&echo1NS, namespace.Config{Prefix: "echo1", Inject: true})).
		Setup(func(ctx resource.Context) error {
			err := setupApps(ctx, namespace.Future(&echo1NS), &customConfig)
			if err != nil {
				return err
			}
			return nil
		}).
		Setup(deployment.SetupSingleNamespace(&apps, deployment.Config{
			Namespaces: []namespace.Getter{
				namespace.Future(&echo1NS),
			},
			Configs: echo.ConfigFuture(&customConfig),
		})).
		Setup(func(ctx resource.Context) error {
			return createCustomInstances(&apps)
		}).
		Run()
}

func setupConfig(_ resource.Context, cfg *istio.Config) {
	if cfg == nil {
		return
	}

	cfg.ControlPlaneValues = `
components:
  egressGateways:
  - enabled: true
    name: istio-egressgateway
    k8s:
      overlays:
        - kind: Deployment
          name: istio-egressgateway
          patches:
            - path: spec.template.spec.volumes[100]
              value: |-
                name: server-certs
                secret:
                  secretName: ` + PilotSecretName + `
                  defaultMode: 420
            - path: spec.template.spec.volumes[101]
              value: |-
                name: client-certs
                secret:
                  secretName: ` + PilotSecretName + `
                  defaultMode: 420
            - path: spec.template.spec.containers[0].volumeMounts[100]
              value: |-
                name: server-certs
                mountPath: /server-certs
            - path: spec.template.spec.containers[0].volumeMounts[101]
              value: |-
                name: client-certs
                mountPath: /client-certs

  ingressGateways:
  - enabled: true
    name: istio-ingressgateway
    k8s:
      overlays:
        - kind: Deployment
          name: istio-ingressgateway
          patches:
            - path: spec.template.spec.volumes[100]
              value: |-
                name: server-certs
                secret:
                  secretName: ` + PilotSecretName + `
                  defaultMode: 420
            - path: spec.template.spec.volumes[101]
              value: |-
                name: client-certs
                secret:
                  secretName: ` + PilotSecretName + `
                  defaultMode: 420
            - path: spec.template.spec.containers[0].volumeMounts[100]
              value: |-
                name: server-certs
                mountPath: /server-certs
            - path: spec.template.spec.containers[0].volumeMounts[101]
              value: |-
                name: client-certs
                mountPath: /client-certs

  pilot:
    enabled: true
    k8s:
      overlays:
        - kind: Deployment
          name: istiod
          patches:
            - path: spec.template.spec.containers.[name:discovery].args[1001]
              value: "--caCertFile=/server-certs/root-cert.pem"
            - path: spec.template.spec.containers.[name:discovery].args[1002]
              value: "--tlsCertFile=/server-certs/cert-chain.pem"
            - path: spec.template.spec.containers.[name:discovery].args[1003]
              value: "--tlsKeyFile=/server-certs/key.pem"
            - path: spec.template.spec.volumes[-1]
              value: |-
                name: server-certs
                secret:
                  secretName: ` + PilotSecretName + `
                  defaultMode: 420
            - path: spec.template.spec.containers[name:discovery].volumeMounts[-1]
              value: |-
                name: server-certs
                mountPath: /server-certs

meshConfig:
  defaultConfig:
    controlPlaneAuthPolicy: "MUTUAL_TLS"
    proxyMetadata: ` + strings.Replace(ProxyMetadataJSON, "\n", "", -1) +
		`
values:
  global:
    pilotCertProvider: "mycopki"
  pilot:
    env:
      # We need to turn off the XDS Auth because test certificates only have a fixed/hardcoded identity, but the identity of the actual
      # deployed test services changes on each run due to a randomly generated namespace suffixes.
      # Turning the XDS-Auth ON will result in the error messages like:
      # Unauthorized XDS: 10.1.0.159:41960 with identity [spiffe://cluster.local/ns/mounted-certs/sa/client client.mounted-certs.svc]:
      #    no identities ([spiffe://cluster.local/ns/mounted-certs/sa/client client.mounted-certs.svc]) matched istio-fd-sds-1-4523/default
      XDS_AUTH: "false"
`

	cfg.EastWestGatewayValues = `
components:
  ingressGateways:
  - enabled: true
    name: istio-eastwestgateway
    label:
      istio: eastwestgateway
      app: istio-eastwestgateway
    k8s:
      overlays:
        - kind: Deployment
          name: istio-eastwestgateway
          patches:
            - path: spec.template.spec.volumes[100]
              value: |-
                name: server-certs
                secret:
                  secretName: ` + PilotSecretName + `
                  defaultMode: 420
            - path: spec.template.spec.volumes[101]
              value: |-
                name: client-certs
                secret:
                  secretName: ` + PilotSecretName + `
                  defaultMode: 420
            - path: spec.template.spec.containers[0].volumeMounts[100]
              value: |-
                name: server-certs
                mountPath: /server-certs
            - path: spec.template.spec.containers[0].volumeMounts[101]
              value: |-
                name: client-certs
                mountPath: /client-certs
`
}

func CreateCustomIstiodSecret(ctx resource.Context) error {
	systemNs, err := istio.ClaimSystemNamespace(ctx)
	if err != nil {
		return err
	}

	err = CreateCustomSecret(ctx, PilotSecretName, systemNs, PilotCertsPath)
	if err != nil {
		return err
	}

	return nil
}

func CreateCustomSecret(ctx resource.Context, name string, namespace namespace.Instance, certsPath string) error {
	var privateKey, clientCert, caCert []byte
	var err error
	if privateKey, err = ReadCustomCertFromFile(certsPath, "key.pem"); err != nil {
		return err
	}
	if clientCert, err = ReadCustomCertFromFile(certsPath, "cert-chain.pem"); err != nil {
		return err
	}
	if caCert, err = ReadCustomCertFromFile(certsPath, "root-cert.pem"); err != nil {
		return err
	}

	kubeAccessor := ctx.Clusters().Default()
	secret := &v1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace.Name(),
		},
		Data: map[string][]byte{
			"key.pem":        privateKey,
			"cert-chain.pem": clientCert,
			"root-cert.pem":  caCert,
		},
	}

	_, err = kubeAccessor.Kube().CoreV1().Secrets(namespace.Name()).Create(context.TODO(), secret, metav1.CreateOptions{})
	if err != nil {
		if kerrors.IsAlreadyExists(err) {
			if _, err := kubeAccessor.Kube().CoreV1().Secrets(namespace.Name()).Update(context.TODO(), secret, metav1.UpdateOptions{}); err != nil {
				return fmt.Errorf("failed updating secret %s: %v", secret.Name, err)
			}
		} else {
			return fmt.Errorf("failed creating secret %s: %v", secret.Name, err)
		}
	}
	return nil
}

func ReadCustomCertFromFile(certsPath string, f string) ([]byte, error) {
	b, err := os.ReadFile(path.Join(env.IstioSrc, certsPath, f))
	if err != nil {
		return nil, err
	}
	return b, nil
}

func setupApps(ctx resource.Context, customNs namespace.Getter, customCfg *[]echo.Config) error {
	appsNamespace := customNs.Get()

	// Server certificate has "server.file-mounted.svc" in SANs; Same is expected in DestinationRule.subjectAltNames for the test Echo server
	// This cert is going to be used as a server and "client" certificate on the "Echo Server"'s side
	err := CreateCustomSecret(ctx, ServerSecretName, appsNamespace, ServerCertsPath)
	if err != nil {
		return fmt.Errorf("Unable to create server secret. %v", err)
	}

	// Pilot secret will be used for xds connections from echo-server & echo-client to the control plane.
	err = CreateCustomSecret(ctx, PilotSecretName, appsNamespace, PilotCertsPath)
	if err != nil {
		return fmt.Errorf("Unable to create pilot secret. %v", err)
	}

	// Client secret will be used as a "server" and client certificate on the "Echo Client"'s side.
	// ie. it is going to be used for connections from EchoClient to EchoServer
	err = CreateCustomSecret(ctx, ClientSecretName, appsNamespace, ClientCertsPath)
	if err != nil {
		return fmt.Errorf("Unable to create client secret. %v", err)
	}

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

	var customConfig []echo.Config

	client := echo.Config{
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
	}

	server := echo.Config{
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
	}
	customConfig = append(customConfig, client, server)
	*customCfg = customConfig
	return nil
}

func createCustomInstances(apps *deployment.SingleNamespaceView) error {
	for index, namespacedName := range apps.EchoNamespace.All.NamespacedNames() {
		switch {
		case namespacedName.Name == "client":
			client = apps.EchoNamespace.All[index]
		case namespacedName.Name == "server":
			server = apps.EchoNamespace.All[index]
		}
	}
	return nil
}
