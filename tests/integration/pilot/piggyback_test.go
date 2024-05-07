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
	"strings"
	"testing"

	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/istioctl"
	"istio.io/istio/pkg/test/util/retry"
)

func TestPiggyback(t *testing.T) {
	// nolint: staticcheck
	framework.
		NewTest(t).RequiresSingleCluster().
		RequiresLocalControlPlane().
		RequireIstioVersion("1.10.0").
		Run(func(t framework.TestContext) {
			workloads := []echo.Instances{apps.A, apps.Sotw}
			istioCtl := istioctl.NewOrFail(t, t, istioctl.Config{Cluster: t.Clusters().Default()})
			for _, workload := range workloads {
				podName := workload[0].WorkloadsOrFail(t)[0].PodName()
				namespace := workload.Config().Namespace.Name()

				retry.UntilSuccessOrFail(t, func() error {
					args := []string{
						"x", "proxy-status", "--xds-via-agents", fmt.Sprintf("%s.%s", podName, namespace),
					}
					output, _, err := istioCtl.Invoke(args)
					if err != nil {
						return err
					}
					return expectSubstrings(output, "Clusters Match", "Listeners Match", "Routes Match")
				})

				// Test gRPC-based tapped XDS using istioctl.
				retry.UntilSuccessOrFail(t, func() error {
					pf, err := t.Clusters()[0].NewPortForwarder(podName, namespace, "localhost", 0, 15004)
					if err != nil {
						return fmt.Errorf("failed to create the port forwarder: %v", err)
					}
					pf.Start()
					defer pf.Close()

					argsToTest := []struct {
						args []string
					}{
						{[]string{"x", "proxy-status", "--plaintext", "--xds-address", pf.Address()}},
						{[]string{"proxy-status", "--plaintext", "--xds-address", pf.Address()}},
					}
					for _, args := range argsToTest {
						istioCtl := istioctl.NewOrFail(t, t, istioctl.Config{Cluster: t.Clusters().Default()})
						output, _, err := istioCtl.Invoke(args.args)
						if err != nil {
							return err
						}

						// Just verify pod A is known to Pilot; implicitly this verifies that
						// the printing code printed it.
						if err := expectSubstrings(output, fmt.Sprintf("%s.%s", podName, namespace)); err != nil {
							return err
						}
					}
					return nil
				})

				// Test gRPC-based --xds-via-agents
				retry.UntilSuccessOrFail(t, func() error {
					istioCtl := istioctl.NewOrFail(t, t, istioctl.Config{Cluster: t.Clusters().Default()})
					args := []string{"x", "proxy-status", "--xds-via-agents"}
					output, _, err := istioCtl.Invoke(args)
					if err != nil {
						return err
					}
					return expectSubstrings(output, fmt.Sprintf("%s.%s", podName, namespace))
				})
			}
		})
}

func expectSubstrings(have string, wants ...string) error {
	for _, want := range wants {
		if !strings.Contains(have, want) {
			return fmt.Errorf("substring %q not found; have %q", want, have)
		}
	}
	return nil
}
