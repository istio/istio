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

package pqc

import (
	"fmt"
	"istio.io/api/annotation"
	"istio.io/istio/pkg/test/echo/common"
	"istio.io/istio/pkg/test/env"
	"istio.io/istio/pkg/test/framework/label"
	"istio.io/istio/pkg/test/util/file"
	"path"
	"testing"
	"time"

	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/common/deployment"
	"istio.io/istio/pkg/test/framework/components/echo/common/ports"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/framework/resource"
)

var (
	i          istio.Instance
	externalNs namespace.Instance
	internalNs namespace.Instance
	a          echo.Instance
	client     echo.Instance
	server     echo.Instance
	configs    []echo.Config
	apps       deployment.TwoNamespaceView
)

const controlPlaneValues = `
values:
  pilot:
    env:
      COMPLIANCE_POLICY: "pqc"
`

func TestMain(m *testing.M) {
	framework.
		NewSuite(m).
		Label(label.CustomSetup).
		Setup(istio.Setup(&i, func(ctx resource.Context, cfg *istio.Config) {
			cfg.DeployEastWestGW = false
			cfg.ControlPlaneValues = controlPlaneValues
		}, nil)).
		SetupParallel(
			namespace.Setup(&internalNs, namespace.Config{Prefix: "internal", Inject: true}),
			namespace.Setup(&externalNs, namespace.Config{Prefix: "external", Inject: false}),
		).
		Setup(func(ctx resource.Context) error {
			return setupAppsConfig(ctx, &configs)
		}).
		Setup(deployment.SetupTwoNamespaces(&apps, deployment.Config{
			Configs:             echo.ConfigFuture(&configs),
			Namespaces:          []namespace.Getter{namespace.Future(&internalNs), namespace.Future(&externalNs)},
			NoExternalNamespace: true,
		})).
		Setup(func(ctx resource.Context) error {
			for _, echoInstance := range apps.All.Instances() {
				switch echoInstance.Config().Service {
				case "client":
					client = echoInstance
				case "server":
					server = echoInstance
				case deployment.ASvc:
					a = echoInstance
				}
			}
			if client == nil || server == nil || a == nil {
				return fmt.Errorf("failed to find all expected echo instances")
			}
			return nil
		}).
		Run()
}

func setupAppsConfig(_ resource.Context, out *[]echo.Config) error {
	*out = []echo.Config{
		{
			Service:   "client",
			Namespace: externalNs,
			Ports:     ports.All(),
			Subsets: []echo.SubsetConfig{{
				Annotations: map[string]string{annotation.SidecarInject.Name: "false"},
			}},
		},
		{
			Service:   "server",
			Namespace: externalNs,
			Ports:     ports.All(),
			TLSSettings: &common.TLSSettings{
				RootCert:         file.MustAsString(path.Join(env.IstioSrc, "tests/testdata/certs/dns/root-cert.pem")),
				ClientCert:       file.MustAsString(path.Join(env.IstioSrc, "tests/testdata/certs/dns/cert-chain.pem")),
				Key:              file.MustAsString(path.Join(env.IstioSrc, "tests/testdata/certs/dns/key.pem")),
				Hostname:         "server.default.svc",
				MinVersion:       "1.3",
				CurvePreferences: []string{"X25519MLKEM768"},
			},
			Subsets: []echo.SubsetConfig{{
				Annotations: map[string]string{annotation.SidecarInject.Name: "false"},
			}},
		},
		{
			Service:   deployment.ASvc,
			Namespace: internalNs,
			Ports:     ports.All(),
		},
	}
	return nil
}

func TestPQC(t *testing.T) {
	framework.NewTest(t).
		Run(func(t framework.TestContext) {
			t.Log("PQC test placeholder")
			time.Sleep(1 * time.Minute)
		})
}
