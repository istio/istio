// Copyright 2019 Istio Authors
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

package single_tls_gateway

import (
	"path"
	"time"

	"istio.io/istio/pkg/test/env"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/bookinfo"
	"istio.io/istio/pkg/test/framework/components/environment"
	"istio.io/istio/pkg/test/framework/components/environment/kube"
	"istio.io/istio/pkg/test/framework/components/ingress"
	"istio.io/istio/pkg/test/framework/components/namespace"

	ingressutil "istio.io/istio/tests/integration/security/sds_ingress/util"

	"testing"
)

var (
	credName = []string{"bookinfo-credential-4"}
	host     = "bookinfo4.example.com"
)

// testSingleTlsGateway tests a single TLS ingress gateway with SDS enabled. Verifies behaviors in three stages.
// (1) no kubernetes secret is provisioned, which means private key and server certificate are not available.
// (2) invalid kubernetes secret is provisioned, which would cause SSL handshake fail.
// (3) valid kubernetes secret is provisioned, and gateway should terminate SSL connection successfully.

// TestSingleTlsGateway_CredentialNotAvailable tests a single TLS ingress gateway with SDS enabled.
// Verifies behavior in these scenarios.
// (1) when kubernetes secret is not provisioned, which means private key and server certificate
// are not available. Verifies that listener is not up and connection creation get rejected.
// (2) A valid kubernetes secret with key/cert is added later, verifies that SSL connection termination is working properly.
// (3) After key/cert rotation, client needs to pick new CA cert to complete SSL connection. Old CA
// cert will cause the SSL connection fail.
func TestSingleTlsGateway_CredentialNotAvailable(t *testing.T) {
	framework.
		NewTest(t).
		RequiresEnvironment(environment.Kube).
		Run(func(ctx framework.TestContext) {
			// TODO(JimmyCYJ): Add support into ingress package to test TLS/mTLS ingress gateway in Minikube
			//  environment
			if ctx.Environment().(*kube.Environment).Settings().Minikube {
				t.Skip("https://github.com/istio/istio/issues/14180")
			}

			deployBookinfo(t, ctx)

			// Do not provide private key and server certificate for ingress gateway. Connection creation should fail.
			ingA := ingress.NewOrFail(t, ctx, ingress.Config{Istio: inst, IngressType: ingress.Tls, CaCert: ingressutil.CaCertA})
			err := ingressutil.VisitProductPage(ingA, host, 30*time.Second,
				ingressutil.ExpectedResponse{ResponseCode: 0, ErrorMessage: "connection refused"}, t)
			if err != nil {
				t.Fatalf("unable to retrieve code 0 from product page at host %s: %v", host, err)
			}

			// Add kubernetes secret to provision key/cert for ingress gateway.
			ingressutil.CreateIngressKubeSecret(t, ctx, credName, ingress.Tls, ingressutil.IngressCredentialA)
			// Wait for ingress gateway to fetch key/cert from Gateway agent via SDS.
			time.Sleep(3 * time.Second)
			ingB := ingress.NewOrFail(t, ctx, ingress.Config{Istio: inst, IngressType: ingress.Tls, CaCert: ingressutil.CaCertA})
			err = ingressutil.VisitProductPage(ingB, host, 30*time.Second,
				ingressutil.ExpectedResponse{ResponseCode: 200, ErrorMessage: ""}, t)
			if err != nil {
				t.Fatalf("unable to retrieve 200 from product page at host %s: %v", host, err)
			}

			// key/cert rotation
			ingressutil.RotateSecrets(t, ctx, credName, ingress.Tls, ingressutil.IngressCredentialB)
			// Wait for ingress gateway to fetch key/cert from Gateway agent via SDS.
			time.Sleep(3 * time.Second)
			ingC := ingress.NewOrFail(t, ctx, ingress.Config{Istio: inst, IngressType: ingress.Tls, CaCert: ingressutil.CaCertB})
			err = ingressutil.VisitProductPage(ingC, host, 30*time.Second,
				ingressutil.ExpectedResponse{ResponseCode: 200, ErrorMessage: ""}, t)
			if err != nil {
				t.Fatalf("unable to retrieve 200 from product page at host %s: %v", host, err)
			}
		})
}

func deployBookinfo(t *testing.T, ctx framework.TestContext) {
	bookinfoNs, err := namespace.New(ctx, "istio-bookinfo", true)
	if err != nil {
		t.Fatalf("Could not create istio-bookinfo Namespace; err:%v", err)
	}
	d := bookinfo.DeployOrFail(t, ctx, bookinfo.Config{Namespace: bookinfoNs, Cfg: bookinfo.BookInfo})

	env.BookInfoRoot = path.Join(env.IstioRoot, "tests/integration/security/sds_ingress/")
	var gatewayPath bookinfo.ConfigFile = "testdata/bookinfo-single-tls-gateway.yaml"
	g.ApplyConfigOrFail(
		t,
		d.Namespace(),
		gatewayPath.LoadGatewayFileWithNamespaceOrFail(t, bookinfoNs.Name()))

	var virtualSvcPath bookinfo.ConfigFile = "testdata/bookinfo-single-virtualservice.yaml"
	var destRulePath bookinfo.ConfigFile = "testdata/bookinfo-productpage-destinationrule.yaml"
	g.ApplyConfigOrFail(
		t,
		d.Namespace(),
		destRulePath.LoadWithNamespaceOrFail(t, bookinfoNs.Name()),
		virtualSvcPath.LoadWithNamespaceOrFail(t, bookinfoNs.Name()))

	time.Sleep(3 * time.Second)
}
