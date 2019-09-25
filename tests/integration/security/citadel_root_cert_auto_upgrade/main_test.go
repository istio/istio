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

package citadel_root_cert_auto_upgrade

import (
	"testing"

	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/environment"
	"istio.io/istio/pkg/test/framework/components/galley"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/components/pilot"
	"istio.io/istio/pkg/test/framework/components/prometheus"
	"istio.io/istio/pkg/test/framework/label"
	"istio.io/istio/pkg/test/framework/resource"
)

var (
	inst istio.Instance
	g    galley.Instance
	p    pilot.Instance
	prom prometheus.Instance
)

func TestMain(m *testing.M) {
	// Integration test for the Citadel auto root cert upgrade.
	framework.
		NewSuite("citadel_root_upgrade_test", m).
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
				if prom, err = prometheus.New(ctx); err != nil {
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
	cfg.Values["security.env.SELF_SIGNED_CA_CERT_TTL"] = "2m30s"
	cfg.Values["security.workloadCertTtl"] = "2m"
	cfg.Values["security.env.WORKLOAD_CERT_MIN_GRACE_PERIOD"] = "1m"
	cfg.Values["security.env.SELF_SIGNED_CA_CHECK_INTERVAL"] = "2m"
	cfg.Values["gateways.istio-egressgateway.enabled"] = "false"
}
