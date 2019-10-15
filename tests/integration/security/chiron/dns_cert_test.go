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

package chiron_test

import (
	"testing"
	"time"

	"istio.io/istio/tests/integration/security/util/secret"

	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/chiron"
	"istio.io/istio/pkg/test/framework/components/environment"
	"istio.io/istio/pkg/test/framework/components/istio"
)

const (
	// Specifies how long we wait before a secret becomes existent.
	secretWaitTime            = 20 * time.Second
	galleySecretName          = "dns.istio-galley-service-account"
	galleyDNSName             = "istio-galley.istio-system.svc"
	sidecarInjectorSecretName = "dns.istio-sidecar-injector-service-account"
	sidecarInjectorDNSName    = "istio-sidecar-injector.istio-system.svc"
)

func TestDNSCertificate(t *testing.T) {
	framework.NewTest(t).
		RequiresEnvironment(environment.Kube).
		Run(func(ctx framework.TestContext) {
			istio.DefaultConfigOrFail(t, ctx)
			c := chiron.NewOrFail(t, ctx, chiron.Config{Istio: inst})
			t.Log("check that DNS certificates have been generated ...")
			galleySecret := c.WaitForSecretToExistOrFail(t, galleySecretName, secretWaitTime)
			sidecarInjectorSecret := c.WaitForSecretToExistOrFail(t, sidecarInjectorSecretName, secretWaitTime)
			t.Log(`checking Galley DNS certificate is valid`)
			secret.ExamineDNSSecretOrFail(t, galleySecret, galleyDNSName)
			t.Log(`checking Sidecar Injector DNS certificate is valid`)
			secret.ExamineDNSSecretOrFail(t, sidecarInjectorSecret, sidecarInjectorDNSName)
		})
}
