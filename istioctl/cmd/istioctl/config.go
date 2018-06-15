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
	"fmt"

	"github.com/spf13/cobra"
	"k8s.io/api/core/v1"

	"istio.io/istio/istioctl/pkg/kubernetes"
	"istio.io/istio/pkg/log"
)

var (
	mesh = "all"

	// TODO - Add diff option to get the difference between pilot's xDS API response and the proxy config
	// TODO - Add support for non-default proxy config locations
	// TODO - Add support for non-kube istio deployments
	// TODO - Bring Endpoint and Pilot config types more inline with each other
	configCmd = &cobra.Command{
		Use:   "proxy-config <endpoint|pilot> <pod-name|mesh> [<configuration-type>]",
		Short: "Retrieves proxy configuration for the specified pod from the endpoint proxy or Pilot [kube only]",
		Long: `
Retrieves proxy configuration for the specified pod from the endpoint proxy or Pilot when running in Kubernetes.
It is also able to retrieve the state of the entire mesh by using mesh instead of <pod-name>. This is only available when querying Pilot.

Available configuration types:

	Endpoint:
	[clusters listeners routes bootstrap]

	Pilot:
	[ads eds]

`,
		Example: `# Retrieve all config for productpage-v1-bb8d5cbc7-k7qbm pod from the endpoint proxy
istioctl proxy-config endpoint productpage-v1-bb8d5cbc7-k7qbm

# Retrieve eds config for productpage-v1-bb8d5cbc7-k7qbm pod from Pilot
istioctl proxy-config pilot productpage-v1-bb8d5cbc7-k7qbm eds

# Retrieve ads config for the mesh from Pilot
istioctl proxy-config pilot mesh ads

# Retrieve bootstrap config for productpage-v1-bb8d5cbc7-k7qbm pod in the application namespace from the endpoint proxy
istioctl proxy-config endpoint -n application productpage-v1-bb8d5cbc7-k7qbm bootstrap`,
		Aliases: []string{"pc"},
		Args:    cobra.MinimumNArgs(2),
		RunE: func(c *cobra.Command, args []string) error {
			location := args[0]
			podName := args[1]
			var configType string
			if len(args) > 2 {
				configType = args[2]
			} else {
				configType = mesh
			}
			log.Infof("Retrieving %v proxy config for %q", configType, podName)

			ns := namespace
			if ns == v1.NamespaceAll {
				ns = defaultNamespace
			}
			kubeClient, err := kubernetes.NewClient(kubeconfig, configContext)
			if err != nil {
				return err
			}
			switch location {
			case "pilot":
				var proxyID string
				if podName == "mesh" {
					proxyID = mesh
				} else {
					proxyID = fmt.Sprintf("%v.%v", podName, ns)
				}
				pilots, err := kubeClient.GetPilotPods(istioNamespace)
				if err != nil {
					return err
				}
				if len(pilots) == 0 {
					return errors.New("unable to find any Pilot instances")
				}
				debug, pilotErr := kubeClient.CallPilotDiscoveryDebug(pilots, proxyID, configType)
				if pilotErr != nil {
					fmt.Println(debug)
					return err
				}
				fmt.Println(debug)
			case "endpoint":
				debug, err := kubeClient.CallPilotAgentDebug(podName, ns, configType)
				if err != nil {
					fmt.Println(debug)
					return err
				}
				fmt.Println(debug)
			default:
				log.Errorf("%q debug not supported", location)
			}
			return nil
		},
	}
)

func init() {
	rootCmd.AddCommand(configCmd)
}
