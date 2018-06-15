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
	"errors"
	"os"

	"github.com/spf13/cobra"

	"istio.io/istio/istioctl/pkg/kubernetes"
	"istio.io/istio/istioctl/pkg/writer"
)

var (
	statusCmd = &cobra.Command{
		Use:   "proxy-status [pod-name]",
		Short: "Retrieves the synchronization status of each Envoy in the mesh [kube only]",
		Long: `
Retrieves last sent and last acknowledged xDS sync from Pilot to each Envoy in the mesh

`,
		Example: `# Retrieve sync status for all Envoys in a mesh
	istioctl proxy-status

# Retrieve sync status for a single Envoy
	istioctl proxy-status istio-egressgateway-59585c5b9c-ndc59
`,
		Aliases: []string{"ps"},
		RunE: func(c *cobra.Command, args []string) error {
			kubeClient, err := kubernetes.NewClient(kubeconfig, configContext)
			if err != nil {
				return err
			}
			pilots, err := kubeClient.GetPilotPods(istioNamespace)
			if err != nil {
				return err
			}
			if len(pilots) == 0 {
				return errors.New("unable to find any Pilot instances")
			}
			statuses, pilotErr := kubeClient.CallPilotDiscoveryStatus(pilots)
			if pilotErr != nil {
				return err
			}
			sw := writer.StatusWriter{Writer: os.Stdout}
			if len(args) > 0 {
				return sw.PrintSingle(statuses, args[0])
			}
			return sw.PrintAll(statuses)
		},
	}
)

func init() {
	rootCmd.AddCommand(statusCmd)
}
