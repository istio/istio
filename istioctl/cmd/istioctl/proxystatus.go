// Copyright 2018 Istio Authors
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

package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"istio.io/istio/istioctl/pkg/kubernetes"
	"istio.io/istio/istioctl/pkg/writer/compare"
	"istio.io/istio/istioctl/pkg/writer/pilot"
)

var (
	statusCmd = &cobra.Command{
		Use:   "proxy-status [<proxy-name>]",
		Short: "Retrieves the synchronization status of each Envoy in the mesh [kube only]",
		Long: `
Retrieves last sent and last acknowledged xDS sync from Pilot to each Envoy in the mesh

`,
		Example: `# Retrieve sync status for all Envoys in a mesh
	istioctl proxy-status

# Retrieve sync diff for a single Envoy and Pilot
	istioctl proxy-status istio-egressgateway-59585c5b9c-ndc59.istio-system
`,
		Aliases: []string{"ps"},
		RunE: func(c *cobra.Command, args []string) error {
			kubeClient, err := clientExecFactory(kubeconfig, configContext)
			if err != nil {
				return err
			}
			if len(args) > 0 {
				podName, ns := getProxyDetails(args[0])
				path := fmt.Sprintf("config_dump")
				envoyDump, err := kubeClient.EnvoyDo(podName, ns, "GET", path, nil)
				if err != nil {
					return err
				}
				path = fmt.Sprintf("/debug/config_dump?proxyID=%s.%s", podName, ns)
				pilotDumps, err := kubeClient.AllPilotsDiscoveryDo(istioNamespace, "GET", path, nil)
				if err != nil {
					return err
				}
				c, err := compare.NewComparator(os.Stdout, pilotDumps, envoyDump)
				if err != nil {
					return err
				}
				return c.Diff()
			}
			statuses, err := kubeClient.AllPilotsDiscoveryDo(istioNamespace, "GET", "/debug/syncz", nil)
			if err != nil {
				return err
			}
			sw := pilot.StatusWriter{Writer: c.OutOrStdout()}
			return sw.PrintAll(statuses)
		},
	}
)

func init() {
	rootCmd.AddCommand(statusCmd)
}

func newExecClient(kubeconfig, configContext string) (kubernetes.ExecClient, error) {
	return kubernetes.NewClient(kubeconfig, configContext)
}
