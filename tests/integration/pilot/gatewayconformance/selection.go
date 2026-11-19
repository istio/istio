//go:build integ

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

// Package gatewayconformance holds helpers shared by the Gateway API conformance
// runners and Gateway integration tests.
package gatewayconformance

import (
	"fmt"
	"regexp"

	"sigs.k8s.io/gateway-api/conformance/utils/suite"
	"sigs.k8s.io/gateway-api/pkg/features"

	"istio.io/istio/pkg/kube/clustertrustbundle"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/istio"
)

const clusterTrustBundleFlag = "PILOT_ENABLE_GATEWAY_API_CLUSTER_TRUST_BUNDLE"

// Accept both YAML and --set overrides.
var clusterTrustBundleFlagDisabledRE = regexp.MustCompile(clusterTrustBundleFlag + `["']?\s*[:=]\s*["']?false`)

// ClusterTrustBundleSupported reports whether ClusterTrustBundle references work in this installation, and if not, why:
// the feature flag must not be disabled and every config cluster must serve the certificates.k8s.io/v1 API.
func ClusterTrustBundleSupported(t framework.TestContext) (bool, string) {
	t.Helper()
	cfg := istio.DefaultConfigOrFail(t, t)
	overrides := []string{cfg.ControlPlaneValues, cfg.ConfigClusterValues, cfg.RemoteClusterValues}
	for k, v := range cfg.Values {
		overrides = append(overrides, k+"="+v)
	}
	for _, o := range overrides {
		if clusterTrustBundleFlagDisabledRE.MatchString(o) {
			return false, fmt.Sprintf("%s is disabled in the istio installation", clusterTrustBundleFlag)
		}
	}
	for _, c := range t.Clusters().Configs() {
		if !clustertrustbundle.IsServed(c) {
			return false, fmt.Sprintf("cluster %s does not serve the certificates.k8s.io/v1 ClusterTrustBundle API (requires Kubernetes 1.37+)",
				c.Name())
		}
	}
	return true, ""
}

// SkipNonStandardTests returns skipTests extended with every conformance test that requires a
// Gateway API feature outside the standard channel.
func SkipNonStandardTests(conformanceTests []suite.ConformanceTest, skippedTests map[string]string) map[string]string {
	skips := make(map[string]string, len(skippedTests))
	for name, reason := range skippedTests {
		skips[name] = reason
	}

	for _, test := range conformanceTests {
		if _, exists := skips[test.ShortName]; exists {
			continue
		}
		for _, name := range test.Features {
			if features.GetFeature(name).Channel != features.FeatureChannelStandard {
				skips[test.ShortName] = fmt.Sprintf("requires non-standard Gateway API feature %q", name)
				break
			}
		}
	}
	return skips
}
