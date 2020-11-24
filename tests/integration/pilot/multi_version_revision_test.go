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
	"io/ioutil"
	"path/filepath"
	"testing"
	"time"

	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/test/env"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/echoboot"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/util/retry"
)

type installConfig struct {
	revision string
	path     string
}

type revisionedNamespace struct {
	revision  string
	namespace namespace.Instance
}

// TestMultiVersionRevision tests traffic between data planes running under differently versioned revisions
// should test all possible revisioned namespace pairings to test traffic between all versions
func TestMultiVersionRevision(t *testing.T) {
	framework.NewTest(t).
		RequiresSingleCluster().
		Features("installation.upgrade").
		Run(func(ctx framework.TestContext) {
			testdataDir := filepath.Join(env.IstioSrc, "tests/integration/pilot/testdata/upgrade")

			// keep these at the latest patch version of each minor version
			// TODO(samnaser) add 1.7.4 once we flag-protection for reading service-api CRDs (https://github.com/istio/istio/issues/29054)
			installConfigs := []installConfig{
				{
					revision: "1-6-11",
					path:     filepath.Join(testdataDir, "1.6.11-install.yaml"),
				},
				{
					revision: "1-8-0",
					path:     filepath.Join(testdataDir, "1.8.0-install.yaml"),
				},
			}

			// keep track of applied configurations and clean up after the test
			configs := make(map[string]string)
			defer func() {
				for _, config := range configs {
					ctx.Config().DeleteYAML("istio-system", config)
				}
			}()

			revisionedNamespaces := []revisionedNamespace{}
			for _, c := range installConfigs {
				configBytes, err := ioutil.ReadFile(c.path)
				if err != nil {
					t.Errorf("failed to read config golden from path %s: %v", c.path, err)
				}
				configs[c.revision] = string(configBytes)
				ctx.Config().ApplyYAMLOrFail(t, i.Settings().SystemNamespace, string(configBytes))

				// create a namespace pointing to each revisioned install
				ns := namespace.NewOrFail(t, ctx, namespace.Config{
					Prefix:   fmt.Sprintf("revision-%s", c.revision),
					Inject:   true,
					Revision: c.revision,
				})
				revisionedNamespaces = append(revisionedNamespaces, revisionedNamespace{
					revision:  c.revision,
					namespace: ns,
				})
			}

			// create an echo instance in each revisioned namespace, all these echo
			// instances will be injected with proxies from their respective versions
			builder := echoboot.NewBuilder(ctx)
			instanceCount := len(revisionedNamespaces) + 1
			instances := make([]echo.Instance, instanceCount)

			// add an existing pod from apps to the rotation to avoid an extra deployment
			instances[instanceCount-1] = apps.PodA[0]

			for i, ns := range revisionedNamespaces {
				builder = builder.With(&instances[i], echo.Config{
					Service:   fmt.Sprintf("revision-%s", ns.revision),
					Namespace: ns.namespace,
					Ports: []echo.Port{
						{
							Name:         "http",
							Protocol:     protocol.HTTP,
							InstancePort: 8080,
						}},
				})
			}
			builder.BuildOrFail(t)
			testAllEchoCalls(t, instances)
		})
}

// testAllEchoCalls takes list of revisioned namespaces and generates list of echo calls covering
// communication between every pair of namespaces
func testAllEchoCalls(t *testing.T, echoInstances []echo.Instance) {
	for _, source := range echoInstances {
		for _, dest := range echoInstances {
			if source == dest {
				continue
			}
			t.Run(fmt.Sprintf("%s->%s", source.Config().Service, dest.Config().Service), func(t *testing.T) {
				retry.UntilSuccessOrFail(t, func() error {
					resp, err := source.Call(echo.CallOptions{
						Target:   dest,
						PortName: "http",
					})
					if err != nil {
						return err
					}
					return resp.CheckOK()
				}, retry.Delay(time.Millisecond*150))
			})
		}
	}
}
