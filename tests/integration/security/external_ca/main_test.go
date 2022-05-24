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

package externalca

import (
	"testing"

	csrctrl "istio.io/istio/pkg/test/csrctrl/controllers"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/deployment"
	"istio.io/istio/pkg/test/framework/components/echo/match"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/framework/label"
	"istio.io/istio/pkg/test/framework/resource"
	"istio.io/istio/pkg/test/util/tmpl"
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
	inst     istio.Instance
	apps     = &EchoDeployments{}
	stopChan = make(chan struct{})
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

	builder := deployment.New(ctx)
	builder.
		WithClusters(ctx.Clusters()...).
		WithConfig(util.EchoConfig(ASvc, apps.Namespace, false, nil)).
		WithConfig(util.EchoConfig(BSvc, apps.Namespace, false, nil))

	echos, err := builder.Build()
	if err != nil {
		return err
	}
	apps.A = match.ServiceName(echo.NamespacedName{Name: ASvc, Namespace: apps.Namespace}).GetMatches(echos)
	apps.B = match.ServiceName(echo.NamespacedName{Name: BSvc, Namespace: apps.Namespace}).GetMatches(echos)
	return nil
}

func TestMain(m *testing.M) {
	// Integration test for testing interoperability with external CA's that are integrated with K8s CSR API
	// Refer to https://kubernetes.io/docs/reference/access-authn-authz/certificate-signing-requests/
	// nolint: staticcheck
	framework.NewSuite(m).
		Label(label.CustomSetup).
		RequireMinVersion(19).
		RequireSingleCluster().
		RequireMultiPrimary().
		Setup(istio.Setup(&inst, setupConfig)).
		Setup(func(ctx resource.Context) error {
			return SetupApps(ctx, apps)
		}).
		Run()
	stopChan <- struct{}{}
	close(stopChan)
}

func setupConfig(ctx resource.Context, cfg *istio.Config) {
	certsChan := make(chan *csrctrl.SignerRootCert, 2)
	go csrctrl.RunCSRController("clusterissuers.istio.io/signer1,clusterissuers.istio.io/signer2", false,
		ctx.Clusters()[0].RESTConfig(), stopChan, certsChan)
	cert1 := <-certsChan
	cert2 := <-certsChan

	if cfg == nil {
		return
	}
	cfgYaml := tmpl.MustEvaluate(`
values:
  meshConfig:
    defaultConfig:
      proxyMetadata:
        PROXY_CONFIG_XDS_AGENT: "true"
        ISTIO_META_CERT_SIGNER: signer1
    trustDomainAliases: [some-other, trust-domain-foo]
    caCertificates:
    - pem: |
{{.rootcert1 | indent 8}}
      certSigners:
      - {{.signer1}}
    - pem: |
{{.rootcert2 | indent 8}}
      certSigners:
      - {{.signer2}}
components:
  pilot:
    enabled: true
    k8s:
      env:
      - name: CERT_SIGNER_DOMAIN
        value: clusterissuers.istio.io
      - name: EXTERNAL_CA
        value: ISTIOD_RA_KUBERNETES_API
      - name: PILOT_CERT_PROVIDER
        value: k8s.io/clusterissuers.istio.io/signer2
      overlays:
        # Amend ClusterRole to add permission for istiod to approve certificate signing by custom signer
        - kind: ClusterRole
          name: istiod-clusterrole-istio-system
          patches:
            - path: rules[-1]
              value: |
                apiGroups:
                - certificates.k8s.io
                resourceNames:
                - clusterissuers.istio.io/*
                resources:
                - signers
                verbs:
                - approve
`, map[string]string{"rootcert1": cert1.Rootcert, "signer1": cert1.Signer, "rootcert2": cert2.Rootcert, "signer2": cert2.Signer})
	cfg.ControlPlaneValues = cfgYaml
	cfg.DeployEastWestGW = false
}
