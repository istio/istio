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
	"testing"
	"time"

	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/echoboot"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/util/retry"
	"istio.io/istio/tests/integration/pilot/common"
)

func TestCNIUpgrade(t *testing.T) {
	framework.NewTest(t).
		RequiresSingleCluster().
		Features("installation.upgrade").
		Run(func(t framework.TestContext) {
			skipIfK8sVersionUnsupported(t)
			if !i.Settings().EnableCNI {
				t.Skipf("CNI not enabled")
			}

			// Install 1.9 with CNI and revision `1-9-4`. This will replace the CNI installed from head.
			config, err := ReadInstallFile("1.9.4-cni-install.yaml")
			if err != nil {
				t.Fatalf("could not read installation config: %v", err)
			}
			if err := t.Config().ApplyYAML("", config); err != nil {
				t.Fatal(err)
			}

			// Deploy some new echo instance, which should be using the 1.9 CNI.
			cniNamespace, err := namespace.New(t, namespace.Config{
				Prefix: "cni-test-19",
				Inject: true,
			})
			if err != nil {
				t.Fatal(err)
			}

			cniEcho, err := echoboot.NewBuilder(t).
				WithClusters(t.Clusters()...).
				WithConfig(echo.Config{
					Service:           "cni-19",
					Namespace:         cniNamespace,
					Ports:             common.EchoPorts,
					Subsets:           []echo.SubsetConfig{{}},
					WorkloadOnlyPorts: common.WorkloadPorts,
				}).Build()
			if err != nil {
				t.Fatal(err)
			}

			// Send traffic between podA and 1.9 CNI pod.
			trafficTypes := []string{"http", "tcp", "grpc"}
			testEchos := []echo.Instance{cniEcho[0], apps.PodA[0]}

			for _, source := range testEchos {
				for _, dest := range testEchos {
					if source == dest {
						continue
					}
					for _, trafficType := range trafficTypes {
						t.NewSubTest(fmt.Sprintf("%s-%s->%s", trafficType, source.Config().Service, dest.Config().Service)).
							Run(func(t framework.TestContext) {
								retry.UntilSuccessOrFail(t, func() error {
									resp, err := source.Call(echo.CallOptions{
										Target:   dest,
										PortName: trafficType,
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
		})
}
