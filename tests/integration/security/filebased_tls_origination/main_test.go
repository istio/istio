//go:build integ

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

package filebasedtlsorigination

import (
	"log"
	"os"
	"path"
	"testing"

	"istio.io/api/annotation"
	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/test/echo/common"
	"istio.io/istio/pkg/test/env"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/common/deployment"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/framework/label"
	"istio.io/istio/pkg/test/framework/resource"
	"istio.io/istio/pkg/test/util/tmpl"
	"istio.io/istio/tests/integration/security/util/cert"
)

var (
	inst            istio.Instance
	apps            deployment.TwoNamespaceView
	client          echo.Instances
	server          echo.Instances
	internalClient  echo.Instances
	externalService echo.Instances
	appNS           namespace.Instance
	serviceNS       namespace.Instance
	customConfig    []echo.Config
)

func TestMain(m *testing.M) {
	// nolint: staticcheck
	framework.
		NewSuite(m).
		Label(label.CustomSetup).
		Setup(istio.Setup(&inst, setupConfig, cert.CreateCustomEgressSecret)).
		Setup(namespace.Setup(&appNS, namespace.Config{Prefix: "appns", Inject: true})).
		Setup(namespace.Setup(&serviceNS, namespace.Config{Prefix: "serverns", Inject: true})).
		Setup(func(ctx resource.Context) error {
			err := setupApps(ctx, namespace.Future(&appNS), namespace.Future(&serviceNS), &customConfig)
			if err != nil {
				return err
			}
			return nil
		}).
		Setup(deployment.SetupTwoNamespaces(&apps, deployment.Config{
			Namespaces: []namespace.Getter{
				namespace.Future(&appNS),
				namespace.Future(&serviceNS),
			},
			Configs: echo.ConfigFuture(&customConfig),
		})).
		Setup(func(ctx resource.Context) error {
			return createCustomInstances(&apps)
		}).
		Run()
}

func setupConfig(ctx resource.Context, cfg *istio.Config) {
	if cfg == nil {
		return
	}
	cfg.ControlPlaneValues = tmpl.MustEvaluate(`
{{- if not .isExternalControlPlane }}
components:
  egressGateways:
  - enabled: true
    name: istio-egressgateway
values:
   gateways:
      istio-egressgateway:
         secretVolumes:
         - name: client-custom-certs
           secretName: egress-gw-cacerts
           mountPath: /etc/certs/custom
{{- end }}
`, map[string]bool{"isExternalControlPlane": ctx.AllClusters().IsExternalControlPlane()})

	cfg.GatewayValues = `
components:
  egressGateways:
  - enabled: true
    name: istio-egressgateway
values:
  gateways:
    istio-egressgateway:
      secretVolumes:
      - name: client-custom-certs
        secretName: egress-gw-cacerts
        mountPath: /etc/certs/custom
`
}

func setupApps(ctx resource.Context, appNs namespace.Getter,
	serviceNs namespace.Getter, customCfg *[]echo.Config,
) error {
	appNamespace := appNs.Get()
	serviceNamespace := serviceNs.Get()
	var customConfig []echo.Config
	client := echo.Config{
		Service:   "client",
		Namespace: appNamespace,
		Ports:     []echo.Port{},
		Subsets: []echo.SubsetConfig{{
			Version: "v1",
			// Set up custom annotations to mount the certs. We will re-use the configmap created by "server"
			// so that we don't need to manage it ourselves.
			// The paths here match the destination rule above
			Annotations: map[string]string{
				annotation.SidecarUserVolume.Name:      `{"custom-certs":{"configMap":{"name":"server-certs"}}}`,
				annotation.SidecarUserVolumeMount.Name: `{"custom-certs":{"mountPath":"/etc/certs/custom"}}`,
			},
		}},
		Cluster: ctx.Clusters().Default(),
	}

	server := echo.Config{
		Service:   "server",
		Namespace: appNamespace,
		Ports: []echo.Port{
			{
				Name:         "grpc",
				Protocol:     protocol.GRPC,
				WorkloadPort: 8090,
				TLS:          true,
			},
			{
				Name:         "http",
				Protocol:     protocol.HTTP,
				WorkloadPort: 8091,
				TLS:          true,
			},
			{
				Name:         "tcp",
				Protocol:     protocol.TCP,
				WorkloadPort: 8092,
				TLS:          true,
			},
		},
		// Set up TLS certs on the server. This will make the server listen with these credentials.
		TLSSettings: &common.TLSSettings{
			RootCert:   mustReadCert("root-cert.pem"),
			ClientCert: mustReadCert("cert-chain.pem"),
			Key:        mustReadCert("key.pem"),
			// Override hostname to match the SAN in the cert we are using
			Hostname: "server.default.svc",
		},
		// Do not inject, as we are testing non-Istio TLS here
		Subsets: []echo.SubsetConfig{{
			Version:     "v1",
			Annotations: map[string]string{annotation.SidecarInject.Name: "false"},
		}},
		Cluster: ctx.Clusters().Default(),
	}

	internalClient := echo.Config{
		Service:   "internal-client",
		Namespace: appNamespace,
		Ports:     []echo.Port{},
		Subsets: []echo.SubsetConfig{{
			Version: "v1",
		}},
		Cluster: ctx.Clusters().Default(),
	}

	externalService := echo.Config{
		Service:   "external-service",
		Namespace: serviceNamespace,
		Ports: []echo.Port{
			{
				// Plain HTTP port only used to route request to egress gateway
				Name:         "http",
				Protocol:     protocol.HTTP,
				ServicePort:  80,
				WorkloadPort: 8080,
			},
			{
				// HTTPS port
				Name:         "https",
				Protocol:     protocol.HTTPS,
				ServicePort:  443,
				WorkloadPort: 8443,
				TLS:          true,
			},
		},
		// Set up TLS certs on the server. This will make the server listen with these credentials.
		TLSSettings: &common.TLSSettings{
			// Echo has these test certs baked into the docker image
			RootCert:   mustReadCert("root-cert.pem"),
			ClientCert: mustReadCert("cert-chain.pem"),
			Key:        mustReadCert("key.pem"),
			// Override hostname to match the SAN in the cert we are using
			Hostname: "external-service.default.svc",
		},
		Subsets: []echo.SubsetConfig{{
			Version:     "v1",
			Annotations: map[string]string{annotation.SidecarInject.Name: "false"},
		}},
		Cluster: ctx.Clusters().Default(),
	}
	customConfig = append(customConfig, client, server, internalClient, externalService)
	*customCfg = customConfig
	return nil
}

func createCustomInstances(apps *deployment.TwoNamespaceView) error {
	for index, namespacedName := range apps.Ns1.All.NamespacedNames() {
		switch {
		case namespacedName.Name == "client":
			client = apps.Ns1.All[index]
		case namespacedName.Name == "server":
			server = apps.Ns1.All[index]
		case namespacedName.Name == "internal-client":
			internalClient = apps.Ns1.All[index]
		}
	}
	for index, namespacedName := range apps.Ns2.All.NamespacedNames() {
		switch {
		case namespacedName.Name == "external-service":
			externalService = apps.Ns2.All[index]
		}
	}
	return nil
}

func mustReadCert(f string) string {
	b, err := os.ReadFile(path.Join(env.IstioSrc, "tests/testdata/certs/dns", f))
	if err != nil {
		log.Fatalf("failed to read %v: %v", f, err)
	}
	return string(b)
}
