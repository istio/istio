//  Copyright 2019 Istio Authors
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

package sdsingress

import (
	"testing"

	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/environment"
	"istio.io/istio/pkg/test/framework/components/galley"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/components/pilot"
	"istio.io/istio/pkg/test/framework/label"
	"istio.io/istio/pkg/test/framework/resource"
)

var (
	inst istio.Instance
	g    galley.Instance
	p    pilot.Instance
)

var (
	singleTLSCredName  = []string{"bookinfo-credential-1"}
	singleTLCredCAName = []string{"bookinfo-credential-1-cacert"}
	singleTLSHost      = "bookinfo1.example.com"

	multipleTLSCredNames = []string{"bookinfo-credential-1", "bookinfo-credential-2", "bookinfo-credential-3",
		"bookinfo-credential-4", "bookinfo-credential-5"}
	multipleTLSHosts = []string{"bookinfo1.example.com", "bookinfo2.example.com", "bookinfo3.example.com",
		"bookinfo4.example.com", "bookinfo5.example.com"}
)

func TestMain(m *testing.M) {
	// Integration test for the ingress SDS Gateway flow.
	framework.
		NewSuite("sds_ingress_multi_mtls_gateway_invalid_secret_test", m).
		Label(label.CustomSetup).
		Label(label.Flaky).
		SetupOnEnv(environment.Kube, istio.Setup(&inst, setupConfig)).
		Setup(func(ctx resource.Context) (err error) {
			if g, err = galley.New(ctx, galley.Config{}); err != nil {
				return err
			}
			if p, err = pilot.New(ctx, pilot.Config{
				Galley: g,
			}); err != nil {
				return err
			}
			return nil
		}).
		Run()

}

func setupConfig(cfg *istio.Config) {
	if cfg == nil {
		return
	}
	// Enable SDS key/certificate provisioning for ingress gateway.
	cfg.Values["gateways.istio-ingressgateway.sds.enabled"] = "true"
	// TODO(https://github.com/istio/istio/issues/14084) remove this
	cfg.Values["pilot.env.PILOT_ENABLE_FALLTHROUGH_ROUTE"] = "0"
}
