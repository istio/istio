//go:build integ

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

package crl

import (
	"fmt"
	"testing"

	"istio.io/api/label"
	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/common/deployment"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/components/namespace"
	testlabel "istio.io/istio/pkg/test/framework/label"
	"istio.io/istio/pkg/test/framework/resource"
	"istio.io/istio/tests/integration/security/crl/util"
)

const (
	ambientControlPlaneValues = `
values:
  pilot:
    env:
      ENABLE_CA_CRL: "true"
  cni:
    repair:
      enabled: false
  ztunnel:
    terminationGracePeriodSeconds: 5
    logLevel: debug
    env:
      SECRET_TTL: 5m
    peerCaCrl:
      enabled: true
`
)

var (
	certBundle *util.Bundle
	clientNS   namespace.Instance
	serverNS   namespace.Instance
	client     echo.Instance
	server     echo.Instance
	configs    []echo.Config
	apps       deployment.TwoNamespaceView
)

func TestMain(m *testing.M) {
	framework.
		NewSuite(m).
		Label(testlabel.CustomSetup).
		Setup(func(ctx resource.Context) error {
			ctx.Settings().Ambient = true
			var err error
			certBundle, err = util.GenerateBundle(ctx)
			return err
		}).
		Setup(istio.Setup(nil, func(ctx resource.Context, cfg *istio.Config) {
			cfg.EnableCNI = true
			cfg.DeployEastWestGW = false
			cfg.ControlPlaneValues = ambientControlPlaneValues
		}, nil)).
		SetupParallel(
			namespace.Setup(&clientNS, namespace.Config{
				Prefix: "ambient-client",
				Inject: false,
				Labels: map[string]string{
					label.IoIstioDataplaneMode.Name: "ambient",
				},
			}),
			namespace.Setup(&serverNS, namespace.Config{
				Prefix: "ambient-server",
				Inject: false,
				Labels: map[string]string{
					label.IoIstioDataplaneMode.Name: "ambient",
				},
			}),
		).
		Setup(func(ctx resource.Context) error {
			return setupAppsConfig(ctx, &configs)
		}).
		SetupParallel(deployment.SetupTwoNamespaces(&apps, deployment.Config{
			Namespaces: []namespace.Getter{
				namespace.Future(&clientNS),
				namespace.Future(&serverNS),
			},
			Configs:             echo.ConfigFuture(&configs),
			NoExternalNamespace: true,
		})).
		Setup(func(ctx resource.Context) error {
			for _, echoInstance := range apps.All.Instances() {
				switch echoInstance.Config().Service {
				case "ambient-client":
					client = echoInstance
				case "ambient-server":
					server = echoInstance
				}
			}
			if client == nil || server == nil {
				return fmt.Errorf("failed to find ambient-client or ambient-server echo instance")
			}
			return nil
		}).
		Run()
}

func setupAppsConfig(_ resource.Context, out *[]echo.Config) error {
	*out = []echo.Config{
		{
			Service:        "ambient-client",
			Namespace:      clientNS,
			ServiceAccount: true,
			Ports: []echo.Port{
				{
					Name:         "http",
					Protocol:     protocol.HTTP,
					WorkloadPort: 8080,
				},
			},
		},
		{
			Service:        "ambient-server",
			Namespace:      serverNS,
			ServiceAccount: true,
			Ports: []echo.Port{
				{
					Name:         "http",
					Protocol:     protocol.HTTP,
					WorkloadPort: 8080,
				},
			},
		},
	}
	return nil
}
