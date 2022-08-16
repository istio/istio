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

package mtlsfirstpartyjwt

import (
	"testing"

	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/test/echo/common"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/common/deployment"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/framework/label"
	"istio.io/istio/pkg/test/framework/resource"
	"istio.io/istio/tests/integration/security/util"
)

var (
	inst          istio.Instance
	apps          deployment.SingleNamespaceView
	echo1NS       namespace.Instance
	customConfig  []echo.Config
	A             echo.Instances
	B             echo.Instances
	C             echo.Instances
	D             echo.Instances
	E             echo.Instances
	Multiversion  echo.Instances
	VM            echo.Instances
	External      echo.Instances
	Naked         echo.Instances
	Headless      echo.Instances
	HeadlessNaked echo.Instances
)

const (
	ASvc             = "a"
	BSvc             = "b"
	CSvc             = "c"
	DSvc             = "d"
	ESvc             = "e"
	MultiversionSvc  = "multiversion"
	VMSvc            = "vm"
	HeadlessSvc      = "headless"
	NakedSvc         = "naked"
	HeadlessNakedSvc = "headless-naked"
	ExternalSvc      = "external"
)

func TestMain(m *testing.M) {
	framework.
		NewSuite(m).
		Label(label.CustomSetup).
		Setup(istio.Setup(&inst, setupConfig)).
		Setup(namespace.Setup(&echo1NS, namespace.Config{Prefix: "echo1", Inject: true})).
		Setup(func(ctx resource.Context) error {
			err := setupApps(ctx, namespace.Future(&echo1NS), &customConfig, false)
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
		// Setup(func(ctx resource.Context) error {
		// 	// TODO: due to issue https://github.com/istio/istio/issues/25286,
		// 	// currently VM does not work in this test
		// 	return util.SetupApps(ctx, inst, apps, false)
		// }).
		Run()
}

func setupConfig(_ resource.Context, cfg *istio.Config) {
	if cfg == nil {
		return
	}
	cfg.Values["global.jwtPolicy"] = "first-party-jwt"
}

func setupApps(ctx resource.Context, customNs namespace.Getter, customCfg *[]echo.Config, buildVM bool) error {
	if ctx.Settings().Skip(echo.VM) {
		buildVM = false
	}
	appsNamespace := customNs.Get()

	var customConfig []echo.Config
	a := util.EchoConfig(ASvc, appsNamespace, false, nil)
	b := util.EchoConfig(BSvc, appsNamespace, false, nil)
	c := util.EchoConfig(CSvc, appsNamespace, false, nil)
	d := util.EchoConfig(DSvc, appsNamespace, false, nil)
	e := util.EchoConfig(ESvc, appsNamespace, false, nil)
	multiversionCfg := func() echo.Config {
		// Multi-version specific setup
		multiVersionCfg := util.EchoConfig(MultiversionSvc, appsNamespace, false, nil)
		multiVersionCfg.Subsets = []echo.SubsetConfig{
			// Istio deployment, with sidecar.
			{
				Version: "vistio",
			},
			// Legacy deployment subset, does not have sidecar injected.
			{
				Version:     "vlegacy",
				Annotations: echo.NewAnnotations().SetBool(echo.SidecarInject, false),
			},
		}
		return multiVersionCfg
	}()
	nakedSvc := util.EchoConfig(NakedSvc, appsNamespace, false, echo.NewAnnotations().
		SetBool(echo.SidecarInject, false))

	vmCfg := func() echo.Config {
		// VM specific setup
		vmCfg := util.EchoConfig(VMSvc, appsNamespace, false, nil)
		// for test cases that have `buildVM` off, vm will function like a regular pod
		vmCfg.DeployAsVM = buildVM
		return vmCfg
	}()

	externalSvc := echo.Config{
		Service: ExternalSvc,
		// Namespace: appsNamespace,
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
			RootCert:   util.MustReadCert("root-cert.pem"),
			ClientCert: util.MustReadCert("cert-chain.pem"),
			Key:        util.MustReadCert("key.pem"),
			// Override hostname to match the SAN in the cert we are using
			Hostname: "server.default.svc",
		},
		Subsets: []echo.SubsetConfig{{
			Version:     "v1",
			Annotations: echo.NewAnnotations().SetBool(echo.SidecarInject, false),
		}},
	}

	headlessSvc := util.EchoConfig(HeadlessSvc, appsNamespace, true, nil)
	headlessNakedSvc := util.EchoConfig(HeadlessNakedSvc, appsNamespace, true, echo.NewAnnotations().SetBool(echo.SidecarInject, false))

	customConfig = append(customConfig, a, b, c, d, e, multiversionCfg, nakedSvc, vmCfg, externalSvc, headlessSvc, headlessNakedSvc)
	*customCfg = customConfig
	return nil
}

func createCustomInstances(apps *deployment.SingleNamespaceView) error {
	for index, namespacedName := range apps.EchoNamespace.All.NamespacedNames() {
		switch {
		case namespacedName.Name == ASvc:
			A = apps.EchoNamespace.All[index]
		case namespacedName.Name == BSvc:
			B = apps.EchoNamespace.All[index]
		case namespacedName.Name == CSvc:
			C = apps.EchoNamespace.All[index]
		case namespacedName.Name == DSvc:
			D = apps.EchoNamespace.All[index]
		case namespacedName.Name == ESvc:
			E = apps.EchoNamespace.All[index]
		case namespacedName.Name == HeadlessSvc:
			Headless = apps.EchoNamespace.All[index]
		case namespacedName.Name == HeadlessNakedSvc:
			HeadlessNaked = apps.EchoNamespace.All[index]
		case namespacedName.Name == ExternalSvc:
			External = apps.EchoNamespace.All[index]
		case namespacedName.Name == NakedSvc:
			Naked = apps.EchoNamespace.All[index]
		case namespacedName.Name == VMSvc:
			VM = apps.EchoNamespace.All[index]
		case namespacedName.Name == MultiversionSvc:
			Multiversion = apps.EchoNamespace.All[index]
		}
	}
	return nil
}
