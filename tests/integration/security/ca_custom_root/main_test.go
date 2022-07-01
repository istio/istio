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
)

func TestMain(m *testing.M) {
	framework.
		NewSuite(m).
		// k8s is required because the plugin CA key and certificate are stored in a k8s secret.
		Label(label.CustomSetup).
		Setup(istio.Setup(&inst, setupConfig, cert.CreateCASecret)).
		Setup(deployment.SetupSingleNamespace(&apps, deployment.Config{})).
		Setup(func(ctx resource.Context) error {
			err := customsetup.SetupApps(&apps, ctx)
			if err != nil {
				return err
			}
			client = apps.CustomApps[0]
			server = apps.CustomApps[1]
			serverNakedFoo = apps.CustomApps[2]
			serverNakedBar = apps.CustomApps[3]
			serverNakedFooAlt = apps.CustomApps[4]
			return nil
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
