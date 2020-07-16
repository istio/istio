// Copyright Istio Authors.
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

package cmd

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

var (
	name           string
	serviceAccount string
	filename       string
	labelsMap      map[string]string

	// optional GKE flags
	gkeProject      string
	clusterLocation string
)

func sidecarCommands() *cobra.Command {
	sidecarCmd := &cobra.Command{
		Use:   "sidecar",
		Short: "Commands to assist in managing sidecar configuration",
	}
	sidecarCmd.AddCommand(createGroupCommand())
	sidecarCmd.AddCommand(generateConfigCommand())
	return sidecarCmd
}

func createGroupCommand() *cobra.Command {
	createGroupCmd := &cobra.Command{
		Use:     "create-group",
		Short:   "Creates a WorkloadGroup YAML artifact",
		Long:    "Creates a WorkloadGroup YAML artifact for passing to the Kubernetes API server (kubectl apply -f workloadgroup.yaml)",
		Example: "create-group --name foo --namespace bar --labels app=foobar,version=1 --serviceAccount default",
		Args: func(cmd *cobra.Command, args []string) error {
			if name == "" {
				return fmt.Errorf("requires a service name")
			}
			if namespace == "" {
				return fmt.Errorf("requres a service namespace")
			}
			return nil
		},
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Printf("Registering service %s in namespace %s with labels %s using service account %s\n", name, namespace, labelsMap, serviceAccount)
		},
	}
	createGroupCmd.PersistentFlags().StringVar(&name, "name", "", "Service name")
	createGroupCmd.PersistentFlags().StringVarP(&namespace, "namespace", "n", "", "Namespace of the service")
	createGroupCmd.PersistentFlags().StringToStringVarP(&labelsMap, "labels", "l", nil, "List of labels to apply for the service; e.g. -l env=prod,vers=2")
	createGroupCmd.PersistentFlags().StringVarP(&serviceAccount, "serviceAccount", "s", "default", "Service account to link to the service")
	return createGroupCmd
}

func generateConfigCommand() *cobra.Command {
	generateConfigCmd := &cobra.Command{
		Use:     "generate-config",
		Short:   "Generates and packs all the required VM configuration files",
		Long:    "Retrieves a WorkloadGroup artifact from the Kubernetes API server, then generates and packs all the required VM configuration files.",
		Example: "generate-config --name foo --namespace bar -o filename",
		Args: func(cmd *cobra.Command, args []string) error {
			if name == "" {
				return fmt.Errorf("requires a service name")
			}
			if namespace == "" {
				return fmt.Errorf("requres a service namespace")
			}
			if filename == "" {
				filename = fmt.Sprintf("%s-%s", name, namespace)
			}
			if !strings.Contains(filename, ".tar.gz") {
				filename += ".tar.gz"
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			envVars := map[string]string{}
			istiod, err := k8sService("istiod", "istio-system")
			if err != nil {
				return err
			}
			envVars["IstiodIP"] = istiod.Spec.ClusterIP

			// consider passed in flags before attempting to infer config
			cluster, err := gkeCluster(gkeProject, clusterLocation, clusterName)
			if err == nil {
				fmt.Printf("using provided flags for (project, location, cluster): (%s, %s, %s)\n", gkeProject, clusterLocation, clusterName)
			} else {
				gkeProject, clusterLocation, clusterName, cluster, err = gkeConfig()
				if err != nil {
					return fmt.Errorf("could not infer (project, location, cluster). Try passing in flags instead")
				}
			}
			envVars["ISTIO_SERVICE_CIDR"] = cluster.ServicesIpv4Cidr

			fmt.Printf("Packed service %s in namespace %s into tarball %s\n", name, namespace, filename)
			return nil
		},
	}
	generateConfigCmd.PersistentFlags().StringVar(&name, "name", "", "Service name")
	generateConfigCmd.PersistentFlags().StringVarP(&namespace, "namespace", "n", "", "Namespace of the service")
	generateConfigCmd.PersistentFlags().StringVarP(&filename, "output filename", "o", "", "Name of the tarball to be created")

	generateConfigCmd.PersistentFlags().StringVar(&gkeProject, "project", "", "Target project name")
	generateConfigCmd.PersistentFlags().StringVar(&clusterLocation, "location", "", "Target cluster location")
	generateConfigCmd.PersistentFlags().StringVar(&clusterName, "cluster", "", "Target cluster name")
	return generateConfigCmd
}
