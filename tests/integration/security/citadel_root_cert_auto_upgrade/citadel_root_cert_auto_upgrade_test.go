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

package citadel_root_cert_auto_upgrade

import (
	"bytes"
	"fmt"
	"testing"
	"time"

	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/environment"
	"istio.io/istio/pkg/test/framework/components/environment/kube"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/framework/components/prometheus"
	"istio.io/istio/security/pkg/pki/util"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	v1 "k8s.io/api/core/v1"
)

const(
	CASecret = "istio-ca-secret"
	caCertID = "ca-cert.pem"
)

func TestCitadelRootCertUpgrade(t *testing.T) {
	framework.NewTest(t).
		RequiresEnvironment(environment.Kube).
			Run(func(ctx framework.TestContext) {
				istioCfg := istio.DefaultConfigOrFail(t, ctx)

				// Get initial root cert.
				kubeAccessor := ctx.Environment().(*kube.Environment).Accessor
				systemNS := namespace.ClaimOrFail(t, ctx, istioCfg.SystemNamespace)
				caScrt, err := kubeAccessor.GetSecret(systemNS.Name()).Get(CASecret, metav1.GetOptions{})
				if err != nil {
					t.Fatalf("unable to load root secret: %s", err.Error())
				}

				// Get initial root cert upgrade count.
				last_root_cert_upgrade_count := 0
				query := fmt.Sprintf("sum(citadel_root_cert_upgrade_count)")
				v, err := getMetric(t, prom, query)
				if err == nil {
					last_root_cert_upgrade_count = int(v)
				} else {
					// If root cert is not upgrade, metric is not available. The error is
					// acceptable.
					t.Logf("unable to get value for metric citadel_root_cert_upgrade_count: %s", err.Error())
				}

				for i := 0; i < 2; i++ {
					// Wait for the next round of root cert upgrade
					time.Sleep(90 * time.Second)
					newCaScrt, err := kubeAccessor.GetSecret(systemNS.Name()).Get(CASecret, metav1.GetOptions{})
					if err != nil {
						t.Fatalf("unable to load root secret: %s", err.Error())
					}
					v, err = getMetric(t, prom, query)
					if err == nil {
						verifyRootUpgrade(t, last_root_cert_upgrade_count, int(v), caScrt, newCaScrt)
						last_root_cert_upgrade_count = int(v)
						caScrt = newCaScrt
					} else {
						t.Fatalf("unable to get value for metric citadel_root_cert_upgrade_count: %s", err.Error())
					}
				}
			})
}

// getMetric queries Prometheus server for metric, and returns value of the
// metric, or error if metric is not found.
func getMetric(t *testing.T, prometheus prometheus.Instance, query string) (int, error) {
	t.Helper()

	t.Logf("prometheus query: %s", query)
	value, err := prometheus.WaitForQuiesce(query)
	if err != nil {
		return 0, fmt.Errorf("could not get metrics from prometheus: %v", err)
	}

	got, err := prometheus.Sum(value, nil)
	if err != nil {
		t.Logf("value: %s", value.String())
		return 0, fmt.Errorf("could not find metric value: %v", err)
	}

	return int(got), nil
}

func verifyRootUpgrade(t *testing.T, lastCount, newCount int, lastScrt, newScrt *v1.Secret) {
	lastCount++
	if lastCount > newCount {
		t.Errorf("root upgrade count does not match, expected %d but got %d", lastCount, newCount)
	}
	if lastScrt == nil || newScrt == nil {
		t.Errorf("root secret is nil")
	}
	if bytes.Equal(lastScrt.Data[caCertID], newScrt.Data[caCertID]) {
		t.Errorf("new root cert and old root cert should be different")
	}
	cert, _ := util.ParsePemEncodedCertificate(newScrt.Data[caCertID])
	timeToExpire := cert.NotAfter
	t.Logf("verified that root cert is upgraded successfully, " +
		"citadel_root_cert_upgrade_count %d, ca cert expiration time %v", newCount, timeToExpire.String())
}
