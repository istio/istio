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

package multiple_tls_gateway

import (
	"istio.io/istio/pkg/test/env"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/bookinfo"
	"istio.io/istio/pkg/test/framework/components/environment"
	"istio.io/istio/pkg/test/framework/components/environment/kube"
	"istio.io/istio/pkg/test/framework/components/ingress"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"path"
	"time"

	ingressutil "istio.io/istio/tests/integration/security/sds_ingress/util"

	"testing"
)

var (
	credNames = []string{"bookinfo-credential-1", "bookinfo-credential-2", "bookinfo-credential-3"}
	hosts = []string{"bookinfo1.example.com", "bookinfo2.example.com", "bookinfo3.example.com"}
)

func testMultiTlsGateways(t *testing.T, ctx framework.TestContext) { // nolint:interfacer
	t.Helper()

	// TODO(JimmyCYJ): Add support into ingress package to test TLS/mTLS ingress gateway in Minikube
	//  environment
	if ctx.Environment().(*kube.Environment).Settings().Minikube {
		t.Skip("https://github.com/istio/istio/issues/14180")
	}

	bookinfoNs, err := namespace.New(ctx, "istio-bookinfo", true)
	if err != nil {
		t.Fatalf("Could not create istio-bookinfo Namespace; err:%v", err)
	}
	d := bookinfo.DeployOrFail(t, ctx, bookinfo.Config{Namespace: bookinfoNs, Cfg: bookinfo.BookInfo})


	env.BookInfoRoot = path.Join(env.IstioRoot, "tests/integration/security/sds_ingress/multiple_tls_gateway")
	var gatewayPath bookinfo.ConfigFile = "testdata/bookinfo-multiple-gateways.yaml"
	g.ApplyConfigOrFail(
		t,
		d.Namespace(),
		gatewayPath.LoadGatewayFileWithNamespaceOrFail(t, bookinfoNs.Name()))

	var virtualSvcPath bookinfo.ConfigFile = "testdata/bookinfo-multiple-virtualservices.yaml"
	var destRulePath bookinfo.ConfigFile = "testdata/bookinfo-multiple-destinationrules.yaml"
	g.ApplyConfigOrFail(
		t,
		d.Namespace(),
		destRulePath.LoadWithNamespaceOrFail(t, bookinfoNs.Name()),
		virtualSvcPath.LoadWithNamespaceOrFail(t, bookinfoNs.Name()))

	ingressutil.CreateIngressKubeSecret(t, ctx, credNames)
	ing := ingress.NewOrFail(t, ctx, ingress.Config{Istio: inst, IngressType: ingress.Tls, CaCert: ingressutil.CaCert})
	time.Sleep(3 * time.Second)

	for _, h := range hosts {
		err = ingressutil.VisitProductPage(ing, h, 30*time.Second, 200, t)
		if err != nil {
			t.Fatalf("unable to retrieve 200 from product page at host %s: %v", h, err)
		}
	}
}

func TestTlsGateways(t *testing.T) {
	framework.
		NewTest(t).
		RequiresEnvironment(environment.Kube).
			Run(func(ctx framework.TestContext) {
			testMultiTlsGateways(t, ctx)
			})
}