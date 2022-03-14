//go:build integ
// +build integ

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
	"os"
	"strings"
	"testing"

	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/common/ports"
	"istio.io/istio/pkg/test/framework/components/echo/deployment"
	"istio.io/istio/pkg/test/framework/components/namespace"
)

// TestRevisionTraffic checks that traffic between all revisions specified works.
// This is similar to TestMultiVersionRevision, except it doesn't actually install the version of Istio under test.
// This allows a conformance-style tests against existing installations in the cluster.
// The test is completely skipped if ISTIO_TEST_EXTRA_REVISIONS is not set
// To run, add each revision to test. eg `ISTIO_TEST_EXTRA_REVISIONS=canary,my-rev`.
func TestRevisionTraffic(t *testing.T) {
	rawExtraRevs, f := os.LookupEnv("ISTIO_TEST_EXTRA_REVISIONS")
	if !f {
		t.Skip("ISTIO_TEST_EXTRA_REVISIONS not specified")
	}
	extraRevs := strings.Split(rawExtraRevs, ",")
	framework.NewTest(t).
		RequiresSingleCluster().
		RequiresLocalControlPlane().
		Features("installation.upgrade").
		Run(func(t framework.TestContext) {
			namespaces := make([]revisionedNamespace, 0, len(extraRevs))
			for _, rev := range extraRevs {
				namespaces = append(namespaces, revisionedNamespace{
					revision: rev,
					namespace: namespace.NewOrFail(t, t, namespace.Config{
						Prefix:   fmt.Sprintf("revision-%s", rev),
						Inject:   true,
						Revision: rev,
					}),
				})
			}
			// Allow all namespaces so we do not hit passthrough cluster
			t.ConfigIstio().YAML(`apiVersion: networking.istio.io/v1alpha3
kind: Sidecar
metadata:
  name: allow-cross-namespaces
spec:
  workloadSelector:
    labels:
      app: a
  egress:
  - hosts:
    - "*/*"`).ApplyOrFail(t, apps.Namespace.Name())
			// create an echo instance in each revisioned namespace, all these echo
			// instances will be injected with proxies from their respective versions
			builder := deployment.New(t).WithClusters(t.Clusters()...)
			for _, ns := range namespaces {
				builder = builder.WithConfig(echo.Config{
					Service:   ns.revision,
					Namespace: ns.namespace,
					Ports:     ports.All(),
					Subsets:   []echo.SubsetConfig{{}},
				})
			}
			instances := builder.BuildOrFail(t)
			// Add our existing revision to the instances list
			instances = append(instances, apps.PodA...)
			testAllEchoCalls(t, instances)
		})
}
