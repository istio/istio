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

package pilot

import (
	"fmt"
	"strings"
	"testing"

	"istio.io/istio/pkg/test/echo/common/scheme"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/check"
	"istio.io/istio/pkg/test/framework/resource/config/apply"
	"istio.io/istio/pkg/test/util/retry"
	"istio.io/istio/pkg/util/protomarshal"
)

// TestSidecarDynamicDNSServiceEntry tests DYNAMIC_DNS wildcard ServiceEntry with ISTIO_MUTUAL TLS
func TestSidecarDynamicDNSServiceEntry(t *testing.T) {
	framework.NewTest(t).
		Run(func(t framework.TestContext) {
			targetNs := apps.Namespace
			serviceAName := apps.A.Config().Service
			serviceBFQDN := apps.B.Config().ClusterLocalFQDN()
			serviceBClusterName := fmt.Sprintf("outbound|80||%s", serviceBFQDN)

			// Configure Sidecar to include SE/DR from istio-system
			sidecarConfig := fmt.Sprintf(`
apiVersion: networking.istio.io/v1
kind: Sidecar
metadata:
  name: restrict-service-a
spec:
  workloadSelector:
    labels:
      app: %s
  egress:
  - hosts:
    - "istio-system/*"  # Control plane and wildcard SE/DR
`, serviceAName)

			serviceEntry := fmt.Sprintf(`
apiVersion: networking.istio.io/v1
kind: ServiceEntry
metadata:
  name: internal-wildcard-http
spec:
  hosts:
  - "*.%s.svc.cluster.local"
  ports:
  - name: http
    number: 80
    protocol: HTTP
  location: MESH_INTERNAL
  resolution: DYNAMIC_DNS
  exportTo:
  - "*"
`, targetNs.Name())

			destinationRule := fmt.Sprintf(`
apiVersion: networking.istio.io/v1
kind: DestinationRule
metadata:
  name: internal-wildcard-dr
spec:
  host: "*.%s.svc.cluster.local"
  trafficPolicy:
    tls:
      mode: ISTIO_MUTUAL
  exportTo:
  - "*"
`, targetNs.Name())

			// Apply ServiceEntry and DestinationRule in istio-system namespace
			seCfg := t.ConfigIstio().YAML(i.Settings().SystemNamespace, serviceEntry, destinationRule)
			seCfg.ApplyOrFail(t, apply.Wait)
			t.Cleanup(func() {
				seCfg.DeleteOrFail(t)
			})

			// Apply Sidecar in apps namespace
			sidecarCfg := t.ConfigIstio().YAML(apps.Namespace.Name(), sidecarConfig)
			sidecarCfg.ApplyOrFail(t, apply.Wait)
			t.Cleanup(func() {
				sidecarCfg.DeleteOrFail(t)
			})

			// Verify call succeeds via DFP cluster (proves DYNAMIC_DNS works with ISTIO_MUTUAL)
			apps.A[0].CallOrFail(t, echo.CallOptions{
				Address: serviceBFQDN,
				Port:    echo.Port{ServicePort: 80},
				Scheme:  scheme.HTTP,
				Count:   1,
				Check:   check.OK(),
			})

			// Verify Service B's regular cluster is NOT in config (proves Sidecar blocks it)
			retry.UntilSuccessOrFail(t, func() error {
				workload := apps.A[0].WorkloadsOrFail(t)[0]
				configDump := workload.Sidecar().ConfigOrFail(t)

				// Convert config dump to JSON string for searching
				configJSON, err := protomarshal.Marshal(configDump)
				if err != nil {
					return fmt.Errorf("failed to marshal config dump: %v", err)
				}

				if strings.Contains(string(configJSON), serviceBClusterName) {
					return fmt.Errorf("Service B cluster should NOT be in config (Sidecar should block it)")
				}
				t.Logf("Service B cluster %q not found in config (Sidecar blocks concrete K8s services)", serviceBClusterName)
				return nil
			})
		})
}
