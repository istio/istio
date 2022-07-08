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

// cacustomroot creates cluster with custom plugin root CA (samples/cert/ca-cert.pem)
// instead of using the auto-generated self-signed root CA.
package cacustomroot

import (
	"path"
	"testing"

	"istio.io/istio/pkg/test/env"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/customsetup"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/common/deployment"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/framework/components/util/cert"
	"istio.io/istio/pkg/test/framework/label"
	"istio.io/istio/pkg/test/framework/resource"
	"istio.io/istio/pkg/test/util/tmpl"
)

var (
	inst              istio.Instance
	apps              deployment.SingleNamespaceView
	client            echo.Instances
	server            echo.Instances
	serverNakedFoo    echo.Instances
	serverNakedBar    echo.Instances
	serverNakedFooAlt echo.Instances
	customConfig      []echo.Config
	echo1NS           namespace.Instance
)

func TestMain(m *testing.M) {
	framework.
		NewSuite(m).
		// k8s is required because the plugin CA key and certificate are stored in a k8s secret.
		Label(label.CustomSetup).
		Setup(istio.Setup(&inst, setupConfig, cert.CreateCASecret)).
		Setup(namespace.Setup(&echo1NS, namespace.Config{Prefix: "xyz1", Inject: true})).
		Setup(func(ctx resource.Context) error {
			err := customsetup.SetupApps(ctx, namespace.Future(&echo1NS), &customConfig)
			if err != nil {
				return err
			}
			return nil
		}).
		Setup(deployment.SetupSingleNamespace(&apps, deployment.Config{
			Namespaces: []namespace.Getter{
				namespace.Future(&echo1NS),
			},
			Custom: echo.CustomFuture(&customConfig),
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
	// Add alternate root certificate to list of trusted anchors
	script := path.Join(env.IstioSrc, "samples/certs", "root-cert-alt.pem")
	rootPEM, err := cert.LoadCert(script)
	if err != nil {
		return
	}

	cfgYaml := tmpl.MustEvaluate(`
values:
  pilot:
    env:
      ISTIO_MULTIROOT_MESH: true
  meshConfig:
    defaultConfig:
      proxyMetadata:
        PROXY_CONFIG_XDS_AGENT: "true"
    trustDomainAliases: [some-other, trust-domain-foo]
    caCertificates:
    - pem: |
{{.pem | indent 8}}
`, map[string]string{"pem": rootPEM})
	cfg.ControlPlaneValues = cfgYaml
}

func createCustomInstances(apps *deployment.SingleNamespaceView) error {
	for index, namespacedName := range apps.EchoNamespace.All.NamespacedNames() {
		switch {
		case namespacedName.Name == "client":
			client = apps.EchoNamespace.All[index]
		case namespacedName.Name == "server":
			server = apps.EchoNamespace.All[index]
		case namespacedName.Name == "server-naked-foo":
			serverNakedFoo = apps.EchoNamespace.All[index]
		case namespacedName.Name == "server-naked-bar":
			serverNakedBar = apps.EchoNamespace.All[index]
		case namespacedName.Name == "server-naked-foo-alt":
			serverNakedFooAlt = apps.EchoNamespace.All[index]
		}
	}
	return nil
}
