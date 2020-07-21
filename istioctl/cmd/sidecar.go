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

	"github.com/ghodss/yaml"
	"github.com/spf13/cobra"

	"istio.io/api/networking/v1alpha3"
	"istio.io/istio/pkg/config/schema/collections"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

var (
	name           string
	network        string
	serviceAccount string
	filename       string
	ports          []string

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
		Short:   "Creates a WorkloadGroup YAML artifact representing workload instances",
		Long:    "Creates a WorkloadGroup YAML artifact representing workload instances for passing to the Kubernetes API server (kubectl apply -f workloadgroup.yaml)",
		Example: "create-group --name foo --namespace bar --labels app=foo,bar=baz --ports grpc=3550,http=8080 --network local --serviceAccount sa",
		Args: func(cmd *cobra.Command, args []string) error {
			if name == "" {
				return fmt.Errorf("expecting a service name")
			}
			if namespace == "" {
				return fmt.Errorf("expecting a service namespace")
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			u := &unstructured.Unstructured{
				Object: map[string]interface{}{
					"apiVersion": collections.IstioNetworkingV1Alpha3Workloadgroups.Resource().APIVersion(),
					"kind":       collections.IstioNetworkingV1Alpha3Workloadgroups.Resource().Kind(),
					"metadata": map[string]interface{}{
						"name":      name,
						"namespace": namespace,
					},
				},
			}
			spec := &v1alpha3.WorkloadGroup{
				Labels:         convertToStringMap(labels),
				Ports:          convertToUnsignedInt32Map(ports),
				Network:        network,
				ServiceAccount: serviceAccount,
			}
			wgYAML, err := generateWorkloadGroupYAML(u, spec)
			if err != nil {
				return err
			}
			cmd.OutOrStdout().Write([]byte(wgYAML))
			return nil
		},
	}
	createGroupCmd.PersistentFlags().StringVar(&name, "name", "", "The name of the workload group")
	createGroupCmd.PersistentFlags().StringVarP(&namespace, "namespace", "n", "", "The namespace that the workload instances will belong to")
	createGroupCmd.PersistentFlags().StringSliceVarP(&labels, "labels", "l", nil, "The labels to apply to the workload instances; e.g. -l env=prod,vers=2")
	createGroupCmd.PersistentFlags().StringSliceVarP(&ports, "ports", "p", nil, "The incoming ports that the workload instances will expose")
	createGroupCmd.PersistentFlags().StringVar(&network, "network", "default", "The name of the network for the workload instances")
	createGroupCmd.PersistentFlags().StringVarP(&serviceAccount, "serviceAccount", "s", "default", "The service identity to associate with the workload instances")
	return createGroupCmd
}

func generateWorkloadGroupYAML(u *unstructured.Unstructured, spec *v1alpha3.WorkloadGroup) (string, error) {
	iSpec, err := unstructureIstioType(spec)
	if err != nil {
		return "", err
	}
	u.Object["spec"] = iSpec

	wgYAML, err := yaml.Marshal(u.Object)
	if err != nil {
		return "", err
	}
	return string(wgYAML), nil
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
