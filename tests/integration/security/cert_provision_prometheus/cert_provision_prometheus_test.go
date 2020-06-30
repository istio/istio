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

package certprovisionprometheus

import (
	"fmt"
	"strings"
	"testing"

	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/framework/label"
	util_dir "istio.io/istio/tests/integration/security/util/dir"
)

const (
	prometheusLabel      = "app=prometheus"
	prometheusContainter = "prometheus"
	prometheusCertDir    = "/etc/istio-certs/"
)

var (
	ist istio.Instance
)

// TestPrometheusCert tests provisioning a certificate to Prometheus.
func TestPrometheusCert(t *testing.T) {
	framework.
		NewTest(t).
		Run(func(ctx framework.TestContext) {
			systemNs := namespace.ClaimSystemNamespaceOrFail(ctx, ctx)
			util_dir.ListDir(systemNs, t, prometheusLabel, prometheusContainter,
				prometheusCertDir, validateCertDir)
		})
}

func validateCertDir(out string) error {
	if !strings.Contains(out, "cert-chain.pem") {
		return fmt.Errorf("the output doesn't contain cert chain file; the output: %v", out)
	}
	if !strings.Contains(out, "key.pem") {
		return fmt.Errorf("the output doesn't contain key file; the output: %v", out)
	}
	if !strings.Contains(out, "root-cert.pem") {
		return fmt.Errorf("the output doesn't contain root cert file; the output: %v", out)
	}

	return nil
}

func TestMain(m *testing.M) {
	framework.
		NewSuite(m).
		RequireSingleCluster().
		Label(label.CustomSetup).
		Setup(istio.Setup(&ist, setupConfig)).
		Run()
}

func setupConfig(cfg *istio.Config) {
	if cfg == nil {
		return
	}
	cfg.Values["meshConfig.enablePrometheusMerge"] = "false"
	cfg.Values["prometheus.enabled"] = "true"
}
