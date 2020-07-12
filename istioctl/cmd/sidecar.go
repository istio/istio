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

func vmRegisterCmd() *cobra.Command {
	vmRegisterCmd := &cobra.Command{
		Use:     "vm-register",
		Short:   "Creates a VM deployment resource",
		Long:    "longer",
		Example: "vm-register --name foo -n bar -l env=prod,vers=2 --serviceAccount sa",
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
	vmRegisterCmd.PersistentFlags().StringVar(&name, "name", "", "Service name")
	vmRegisterCmd.PersistentFlags().StringVarP(&namespace, "namespace", "n", "", "Namespace of the service")
	vmRegisterCmd.PersistentFlags().StringToStringVarP(&labelsMap, "labels", "l", nil, "List of labels to apply for the service; e.g. -l env=prod,vers=2")
	vmRegisterCmd.PersistentFlags().StringVarP(&serviceAccount, "serviceAccount", "s", "default", "Service account to link to the service")
	return vmRegisterCmd
}

func vmPackCmd() *cobra.Command {
	vmPackCmd := &cobra.Command{
		Use:     "vm-pack",
		Short:   "Pack VM dependencies into a tarball",
		Long:    "Packs ??? into a tarball",
		Example: "vm-pack --name foo -n bar -o foo.tar.gz",
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
			clientset, err := outsideClientset()
			if err != nil {
				return err
			}

			istiod, err := k8sService(clientset, "istio-system", "istiod")
			if err != nil {
				return err
			}
			envVars["IstiodIP"] = istiod.Spec.ClusterIP

			// consider passed in flags before attempting to infer config
			if validGKEConfig(gkeProject, clusterLocation, clusterName) {
				fmt.Printf("using provided flags for (project, location, cluster): (%s, %s, %s)\n", gkeProject, clusterLocation, clusterName)
			} else {
				gkeProject, clusterLocation, clusterName = gkeConfig()
				if !validGKEConfig(gkeProject, clusterLocation, clusterLocation) {
					return fmt.Errorf("could not infer (project, location, cluster). Try passing in flags instead")
				}
				fmt.Printf("inferred (project, location, cluster): (%s, %s, %s)\n", gkeProject, clusterLocation, clusterName)
			}
			cluster, err := gkeCluster(gkeProject, clusterLocation, clusterName)
			if err != nil {
				return err
			}
			envVars["ISTIO_SERVICE_CIDR"] = cluster.ServicesIpv4Cidr

			fmt.Printf("Packed service %s in namespace %s into tarball %s\n", name, namespace, filename)
			return nil
		},
	}
	vmPackCmd.PersistentFlags().StringVar(&name, "name", "", "Service name")
	vmPackCmd.PersistentFlags().StringVarP(&namespace, "namespace", "n", "", "Namespace of the service")
	vmPackCmd.PersistentFlags().StringVarP(&filename, "filename", "o", "", "Name of the tarball to be created")

	vmPackCmd.PersistentFlags().StringVar(&gkeProject, "project", "", "Target project name")
	vmPackCmd.PersistentFlags().StringVar(&clusterLocation, "location", "", "Target cluster location")
	vmPackCmd.PersistentFlags().StringVar(&clusterName, "cluster", "", "Target cluster name")
	return vmPackCmd
}
