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
	"k8s.io/api/core/v1"

	"istio.io/istio/istioctl/pkg/kubernetes"
	"istio.io/istio/istioctl/pkg/writer/envoy/configdump"
	"istio.io/istio/pkg/log"
)

var (
	mesh             = "all"
	debugConfigTypes = map[string]string{
		"ads": "adsz",
		"eds": "edsz",
	}

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
				return pilotConfig(kubeClient, podName, ns, configType)
			case "endpoint":
				return endpointConfig(kubeClient, podName, ns, configType)
			default:
				log.Errorf("%q debug not supported", location)
			}
			return nil
		},
	}
)

func pilotConfig(kubeClient *kubernetes.Client, podName, podNamespace, configType string) error {
	path := ""
	ctEndpoint, ok := debugConfigTypes[configType]
	if !ok {
		return fmt.Errorf("%q is not a supported debugging config type", configType)
	}
	path = fmt.Sprintf("/debug/%v", ctEndpoint)

	if podName != "mesh" {
		path += fmt.Sprintf("?proxyID=%v.%v", podName, podNamespace)
	}
	configs, err := kubeClient.PilotDiscoveryDo(istioNamespace, "GET", path, nil)
	if err != nil {
		return err
	}
	for _, config := range configs {
		fmt.Println(string(config))
	}
	return nil
}

func endpointConfig(kubeClient *kubernetes.Client, podName, podNamespace, configType string) error {
	path := "config_dump"
	debug, err := kubeClient.EnvoyDo(podName, podNamespace, "GET", path, nil)
	if err != nil {
		return err
	}
	cw := configdump.ConfigWriter{Stdout: os.Stdout}
	err = cw.Prime(debug)
	if err != nil {
		return err
	}
	switch configType {
	case "cluster", "clusters":
		return cw.PrintClusterDump()
	case "listener", "listeners":
		return cw.PrintListenerDump()
	case "route", "routes":
		return cw.PrintRoutesDump()
	case "bootstrap":
		return cw.PrintBootstrapDump()
	case "all":
		if err := cw.PrintClusterDump(); err != nil {
			return err
		}
		if err := cw.PrintListenerDump(); err != nil {
			return err
		}
		if err := cw.PrintRoutesDump(); err != nil {
			return err
		}
		if err := cw.PrintBootstrapDump(); err != nil {
			return err
		}
	default:
		return fmt.Errorf("%q is not a supported debugging config type", configType)
	}
	return nil
}

func init() {
	rootCmd.AddCommand(configCmd)
}
