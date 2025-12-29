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
	"path/filepath"
	"strings"
	"testing"
	"time"

	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/test/env"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/check"
	"istio.io/istio/pkg/test/framework/components/echo/deployment"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/framework/label"
	"istio.io/istio/pkg/test/framework/resource/config/apply"
	"istio.io/istio/pkg/test/util/file"
	"istio.io/istio/pkg/test/util/retry"
)

const (
	NMinusOne   = "1.10.0"
	NMinusTwo   = "1.9.5"
	NMinusThree = "1.8.6"
	NMinusFour  = "1.7.6"
	NMinusFive  = "1.6.11"
)

var versions = []string{NMinusOne, NMinusTwo, NMinusThree, NMinusFour, NMinusFive}

type revisionedNamespace struct {
	revision  string
	namespace namespace.Instance
}

// TestMultiVersionRevision tests traffic between data planes running under differently versioned revisions
// should test all possible revisioned namespace pairings to test traffic between all versions
func TestMultiVersionRevision(t *testing.T) {
	// nolint: staticcheck
	framework.NewTest(t).
		RequiresSingleCluster().
		RequiresLocalControlPlane().
		// Requires installation of CPs from manifests, won't succeed
		// if existing CPs have different root cert
		Label(label.CustomSetup).
		Run(func(t framework.TestContext) {
			t.Skip("https://github.com/istio/istio/pull/46213")
			skipIfK8sVersionUnsupported(t)

			// keep track of applied configurations and clean up after the test
			configs := make(map[string]string)
			t.CleanupConditionally(func() {
				for _, config := range configs {
					_ = t.ConfigIstio().YAML("istio-system", config).Delete()
				}
			})

			revisionedNamespaces := []revisionedNamespace{}
			for _, v := range versions {
				installRevisionOrFail(t, v, configs)

				// create a namespace pointed to the revisioned control plane we just installed
				rev := strings.ReplaceAll(v, ".", "-")
				ns, err := namespace.New(t, namespace.Config{
					Prefix:   fmt.Sprintf("revision-%s", rev),
					Inject:   true,
					Revision: rev,
				})
				if err != nil {
					t.Fatalf("failed to created revisioned namespace: %v", err)
				}
				revisionedNamespaces = append(revisionedNamespaces, revisionedNamespace{
					revision:  rev,
					namespace: ns,
				})
			}

			// create an echo instance in each revisioned namespace, all these echo
			// instances will be injected with proxies from their respective versions
			builder := deployment.New(t)

			for _, ns := range revisionedNamespaces {
				builder = builder.WithConfig(echo.Config{
					Service:   fmt.Sprintf("revision-%s", ns.revision),
					Namespace: ns.namespace,
					Ports: []echo.Port{
						{
							Name:         "http",
							Protocol:     protocol.HTTP,
							WorkloadPort: 8000,
						},
						{
							Name:         "tcp",
							Protocol:     protocol.TCP,
							WorkloadPort: 9000,
						},
						{
							Name:         "grpc",
							Protocol:     protocol.GRPC,
							WorkloadPort: 9090,
						},
					},
				})
			}
			instances := builder.BuildOrFail(t)
			// add an existing pod from apps to the rotation to avoid an extra deployment
			instances = append(instances, apps.A[0])

			testAllEchoCalls(t, instances)
		})
}

// testAllEchoCalls takes list of revisioned namespaces and generates list of echo calls covering
// communication between every pair of namespaces
func testAllEchoCalls(t framework.TestContext, echoInstances []echo.Instance) {
	trafficTypes := []string{"http", "tcp", "grpc"}
	for _, from := range echoInstances {
		for _, to := range echoInstances {
			if from == to {
				continue
			}
			for _, trafficType := range trafficTypes {
				t.NewSubTest(fmt.Sprintf("%s-%s->%s", trafficType, from.Config().Service, to.Config().Service)).
					Run(func(t framework.TestContext) {
						retry.UntilSuccessOrFail(t, func() error {
							result, err := from.Call(echo.CallOptions{
								To:    to,
								Count: 1,
								Port: echo.Port{
									Name: trafficType,
								},
								Retry: echo.Retry{
									NoRetry: true,
								},
							})
							return check.And(
								check.NoError(),
								check.OK()).Check(result, err)
						}, retry.Delay(time.Millisecond*150))
					})
			}
		}
	}
}

// installRevisionOrFail takes an Istio version and installs a revisioned control plane running that version
// provided istio version must be present in tests/integration/pilot/testdata/upgrade for the installation to succeed
func installRevisionOrFail(t framework.TestContext, version string, configs map[string]string) {
	config, err := file.ReadTarFile(filepath.Join(env.IstioSrc, "tests/integration/pilot/testdata/upgrade",
		fmt.Sprintf("%s-install.yaml.tar", version)))
	if err != nil {
		t.Fatalf("could not read installation config: %v", err)
	}
	configs[version] = config
	if err := t.ConfigIstio().YAML(i.Settings().SystemNamespace, config).Apply(apply.NoCleanup); err != nil {
		t.Fatal(err)
	}
}

// skipIfK8sVersionUnsupported skips the test if we're running on a k8s version that is not expected to work
// with any of the revision versions included in the test (i.e. istio 1.7 not supported on k8s 1.15)
func skipIfK8sVersionUnsupported(t framework.TestContext) {
	if !t.Clusters().Default().MinKubeVersion(16) {
		t.Skipf("k8s version not supported for %s (<%s)", t.Name(), "1.16")
	}
	// Kubernetes 1.22 drops support for a number of legacy resources, so we cannot install the old versions
	if !t.Clusters().Default().MaxKubeVersion(21) {
		t.Skipf("k8s version not supported for %s (>%s)", t.Name(), "1.21")
	}
}
