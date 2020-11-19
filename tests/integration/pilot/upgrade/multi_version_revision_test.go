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

package upgrade

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
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/util/retry"
)

var i istio.Instance

// TestMain defines the entrypoint for pilot tests using a standard Istio installation.
// If a test requires a custom install it should go into its own package, otherwise it should go
// here to reuse a single install across tests.
func TestMain(m *testing.M) {
	framework.
		NewSuite(m).
		RequireSingleCluster().
		Setup(istio.Setup(&i, nil)).
		Run()
}

type installGolden struct {
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
		Features("upgrade").
		Run(func(ctx framework.TestContext) {
			goldenDir := filepath.Join(env.IstioSrc, "tests/integration/pilot/upgrade/goldens")

			// keep these at the latest patch version of each minor version
			// TODO(samnaser) script the creation of these golden files
			installGoldens := []installGolden{
				{
					revision: "1-7-4",
					path:     filepath.Join(goldenDir, "golden-minimal-1.7.4.yaml"),
				},
				{
					revision: "1-6-11",
					path:     filepath.Join(goldenDir, "golden-minimal-1.6.11.yaml"),
				},
			}

			// keep track of applied configurations and clean up after the test
			configs := make(map[string]string)
			defer func() {
				for _, config := range configs {
					ctx.Config().DeleteYAML("istio-system", config)
				}
			}()

			nsNoRevision := namespace.NewOrFail(t, ctx, namespace.Config{
				Prefix: "no-revision",
				Inject: true,
			})
			echoNamespaces := []revisionedNamespace{
				{
					revision:  "none",
					namespace: nsNoRevision,
				},
			}

			for _, g := range installGoldens {
				configBytes, err := ioutil.ReadFile(g.path)
				if err != nil {
					t.Errorf("failed to read config golden from path %s: %v", g.path, err)
				}
				configs[g.revision] = string(configBytes)
				ctx.Config().ApplyYAMLOrFail(t, "istio-system", string(configBytes))

				// create a namespace pointing to each revisioned install
				ns := namespace.NewOrFail(t, ctx, namespace.Config{
					Prefix:   fmt.Sprintf("revision-%s", g.revision),
					Inject:   true,
					Revision: g.revision,
				})
				echoNamespaces = append(echoNamespaces, revisionedNamespace{
					revision:  g.revision,
					namespace: ns,
				})
			}

			// create an echo instance in each revisioned namespace, all these echo
			// instances will be injected with proxies from their respective versions
			builder := echoboot.NewBuilder(ctx)
			instances := make([]echo.Instance, len(echoNamespaces))
			for i, ns := range echoNamespaces {
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

// generateEchoCalls takes list of revisioned namespaces and generates list of echo calls covering
// communication between every pair of namespaces
func testAllEchoCalls(t *testing.T, echoInstances []echo.Instance) {
	for i := 0; i < len(echoInstances); i++ {
		for j := 0; j < len(echoInstances); j++ {
			if i == j {
				continue
			}
			source := echoInstances[i]
			dest := echoInstances[j]
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
