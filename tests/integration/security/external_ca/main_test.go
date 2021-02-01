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

package externalca

import (
	"testing"

	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/echoboot"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/framework/label"
	"istio.io/istio/pkg/test/framework/resource"
	"istio.io/istio/tests/integration/security/util"
)

const (
	ASvc = "a"
	BSvc = "b"
)

type EchoDeployments struct {
	Namespace namespace.Instance
	// workloads for TestSecureNaming
	A, B echo.Instances
}

var (
	inst istio.Instance
	apps = &EchoDeployments{}
)

func SetupApps(ctx resource.Context, apps *EchoDeployments) error {
	var err error
	apps.Namespace, err = namespace.New(ctx, namespace.Config{
		Prefix: "test-ns",
		Inject: true,
	})
	if err != nil {
		return err
	}

	builder := echoboot.NewBuilder(ctx)
	builder.
		WithClusters(ctx.Clusters()...).
		WithConfig(util.EchoConfig(ASvc, apps.Namespace, false, nil)).
		WithConfig(util.EchoConfig(BSvc, apps.Namespace, false, nil))

	echos, err := builder.Build()
	if err != nil {
		return err
	}
	apps.A = echos.Match(echo.Service(ASvc))
	apps.B = echos.Match(echo.Service(BSvc))
	return nil
}

func TestMain(m *testing.M) {
	// Integration test for testing interoperability with external CA's that are integrated with K8s CSR API
	// Refer to https://kubernetes.io/docs/reference/access-authn-authz/certificate-signing-requests/
	framework.NewSuite(m).
		Label(label.CustomSetup).
		RequireEnvironmentVersion("1.18").
		Setup(istio.Setup(&inst, setupConfig)).
		Setup(func(ctx resource.Context) error {
			return SetupApps(ctx, apps)
		}).
		Run()
}

func setupConfig(_ resource.Context, cfg *istio.Config) {
	if cfg == nil {
		return
	}

	// TODO: Replace K8s legacy signer by deploying external signer common to all clusters with a known root
	cfg.ControlPlaneValues = `
components:
  pilot:
    k8s:
      env:
      - name: EXTERNAL_CA
        value: ISTIOD_RA_KUBERNETES_API
      - name: K8S_SIGNER
        value: kubernetes.io/legacy-unknown
values:
  meshConfig:
    trustDomainAliases: [some-other, trust-domain-foo]
`
}
